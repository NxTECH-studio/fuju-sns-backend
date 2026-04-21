package handler

import (
	"net/http"

	timelineusecase "github.com/fuju/backend/internal/usecase/timeline"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
)

// TimelineHandler wires the HTTP layer onto the timeline use cases.
type TimelineHandler struct {
	home   *timelineusecase.HomeTimelineUseCase
	user   *timelineusecase.UserTimelineUseCase
	global *timelineusecase.GlobalTimelineUseCase
}

// NewTimelineHandler constructs a TimelineHandler.
func NewTimelineHandler(
	home *timelineusecase.HomeTimelineUseCase,
	user *timelineusecase.UserTimelineUseCase,
	global *timelineusecase.GlobalTimelineUseCase,
) *TimelineHandler {
	return &TimelineHandler{home: home, user: user, global: global}
}

// Home handles GET /timeline/home. Requires authentication.
func (h *TimelineHandler) Home(w http.ResponseWriter, r *http.Request) {
	me, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}
	cursor := parseCursorQuery(r)
	limit := parseLimitQuery(r)
	details, next, err := h.home.Execute(r.Context(), me, cursor, limit)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	writeJSONResponse(w, toPostListResponse(details, next), http.StatusOK)
}

// User handles GET /timeline/user/{sub}. Public read — viewer is optional.
func (h *TimelineHandler) User(w http.ResponseWriter, r *http.Request) {
	targetSub, ok := parseSubFromPath(w, r, "sub")
	if !ok {
		return
	}
	cursor := parseCursorQuery(r)
	limit := parseLimitQuery(r)
	viewer := optionalSubFromContext(r.Context())
	details, next, err := h.user.Execute(r.Context(), targetSub, cursor, limit, viewer)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	writeJSONResponse(w, toPostListResponse(details, next), http.StatusOK)
}

// Global handles GET /timeline/global. Public read — viewer is optional.
func (h *TimelineHandler) Global(w http.ResponseWriter, r *http.Request) {
	cursor := parseCursorQuery(r)
	limit := parseLimitQuery(r)
	viewer := optionalSubFromContext(r.Context())
	details, next, err := h.global.Execute(r.Context(), cursor, limit, viewer)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	writeJSONResponse(w, toPostListResponse(details, next), http.StatusOK)
}
