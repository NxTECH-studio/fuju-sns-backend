package handler

import (
	"net/http"

	"github.com/fuju/backend/internal/domain"
	followusecase "github.com/fuju/backend/internal/usecase/follow"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
)

// FollowHandler wires the HTTP layer onto the follow use cases.
type FollowHandler struct {
	follow        *followusecase.FollowUseCase
	unfollow      *followusecase.UnfollowUseCase
	listFollowers *followusecase.ListFollowersUseCase
	listFollowing *followusecase.ListFollowingUseCase
}

// NewFollowHandler constructs a FollowHandler.
func NewFollowHandler(
	follow *followusecase.FollowUseCase,
	unfollow *followusecase.UnfollowUseCase,
	listFollowers *followusecase.ListFollowersUseCase,
	listFollowing *followusecase.ListFollowingUseCase,
) *FollowHandler {
	return &FollowHandler{
		follow:        follow,
		unfollow:      unfollow,
		listFollowers: listFollowers,
		listFollowing: listFollowing,
	}
}

// followResultView is the POST/DELETE follow response shape. Returns the
// refreshed followers_count so the profile UI can reflect the change
// without a second roundtrip.
type followResultView struct {
	Following      bool  `json:"following"`
	FollowersCount int64 `json:"followers_count"`
}

// followListResponse is the cursor-paginated envelope for followers /
// following lists. Matches the post list shape.
type followListResponse struct {
	Data       []publicUserView `json:"data"`
	NextCursor *string          `json:"next_cursor"`
}

// Follow handles POST /users/{sub}/follow.
func (h *FollowHandler) Follow(w http.ResponseWriter, r *http.Request) {
	targetSub, ok := parseSubFromPath(w, r, "sub")
	if !ok {
		return
	}
	me, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}
	res, err := h.follow.Execute(r.Context(), me, targetSub)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	WriteSuccessResponse(w, followResultView{Following: res.Following, FollowersCount: res.FollowersCount}, http.StatusOK)
}

// Unfollow handles DELETE /users/{sub}/follow.
func (h *FollowHandler) Unfollow(w http.ResponseWriter, r *http.Request) {
	targetSub, ok := parseSubFromPath(w, r, "sub")
	if !ok {
		return
	}
	me, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}
	res, err := h.unfollow.Execute(r.Context(), me, targetSub)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	WriteSuccessResponse(w, followResultView{Following: res.Following, FollowersCount: res.FollowersCount}, http.StatusOK)
}

// ListFollowers handles GET /users/{sub}/followers. The cursor, when
// present, is a base64url(RFC3339Nano + "|" + follower_sub) token
// returned by a prior page; see repository.FollowRepository for the
// canonical definition.
func (h *FollowHandler) ListFollowers(w http.ResponseWriter, r *http.Request) {
	targetSub, ok := parseSubFromPath(w, r, "sub")
	if !ok {
		return
	}
	cursor := parseOpaqueCursorQuery(r)
	limit := parseLimitQuery(r)

	users, next, err := h.listFollowers.Execute(r.Context(), targetSub, cursor, limit)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	writeJSONResponse(w, buildFollowListResponse(users, next), http.StatusOK)
}

// ListFollowing handles GET /users/{sub}/following. Cursor format matches
// ListFollowers except the peer is followee_sub instead of follower_sub.
func (h *FollowHandler) ListFollowing(w http.ResponseWriter, r *http.Request) {
	targetSub, ok := parseSubFromPath(w, r, "sub")
	if !ok {
		return
	}
	cursor := parseOpaqueCursorQuery(r)
	limit := parseLimitQuery(r)

	users, next, err := h.listFollowing.Execute(r.Context(), targetSub, cursor, limit)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	writeJSONResponse(w, buildFollowListResponse(users, next), http.StatusOK)
}

// buildFollowListResponse emits a bare publicUserView list (no badges)
// for the follow endpoints. Badge lookups are deferred to future work.
func buildFollowListResponse(users []*domain.User, nextCursor string) followListResponse {
	views := make([]publicUserView, len(users))
	for i, u := range users {
		views[i] = toPublicView(u, nil)
	}
	var next *string
	if nextCursor != "" {
		nc := nextCursor
		next = &nc
	}
	return followListResponse{Data: views, NextCursor: next}
}

// parseOpaqueCursorQuery returns the raw cursor string for endpoints
// whose cursor is not a ULID (e.g. base64url composite cursors for
// follower lists). The repository is responsible for rejecting
// malformed values gracefully.
func parseOpaqueCursorQuery(r *http.Request) *string {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return nil
	}
	return &raw
}
