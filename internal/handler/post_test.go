package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fuju/backend/internal/repository/inmemory"
	"github.com/fuju/backend/internal/tagextractor"
	postusecase "github.com/fuju/backend/internal/usecase/post"
	"github.com/fuju/backend/pkg/auth"
)

const (
	ownerSub   = "01POSTOWNER00000000000000A"
	viewerSub  = "01POSTVIEWER0000000000000B"
	validPost  = "01POSTIDFIXED000000000000C"
	anotherPst = "01POSTIDFIXED000000000000D"
)

func newPostHandlerFixture(t *testing.T) *PostHandler {
	t.Helper()
	links := inmemory.NewLinkStore()
	postRepo := inmemory.NewPostRepository(links)
	imageRepo := inmemory.NewImageRepository(links)
	tagRepo := inmemory.NewTagRepository(links)
	likeRepo := inmemory.NewLikeRepository()

	ex := tagextractor.NewRegexTagExtractor(nil)
	create := postusecase.NewCreatePostUseCase(postRepo, imageRepo, tagRepo, ex)
	get := postusecase.NewGetPostUseCase(postRepo, imageRepo, tagRepo, likeRepo)
	del := postusecase.NewDeletePostUseCase(postRepo)
	list := postusecase.NewListPostsUseCase(postRepo, imageRepo, tagRepo, likeRepo)
	replies := postusecase.NewListRepliesUseCase(postRepo, imageRepo, tagRepo, likeRepo)
	like := postusecase.NewLikePostUseCase(postRepo, likeRepo)
	unlike := postusecase.NewUnlikePostUseCase(postRepo, likeRepo)

	return NewPostHandler(get, create, del, list, replies, like, unlike)
}

func TestCreatePost_RequiresAuth(t *testing.T) {
	h := newPostHandlerFixture(t)
	body := bytes.NewBufferString(`{"content":"hi"}`)
	req := httptest.NewRequest("POST", "/posts", body)
	w := httptest.NewRecorder()
	h.CreatePost(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestDeletePost_NonULIDIs400(t *testing.T) {
	h := newPostHandlerFixture(t)
	req := httptest.NewRequest("DELETE", "/posts/not-a-ulid", nil)
	req.SetPathValue("id", "not-a-ulid")
	req = req.WithContext(auth.SetSubInContext(req.Context(), ownerSub))
	w := httptest.NewRecorder()
	h.DeletePost(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDeletePost_Forbidden(t *testing.T) {
	h := newPostHandlerFixture(t)

	// Create a post owned by ownerSub.
	createReq := httptest.NewRequest("POST", "/posts", bytes.NewBufferString(`{"content":"mine"}`))
	createReq = createReq.WithContext(auth.SetSubInContext(createReq.Context(), ownerSub))
	createW := httptest.NewRecorder()
	h.CreatePost(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", createW.Code, createW.Body.String())
	}
	var createdResp struct {
		Data postDetailView `json:"data"`
	}
	if err := json.Unmarshal(createW.Body.Bytes(), &createdResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	postID := createdResp.Data.ID

	// Another user tries to delete.
	delReq := httptest.NewRequest("DELETE", "/posts/"+postID, nil)
	delReq.SetPathValue("id", postID)
	delReq = delReq.WithContext(auth.SetSubInContext(delReq.Context(), viewerSub))
	delW := httptest.NewRecorder()
	h.DeletePost(delW, delReq)
	if delW.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d (%s)", delW.Code, delW.Body.String())
	}
}

func TestListPosts_CursorShape(t *testing.T) {
	h := newPostHandlerFixture(t)

	// Seed 3 posts.
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/posts", bytes.NewBufferString(`{"content":"p"}`))
		req = req.WithContext(auth.SetSubInContext(req.Context(), ownerSub))
		w := httptest.NewRecorder()
		h.CreatePost(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("seed %d: %d", i, w.Code)
		}
	}

	// Page with limit=2 must include next_cursor.
	listReq := httptest.NewRequest("GET", "/posts?limit=2", nil)
	listReq = listReq.WithContext(context.Background())
	listW := httptest.NewRecorder()
	h.ListPosts(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list: %d", listW.Code)
	}
	var resp postListResponse
	if err := json.Unmarshal(listW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Errorf("expected 2 items, got %d", len(resp.Data))
	}
	if resp.NextCursor == nil || *resp.NextCursor == "" {
		t.Errorf("expected non-empty next_cursor, got %v", resp.NextCursor)
	}
}

func TestGetPost_NonULIDIs400(t *testing.T) {
	h := newPostHandlerFixture(t)
	req := httptest.NewRequest("GET", "/posts/bogus", nil)
	req.SetPathValue("id", "bogus")
	w := httptest.NewRecorder()
	h.GetPost(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestLike_IdempotentViaHandler(t *testing.T) {
	h := newPostHandlerFixture(t)
	// Seed a post.
	req := httptest.NewRequest("POST", "/posts", bytes.NewBufferString(`{"content":"p"}`))
	req = req.WithContext(auth.SetSubInContext(req.Context(), ownerSub))
	w := httptest.NewRecorder()
	h.CreatePost(w, req)
	var created struct {
		Data postDetailView `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id := created.Data.ID

	// Like twice — both should succeed (204).
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "/posts/"+id+"/like", nil)
		r.SetPathValue("id", id)
		r = r.WithContext(auth.SetSubInContext(r.Context(), viewerSub))
		rw := httptest.NewRecorder()
		h.LikePost(rw, r)
		if rw.Code != http.StatusNoContent {
			t.Errorf("like %d: expected 204, got %d", i, rw.Code)
		}
	}
}
