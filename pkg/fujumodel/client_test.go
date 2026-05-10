package fujumodel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubTokens returns a fixed token regardless of scope.
type stubTokens struct {
	tok string
	err error
}

func (s stubTokens) GetServiceToken(_ context.Context, _ string) (string, error) {
	return s.tok, s.err
}

func TestNew_Validation(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		ok   bool
	}{
		{"happy", Options{BaseURL: "https://x", TenantID: "t", Tokens: stubTokens{tok: "x"}}, true},
		{"missing baseurl", Options{TenantID: "t", Tokens: stubTokens{tok: "x"}}, false},
		{"missing tenant", Options{BaseURL: "https://x", Tokens: stubTokens{tok: "x"}}, false},
		{"missing tokens", Options{BaseURL: "https://x", TenantID: "t"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.opts)
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestRegisterContents_HappyPath(t *testing.T) {
	var seenPath, seenAuth, seenContentType string
	var seenBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		seenContentType = r.Header.Get("Content-Type")
		seenBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"upserted":1}`))
	}))
	t.Cleanup(srv.Close)

	c, err := New(Options{BaseURL: srv.URL, TenantID: "sns_a", Tokens: stubTokens{tok: "svc_xxx"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	handle := "@alice"
	createdAt := time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC)
	if err := c.RegisterContents(context.Background(), []Content{{
		ContentID: "01HX", AuthorID: "01HY", CreatorHandle: &handle,
		Text:      ptr("hello"),
		ImageURLs: []string{"https://cdn/x.jpg"},
		CreatedAt: &createdAt,
	}}); err != nil {
		t.Fatalf("RegisterContents: %v", err)
	}
	if seenPath != "/v1/sns_a/contents" {
		t.Fatalf("path=%q", seenPath)
	}
	if seenAuth != "Bearer svc_xxx" {
		t.Fatalf("auth=%q", seenAuth)
	}
	if seenContentType != "application/json" {
		t.Fatalf("content-type=%q", seenContentType)
	}
	var got struct {
		Items []Content `json:"items"`
	}
	if err := json.Unmarshal(seenBody, &got); err != nil {
		t.Fatalf("unmarshal: %v body=%q", err, string(seenBody))
	}
	if len(got.Items) != 1 || got.Items[0].ContentID != "01HX" {
		t.Fatalf("items=%+v", got.Items)
	}
}

func TestSendEvents_HappyPath(t *testing.T) {
	var seenBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := New(Options{BaseURL: srv.URL, TenantID: "sns_a", Tokens: stubTokens{tok: "t"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	dur := 60.0
	pos := 58.0
	if err := c.SendEvents(context.Background(), []Event{{
		UserID: "01HU", ItemID: "01HX",
		EventType:       EventViewEnd,
		Timestamp:       time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC),
		DurationSeconds: &dur, PositionSeconds: &pos,
	}, {
		UserID: "01HU", ItemID: "01HX",
		EventType: EventLike,
		Timestamp: time.Date(2026, 4, 28, 10, 1, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("SendEvents: %v", err)
	}
	var got struct {
		Events []Event `json:"events"`
	}
	if err := json.Unmarshal(seenBody, &got); err != nil {
		t.Fatalf("unmarshal: %v body=%q", err, string(seenBody))
	}
	if len(got.Events) != 2 {
		t.Fatalf("events=%d", len(got.Events))
	}
	if got.Events[0].EventType != EventViewEnd {
		t.Fatalf("ev[0]=%v", got.Events[0])
	}
}

func TestSendEvents_EmptyIsNoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)
	c, _ := New(Options{BaseURL: srv.URL, TenantID: "t", Tokens: stubTokens{tok: "x"}})
	if err := c.SendEvents(context.Background(), nil); err != nil {
		t.Fatalf("err: %v", err)
	}
	if called {
		t.Fatalf("HTTP must not be called for an empty batch")
	}
}

func TestStatusCodeMapping(t *testing.T) {
	cases := []struct {
		status  int
		body    string
		wantErr error
	}{
		{http.StatusOK, "", nil},
		{http.StatusUnauthorized, ``, ErrUnauthorized},
		{http.StatusBadRequest, `{"detail":"bad"}`, ErrInvalidPayload},
		{http.StatusUnprocessableEntity, `{"detail":"bad"}`, ErrInvalidPayload},
		{http.StatusBadGateway, ``, ErrUpstream},
		{http.StatusServiceUnavailable, ``, ErrUpstream},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				}
			}))
			t.Cleanup(srv.Close)
			c, _ := New(Options{BaseURL: srv.URL, TenantID: "t", Tokens: stubTokens{tok: "x"}})
			err := c.SendEvents(context.Background(), []Event{{UserID: "u", ItemID: "i", EventType: EventLike, Timestamp: time.Now()}})
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v want=%v", err, tc.wantErr)
			}
		})
	}
}

func TestServiceTokenSourceErrorPropagates(t *testing.T) {
	c, _ := New(Options{
		BaseURL: "https://example.invalid", TenantID: "t",
		Tokens: stubTokens{err: errors.New("boom")},
	})
	err := c.SendEvents(context.Background(), []Event{{UserID: "u", ItemID: "i", EventType: EventLike, Timestamp: time.Now()}})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("err=%v want ErrUpstream", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err message lost upstream context: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
