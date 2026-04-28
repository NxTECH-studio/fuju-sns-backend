package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	fujuusecase "github.com/fuju/backend/internal/usecase/fuju"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/fujumodel"
	"github.com/fuju/backend/pkg/logger"
)

// recordingSender captures every batch the dispatcher flushes.
type recSender struct {
	events   [][]fujumodel.Event
	contents [][]fujumodel.Content
}

func (r *recSender) RegisterContents(_ context.Context, c []fujumodel.Content) error {
	cp := make([]fujumodel.Content, len(c))
	copy(cp, c)
	r.contents = append(r.contents, cp)
	return nil
}

func (r *recSender) SendEvents(_ context.Context, e []fujumodel.Event) error {
	cp := make([]fujumodel.Event, len(e))
	copy(cp, e)
	r.events = append(r.events, cp)
	return nil
}

func newEventsFixture(t *testing.T) (*MeEventsHandler, *recSender) {
	t.Helper()
	sender := &recSender{}
	d, err := fujuusecase.NewDispatcher(fujuusecase.Options{
		Sender: sender, Log: logger.New("error"),
		BatchSize: 1, FlushInterval: 30 * time.Millisecond,
		QueueCapacity: 16, SendTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.Run(ctx)
	t.Cleanup(func() {
		cancel()
		d.Wait()
	})
	return NewMeEventsHandler(d), sender
}

// authedCtx builds a request context with sub set, mirroring what
// AuthMiddleware attaches in production.
func authedCtx() context.Context {
	return auth.SetSubInContext(context.Background(), "01USER")
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitFor: condition not met within %v", timeout)
}

func TestMeEvents_RequiresAuth(t *testing.T) {
	h, _ := newEventsFixture(t)
	body := strings.NewReader(`{"events":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/me/events", body)
	rr := httptest.NewRecorder()
	h.PostEvents(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rr.Code)
	}
}

func TestMeEvents_HappyPath(t *testing.T) {
	h, sender := newEventsFixture(t)

	body := bytes.NewBufferString(`{
        "events": [
            {"item_id":"01POST","event_type":"view_start","timestamp":"2026-04-28T10:00:00Z"},
            {"item_id":"01POST","event_type":"view_end","timestamp":"2026-04-28T10:00:45Z","duration_seconds":45,"position_seconds":45}
        ]
    }`)
	req := httptest.NewRequest(http.MethodPost, "/v1/me/events", body).WithContext(authedCtx())
	rr := httptest.NewRecorder()
	h.PostEvents(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]int
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["accepted"] != 2 {
		t.Fatalf("accepted=%d want 2", resp["accepted"])
	}

	waitFor(t, time.Second, func() bool {
		count := 0
		for _, b := range sender.events {
			count += len(b)
		}
		return count == 2
	})
	// user_id is server-overridden; confirm.
	for _, b := range sender.events {
		for _, e := range b {
			if e.UserID != "01USER" {
				t.Fatalf("event UserID=%q want 01USER", e.UserID)
			}
		}
	}
}

func TestMeEvents_RejectsServerSideTypes(t *testing.T) {
	h, _ := newEventsFixture(t)
	cases := []string{"like", "follow", "comment", "share", "save", "unsave"}
	for _, et := range cases {
		t.Run(et, func(t *testing.T) {
			body := strings.NewReader(`{"events":[{"item_id":"01P","event_type":"` + et + `","timestamp":"2026-04-28T10:00:00Z"}]}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/me/events", body).WithContext(authedCtx())
			rr := httptest.NewRecorder()
			h.PostEvents(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want 400 for et=%q", rr.Code, et)
			}
		})
	}
}

func TestMeEvents_ViewEndRequiresDuration(t *testing.T) {
	h, _ := newEventsFixture(t)
	body := strings.NewReader(`{"events":[{"item_id":"01P","event_type":"view_end","timestamp":"2026-04-28T10:00:00Z"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/me/events", body).WithContext(authedCtx())
	rr := httptest.NewRecorder()
	h.PostEvents(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMeEvents_RejectsMissingItemID(t *testing.T) {
	h, _ := newEventsFixture(t)
	body := strings.NewReader(`{"events":[{"event_type":"view_start","timestamp":"2026-04-28T10:00:00Z"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/me/events", body).WithContext(authedCtx())
	rr := httptest.NewRecorder()
	h.PostEvents(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestMeEvents_BodyTooLarge(t *testing.T) {
	h, _ := newEventsFixture(t)
	huge := strings.Repeat("x", maxEventsBatchBytes+10)
	body := strings.NewReader(`{"events":[{"item_id":"` + huge + `","event_type":"view_start","timestamp":"2026-04-28T10:00:00Z"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/me/events", body).WithContext(authedCtx())
	rr := httptest.NewRecorder()
	h.PostEvents(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMeEvents_EmptyBatchOK(t *testing.T) {
	h, _ := newEventsFixture(t)
	body := strings.NewReader(`{"events":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/me/events", body).WithContext(authedCtx())
	rr := httptest.NewRecorder()
	h.PostEvents(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
}
