package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	fujuusecase "github.com/fuju/backend/internal/usecase/fuju"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
	"github.com/fuju/backend/pkg/fujumodel"
)

// maxEventsBatchBytes caps the POST /v1/me/events JSON body. The
// frontend batches view_*/scroll_stop/rewind locally and flushes in
// modest sized batches; 64 KiB easily holds a few hundred events.
const maxEventsBatchBytes = 64 * 1024

// MeEventsHandler accepts client-side telemetry events (view_start /
// view_end / scroll_stop / rewind) from the SNS frontend and forwards
// them to fuju via the dispatcher. The endpoint is auth-required: the
// caller's sub overrides any user_id sent in the body, so a malicious
// client cannot fake telemetry on someone else's behalf.
type MeEventsHandler struct {
	dispatcher *fujuusecase.Dispatcher
}

// NewMeEventsHandler constructs a MeEventsHandler.
func NewMeEventsHandler(dispatcher *fujuusecase.Dispatcher) *MeEventsHandler {
	return &MeEventsHandler{dispatcher: dispatcher}
}

// meEventInput is the request shape from the SNS frontend.
//
// Whitelist of acceptable EventTypes: deliberately narrow to the ones
// the frontend can legitimately observe. like / follow / comment are
// emitted server-side via use case hooks; the frontend must not be
// allowed to spoof them on this endpoint.
type meEventInput struct {
	ItemID          string         `json:"item_id"`
	EventType       string         `json:"event_type"`
	Timestamp       time.Time      `json:"timestamp"`
	DurationSeconds *float64       `json:"duration_seconds,omitempty"`
	PositionSeconds *float64       `json:"position_seconds,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type meEventsBatch struct {
	Events []meEventInput `json:"events"`
}

// allowedFrontendEvents lists the EventTypes accepted from the
// frontend telemetry endpoint.
var allowedFrontendEvents = map[string]fujumodel.EventType{
	"view_start":  fujumodel.EventViewStart,
	"view_end":    fujumodel.EventViewEnd,
	"scroll_stop": fujumodel.EventScrollStop,
	"rewind":      fujumodel.EventRewind,
}

// PostEvents handles POST /v1/me/events.
func (h *MeEventsHandler) PostEvents(w http.ResponseWriter, r *http.Request) {
	sub, ok := auth.GetSubFromContext(r.Context())
	if !ok || sub == "" {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxEventsBatchBytes+1))
	if err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("failed to read request body", err))
		return
	}
	if len(body) > maxEventsBatchBytes {
		WriteErrorResponse(w, errors.InvalidRequest("request body exceeds 64KiB", nil))
		return
	}

	var batch meEventsBatch
	if err := json.Unmarshal(body, &batch); err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid JSON", err))
		return
	}
	if len(batch.Events) == 0 {
		writeJSONResponse(w, map[string]int{"accepted": 0}, http.StatusOK)
		return
	}

	accepted := 0
	for _, ev := range batch.Events {
		typ, ok := allowedFrontendEvents[ev.EventType]
		if !ok {
			WriteErrorResponse(w, errors.ValidationFailed("unsupported event_type: "+ev.EventType))
			return
		}
		if ev.ItemID == "" {
			WriteErrorResponse(w, errors.ValidationFailed("item_id is required"))
			return
		}
		if ev.Timestamp.IsZero() {
			WriteErrorResponse(w, errors.ValidationFailed("timestamp is required"))
			return
		}
		// view_end demands duration_seconds (RFC-LT-003); the frontend
		// is responsible for emitting it. Reject early with a clear
		// message rather than silently letting fuju drop it.
		if typ == fujumodel.EventViewEnd && ev.DurationSeconds == nil {
			WriteErrorResponse(w, errors.ValidationFailed("view_end requires duration_seconds"))
			return
		}
		h.dispatcher.EnqueueEvent(r.Context(), fujumodel.Event{
			UserID:          sub, // server-side override; client value (if any) is ignored
			ItemID:          ev.ItemID,
			EventType:       typ,
			Timestamp:       ev.Timestamp,
			DurationSeconds: ev.DurationSeconds,
			PositionSeconds: ev.PositionSeconds,
			Metadata:        ev.Metadata,
		})
		accepted++
	}
	writeJSONResponse(w, map[string]int{"accepted": accepted}, http.StatusOK)
}
