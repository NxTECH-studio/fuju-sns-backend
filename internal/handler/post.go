// Package handler provides HTTP request handlers.
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/fuju/backend/internal/domain"
	postusecase "github.com/fuju/backend/internal/usecase/post"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
	"github.com/fuju/backend/pkg/response"
)

// PostHandler contains handlers for post endpoints.
type PostHandler struct {
	getPost    *postusecase.GetPostUseCase
	createPost *postusecase.CreatePostUseCase
	deletePost *postusecase.DeletePostUseCase
	listPosts  *postusecase.ListPostsUseCase
}

// NewPostHandler creates a new PostHandler.
func NewPostHandler(
	getPost *postusecase.GetPostUseCase,
	createPost *postusecase.CreatePostUseCase,
	deletePost *postusecase.DeletePostUseCase,
	listPosts *postusecase.ListPostsUseCase,
) *PostHandler {
	return &PostHandler{
		getPost:    getPost,
		createPost: createPost,
		deletePost: deletePost,
		listPosts:  listPosts,
	}
}

// GetPost handles GET /posts/{id}.
func (h *PostHandler) GetPost(w http.ResponseWriter, r *http.Request) {
	postID, ok := parsePostIDFromPath(w, r)
	if !ok {
		return
	}

	post, err := h.getPost.Execute(r.Context(), postID)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, post, http.StatusOK)
}

// parsePostIDFromPath extracts and validates the ULID post ID from URL path.
func parsePostIDFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	return parseULIDFromPath(w, r, "id", "invalid post ID")
}

// CreatePost handles POST /posts.
func (h *PostHandler) CreatePost(w http.ResponseWriter, r *http.Request) {
	sub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	req, ok := parseCreatePostRequest(w, r)
	if !ok {
		return
	}

	post, err := h.createPost.Execute(r.Context(), sub, req)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, post, http.StatusCreated)
}

// parseCreatePostRequest parses and validates the create post request.
func parseCreatePostRequest(w http.ResponseWriter, r *http.Request) (*domain.CreatePostRequest, bool) {
	var req domain.CreatePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid request body", err))
		return nil, false
	}
	return &req, true
}

// DeletePost handles DELETE /posts/{id}.
func (h *PostHandler) DeletePost(w http.ResponseWriter, r *http.Request) {
	postID, ok := parsePostIDFromPath(w, r)
	if !ok {
		return
	}

	sub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	if err := h.deletePost.Execute(r.Context(), postID, sub); err != nil {
		WriteErrorResponse(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListPosts handles GET /posts.
func (h *PostHandler) ListPosts(w http.ResponseWriter, r *http.Request) {
	limit, offset, userSub, ok := parseListPostsParams(w, r)
	if !ok {
		return
	}

	posts, total, err := h.listPosts.Execute(r.Context(), userSub, limit, offset)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	resp := response.ListResponse{
		Data:   posts,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}

	WriteListResponse(w, &resp, http.StatusOK)
}

// parseListPostsParams extracts limit, offset, and optional user_id (ULID)
// from query parameters. Invalid user_id values surface as 400 rather than
// silently being ignored.
func parseListPostsParams(w http.ResponseWriter, r *http.Request) (int, int, *string, bool) {
	limit := 20
	offset := 0
	var userID *string

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	if u := r.URL.Query().Get("user_id"); u != "" {
		if !ulidPattern.MatchString(u) {
			WriteErrorResponse(w, errors.InvalidRequest("invalid user_id (expected ULID)", nil))
			return 0, 0, nil, false
		}
		val := u
		userID = &val
	}

	return limit, offset, userID, true
}
