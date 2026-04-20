package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/inmemory"
	adminusecase "github.com/fuju/backend/internal/usecase/admin"
	badgeusecase "github.com/fuju/backend/internal/usecase/badge"
	userusecase "github.com/fuju/backend/internal/usecase/user"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/response"
)

// Subs must be 26-char Crockford Base32 (no I/L/O/U) to pass the handler's
// ULID format check.
const (
	testAdminSub = "01ADMNADMNADMNADMNADMNADMN"
	testUserSub  = "01BCDEFGHJKMNPQRSTVWXYZ234"
)

type badgeHandlerFixture struct {
	t            *testing.T
	userHandler  *UserHandler
	badgeHandler *BadgeHandler
}

func newBadgeHandlerFixture(t *testing.T, withAdmin bool) *badgeHandlerFixture {
	t.Helper()
	ctx := context.Background()

	userRepo := inmemory.NewUserRepository()
	if _, err := userRepo.Upsert(ctx, &domain.User{Sub: testAdminSub, IsAdmin: withAdmin}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if _, err := userRepo.Upsert(ctx, &domain.User{Sub: testUserSub}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	badgeRepo := inmemory.NewBadgeRepository()
	if _, err := badgeRepo.Create(ctx, &domain.Badge{
		ID: "01BDEV0000000000000000000A", Key: "developer", Label: "dev", Color: "gold", Priority: 5,
	}); err != nil {
		t.Fatalf("seed badge: %v", err)
	}

	checker := adminusecase.NewChecker(userRepo)
	grantUC := badgeusecase.NewGrantBadgeUseCase(badgeRepo, userRepo, checker)
	revokeUC := badgeusecase.NewRevokeBadgeUseCase(badgeRepo, checker)
	listUC := badgeusecase.NewListBadgesUseCase(badgeRepo)
	createUC := badgeusecase.NewCreateBadgeUseCase(badgeRepo, checker)
	updateUC := badgeusecase.NewUpdateBadgeUseCase(badgeRepo, checker)
	getUserBadges := badgeusecase.NewGetUserBadgesUseCase(badgeRepo)
	batchUserBadges := badgeusecase.NewListUserBadgesBatchUseCase(badgeRepo)

	getUserUC := userusecase.NewGetUserUseCase(userRepo)

	return &badgeHandlerFixture{
		t:            t,
		userHandler:  NewUserHandler(getUserUC, nil, nil, nil, getUserBadges, batchUserBadges),
		badgeHandler: NewBadgeHandler(listUC, createUC, updateUC, grantUC, revokeUC),
	}
}

func TestGrantBadge_NonAdminGets403(t *testing.T) {
	f := newBadgeHandlerFixture(t, false)

	body := bytes.NewBufferString(`{"badge_key":"developer"}`)
	req := httptest.NewRequest("POST", "/v1/admin/users/"+testUserSub+"/badges", body)
	req = req.WithContext(auth.SetSubInContext(req.Context(), testAdminSub))
	req.SetPathValue("sub", testUserSub)
	w := httptest.NewRecorder()

	f.badgeHandler.GrantBadge(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGrantBadge_AdminThenGetUser_ReflectsBadge(t *testing.T) {
	f := newBadgeHandlerFixture(t, true)

	// Admin grants developer to testUserSub.
	body := bytes.NewBufferString(`{"badge_key":"developer"}`)
	req := httptest.NewRequest("POST", "/v1/admin/users/"+testUserSub+"/badges", body)
	req = req.WithContext(auth.SetSubInContext(req.Context(), testAdminSub))
	req.SetPathValue("sub", testUserSub)
	w := httptest.NewRecorder()
	f.badgeHandler.GrantBadge(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("grant: expected 201, got %d (body=%s)", w.Code, w.Body.String())
	}

	// GET /users/{sub} should now include developer in badges.
	getReq := httptest.NewRequest("GET", "/users/"+testUserSub, nil)
	getReq.SetPathValue("sub", testUserSub)
	getW := httptest.NewRecorder()
	f.userHandler.GetUser(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("get user: expected 200, got %d (body=%s)", getW.Code, getW.Body.String())
	}

	var resp response.SuccessResponse
	if err := json.Unmarshal(getW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected user object in data, got %T", resp.Data)
	}
	badges, ok := data["badges"].([]interface{})
	if !ok {
		t.Fatalf("expected badges array, got %T", data["badges"])
	}
	if len(badges) != 1 {
		t.Fatalf("expected 1 badge, got %d", len(badges))
	}
	first := badges[0].(map[string]interface{})
	if first["key"] != "developer" {
		t.Errorf("expected developer badge, got %v", first["key"])
	}
}

func TestGetUser_BadgesKeyAlwaysPresent(t *testing.T) {
	f := newBadgeHandlerFixture(t, true)

	req := httptest.NewRequest("GET", "/users/"+testUserSub, nil)
	req.SetPathValue("sub", testUserSub)
	w := httptest.NewRecorder()
	f.userHandler.GetUser(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get user: %d body=%s", w.Code, w.Body.String())
	}
	// The `badges` JSON field must exist even when empty, not be omitted.
	if !bytes.Contains(w.Body.Bytes(), []byte(`"badges":[]`)) {
		t.Errorf("expected empty badges array in response, got %s", w.Body.String())
	}
}
