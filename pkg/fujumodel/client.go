// Package fujumodel is the HTTP client this backend uses to talk to
// fuju-emotion-model. Two endpoints are exposed:
//
//   - POST /v1/{tenant}/contents — register a creator's work when a
//     post is published in SNS
//   - POST /v1/{tenant}/events   — append a batch of user events
//     (view_*, like, follow, comment, ...)
//
// Both calls authenticate with an AuthCore-issued service token via
// ServiceTokenSource. The token is fetched lazily and cached in the
// source; this client only consumes it.
//
// The on-the-wire payload shapes match fuju's pydantic schemas
// (`api/ingest_schemas.ContentMetadataIngest` /
// `api/schemas.RawEvent`). Field names use snake_case to keep the
// JSON identical without per-field tags except where Go style and
// JSON convention diverge.
package fujumodel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Errors surfaced to callers.
var (
	// ErrUpstream covers transport failures, 5xx responses, and other
	// "fuju is unreachable / broken" cases. Caller can retry.
	ErrUpstream = errors.New("fujumodel: upstream error")
	// ErrInvalidPayload covers 4xx responses (other than 401) — the
	// caller built a bad request and retrying as-is will not help.
	ErrInvalidPayload = errors.New("fujumodel: invalid payload")
	// ErrUnauthorized — the service token was rejected. Caller should
	// refresh credentials and retry once.
	ErrUnauthorized = errors.New("fujumodel: unauthorized")
)

// ServiceTokenSource issues a Bearer service token for outbound calls.
// Implemented by “authcore.CachedServiceTokenSource“.
type ServiceTokenSource interface {
	GetServiceToken(ctx context.Context, scope string) (string, error)
}

// Options configure the client.
type Options struct {
	BaseURL    string             // e.g. "https://fuju.example.com"
	TenantID   string             // path-segment tenant id (e.g. "sns_a")
	Tokens     ServiceTokenSource // required
	Scope      string             // optional: requested scope on token issuance
	HTTPClient *http.Client       // default: 5s timeout
}

// Client is the HTTP client.
type Client struct {
	baseURL    string
	tenantID   string
	tokens     ServiceTokenSource
	scope      string
	httpClient *http.Client
}

// New builds a Client. Validates Options sufficient to fail fast in
// main.go's bootstrap.
func New(opts Options) (*Client, error) {
	if opts.BaseURL == "" {
		return nil, errors.New("fujumodel: BaseURL is required")
	}
	if opts.TenantID == "" {
		return nil, errors.New("fujumodel: TenantID is required")
	}
	if opts.Tokens == nil {
		return nil, errors.New("fujumodel: Tokens (ServiceTokenSource) is required")
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(opts.BaseURL, "/"),
		tenantID:   opts.TenantID,
		tokens:     opts.Tokens,
		scope:      opts.Scope,
		httpClient: httpClient,
	}, nil
}

// ─── Payload types (mirror fuju's pydantic schemas) ──────────────────

// Content is a single content registration. Mirrors fuju's
// “ContentMetadataIngest“. SNS does NOT pre-extract hashtags /
// entities / theme_text — fuju does that side itself.
type Content struct {
	ContentID       string     `json:"content_id"`
	AuthorID        string     `json:"author_id"`
	CreatorHandle   *string    `json:"creator_handle,omitempty"`
	DisplayTitle    *string    `json:"display_title,omitempty"`
	Text            *string    `json:"text,omitempty"`
	ImageURLs       []string   `json:"image_urls,omitempty"`
	Genre           *string    `json:"genre,omitempty"`
	DurationSeconds *float64   `json:"duration_seconds,omitempty"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
}

// EventType enumerates the values fuju's RawEvent.event_type accepts.
// Frontend / backend hooks must use one of these.
type EventType string

// Event types accepted by fuju (mirrors api.schemas.EventType on the
// fuju side). See docs/sns_log_contract.md in the fuju repo for the
// per-type semantics.
const (
	EventViewStart  EventType = "view_start"
	EventViewEnd    EventType = "view_end"
	EventScrollStop EventType = "scroll_stop"
	EventRewind     EventType = "rewind"
	EventSave       EventType = "save"
	EventUnsave     EventType = "unsave"
	EventShare      EventType = "share"
	EventComment    EventType = "comment"
	EventLike       EventType = "like"
	EventFollow     EventType = "follow"
	EventReport     EventType = "report"
)

// Event mirrors fuju's RawEvent. “ItemID“ is the target post's
// content_id for view/like/comment/share/save/scroll_stop/rewind/report,
// and the followee's user_id for follow events.
type Event struct {
	UserID          string         `json:"user_id"`
	ItemID          string         `json:"item_id"`
	EventType       EventType      `json:"event_type"`
	Timestamp       time.Time      `json:"timestamp"`
	DurationSeconds *float64       `json:"duration_seconds,omitempty"`
	PositionSeconds *float64       `json:"position_seconds,omitempty"`
	Text            *string        `json:"text,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// ─── HTTP calls ──────────────────────────────────────────────────────

// RegisterContents POSTs one or more contents to fuju's
// /v1/{tenant}/contents. Idempotent on (tenant, content_id) — fuju
// UPSERTs by content_id so safe to retry.
func (c *Client) RegisterContents(ctx context.Context, items []Content) error {
	if len(items) == 0 {
		return nil
	}
	body := struct {
		Items []Content `json:"items"`
	}{Items: items}
	path := fmt.Sprintf("/v1/%s/contents", c.tenantID)
	return c.postJSON(ctx, path, body)
}

// SendEvents POSTs a batch of events to fuju's /v1/{tenant}/events.
// Append-only on the fuju side — caller must not retry the same batch
// without deduplication, since fuju does not de-dup events.
func (c *Client) SendEvents(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	body := struct {
		Events []Event `json:"events"`
	}{Events: events}
	path := fmt.Sprintf("/v1/%s/events", c.tenantID)
	return c.postJSON(ctx, path, body)
}

func (c *Client) postJSON(ctx context.Context, path string, body any) error {
	tok, err := c.tokens.GetServiceToken(ctx, c.scope)
	if err != nil {
		return fmt.Errorf("%w: get service token: %v", ErrUpstream, err)
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: marshal body: %v", ErrInvalidPayload, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrUpstream, err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: status=%d", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("%w: status=%d body=%q", ErrInvalidPayload, resp.StatusCode, string(preview))
	default:
		return fmt.Errorf("%w: status=%d", ErrUpstream, resp.StatusCode)
	}
}
