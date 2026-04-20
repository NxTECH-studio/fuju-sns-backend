// Package handler provides HTTP request handlers.
package handler

import (
	"net/http"

	"github.com/fuju/backend/internal/domain"
	badgeusecase "github.com/fuju/backend/internal/usecase/badge"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
)

// maxAdminBodyBytes caps admin JSON body size. Admin endpoints are
// low-volume and carry only short text / URLs — a MB is generous.
const maxAdminBodyBytes = 1 << 20 // 1 MiB

// decodeAdminBody is a thin wrapper over decodeJSONBody that uses the
// admin-specific size cap.
func decodeAdminBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	return decodeJSONBody(w, r, dst, maxAdminBodyBytes)
}

// BadgeHandler contains handlers for admin badge endpoints.
type BadgeHandler struct {
	listBadges  *badgeusecase.ListBadgesUseCase
	createBadge *badgeusecase.CreateBadgeUseCase
	updateBadge *badgeusecase.UpdateBadgeUseCase
	grantBadge  *badgeusecase.GrantBadgeUseCase
	revokeBadge *badgeusecase.RevokeBadgeUseCase
}

// NewBadgeHandler constructs a BadgeHandler.
func NewBadgeHandler(
	listBadges *badgeusecase.ListBadgesUseCase,
	createBadge *badgeusecase.CreateBadgeUseCase,
	updateBadge *badgeusecase.UpdateBadgeUseCase,
	grantBadge *badgeusecase.GrantBadgeUseCase,
	revokeBadge *badgeusecase.RevokeBadgeUseCase,
) *BadgeHandler {
	return &BadgeHandler{
		listBadges:  listBadges,
		createBadge: createBadge,
		updateBadge: updateBadge,
		grantBadge:  grantBadge,
		revokeBadge: revokeBadge,
	}
}

// badgeView is the JSON shape used both in admin responses and the public
// `badges` field embedded on user responses.
type badgeView struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	IconURL     string `json:"icon_url"`
	Color       string `json:"color"`
	Priority    int32  `json:"priority"`
}

func toBadgeView(b *domain.Badge) badgeView {
	return badgeView{
		ID:          b.ID,
		Key:         b.Key,
		Label:       b.Label,
		Description: b.Description,
		IconURL:     b.IconURL,
		Color:       b.Color,
		Priority:    b.Priority,
	}
}

func toBadgeViews(badges []*domain.Badge) []badgeView {
	views := make([]badgeView, len(badges))
	for i, b := range badges {
		views[i] = toBadgeView(b)
	}
	return views
}

// ListBadges handles GET /v1/admin/badges — returns the master list.
func (h *BadgeHandler) ListBadges(w http.ResponseWriter, r *http.Request) {
	badges, err := h.listBadges.Execute(r.Context())
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	WriteSuccessResponse(w, toBadgeViews(badges), http.StatusOK)
}

// createBadgeRequest is the payload for POST /v1/admin/badges.
type createBadgeRequest struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	Color       string `json:"color"`
	Priority    int32  `json:"priority"`
}

// CreateBadge handles POST /v1/admin/badges.
func (h *BadgeHandler) CreateBadge(w http.ResponseWriter, r *http.Request) {
	adminSub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	var req createBadgeRequest
	if err := decodeAdminBody(w, r, &req); err != nil {
		WriteErrorResponse(w, err)
		return
	}

	badge := &domain.Badge{
		Key:         req.Key,
		Label:       req.Label,
		Description: req.Description,
		IconURL:     req.IconURL,
		Color:       req.Color,
		Priority:    req.Priority,
	}

	created, err := h.createBadge.Execute(r.Context(), adminSub, badge)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	WriteSuccessResponse(w, toBadgeView(created), http.StatusCreated)
}

// updateBadgeRequest is the payload for PUT /v1/admin/badges/{id}. Key is
// not mutable through this endpoint.
type updateBadgeRequest struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	Color       string `json:"color"`
	Priority    int32  `json:"priority"`
}

// UpdateBadge handles PUT /v1/admin/badges/{id}.
func (h *BadgeHandler) UpdateBadge(w http.ResponseWriter, r *http.Request) {
	badgeID, ok := parseULIDFromPath(w, r, "id", "invalid badge id (expected ULID)")
	if !ok {
		return
	}
	adminSub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	var req updateBadgeRequest
	if err := decodeAdminBody(w, r, &req); err != nil {
		WriteErrorResponse(w, err)
		return
	}

	patch := &domain.Badge{
		Label:       req.Label,
		Description: req.Description,
		IconURL:     req.IconURL,
		Color:       req.Color,
		Priority:    req.Priority,
	}

	updated, err := h.updateBadge.Execute(r.Context(), adminSub, badgeID, patch)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	WriteSuccessResponse(w, toBadgeView(updated), http.StatusOK)
}

// GrantBadge handles POST /v1/admin/users/{sub}/badges.
func (h *BadgeHandler) GrantBadge(w http.ResponseWriter, r *http.Request) {
	targetSub, ok := parseSubFromPath(w, r, "sub")
	if !ok {
		return
	}
	adminSub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	var req domain.GrantBadgeRequest
	if err := decodeAdminBody(w, r, &req); err != nil {
		WriteErrorResponse(w, err)
		return
	}

	badge, err := h.grantBadge.Execute(r.Context(), adminSub, targetSub, &req)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	resp := map[string]interface{}{
		"status":  "granted",
		"user_id": targetSub,
		"badge":   toBadgeView(badge),
	}
	WriteSuccessResponse(w, resp, http.StatusCreated)
}

// RevokeBadge handles DELETE /v1/admin/users/{sub}/badges/{badge_id}.
func (h *BadgeHandler) RevokeBadge(w http.ResponseWriter, r *http.Request) {
	targetSub, ok := parseSubFromPath(w, r, "sub")
	if !ok {
		return
	}
	badgeID, ok := parseULIDFromPath(w, r, "badge_id", "invalid badge id (expected ULID)")
	if !ok {
		return
	}
	adminSub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}
	if err := h.revokeBadge.Execute(r.Context(), adminSub, targetSub, badgeID); err != nil {
		WriteErrorResponse(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
