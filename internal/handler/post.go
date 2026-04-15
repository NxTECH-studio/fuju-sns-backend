// Package handler - Post handlers
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

// PostHandler contains handlers for post endpoints
type PostHandler struct {
getPost    *postusecase.GetPostUseCase
createPost *postusecase.CreatePostUseCase
deletePost *postusecase.DeletePostUseCase
listPosts  *postusecase.ListPostsUseCase
}

// NewPostHandler creates a new PostHandler
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

// GetPost handles GET /posts/{id}
func (h *PostHandler) GetPost(w http.ResponseWriter, r *http.Request) {
postIDStr := r.PathValue("id")
postID, err := strconv.ParseInt(postIDStr, 10, 64)
if err != nil {
WriteErrorResponse(w, errors.InvalidRequest("invalid post ID", err))
return
}

post, err := h.getPost.Execute(r.Context(), postID)
if err != nil {
WriteErrorResponse(w, err)
return
}

WriteSuccessResponse(w, post, http.StatusOK)
}

// CreatePost handles POST /posts
func (h *PostHandler) CreatePost(w http.ResponseWriter, r *http.Request) {
userID, ok := auth.GetUserIDFromContext(r.Context())
if !ok {
WriteErrorResponse(w, errors.Unauthorized("authentication required"))
return
}

var req domain.CreatePostRequest
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
WriteErrorResponse(w, errors.InvalidRequest("invalid request body", err))
return
}

post, err := h.createPost.Execute(r.Context(), userID, &req)
if err != nil {
WriteErrorResponse(w, err)
return
}

WriteSuccessResponse(w, post, http.StatusCreated)
}

// DeletePost handles DELETE /posts/{id}
func (h *PostHandler) DeletePost(w http.ResponseWriter, r *http.Request) {
postIDStr := r.PathValue("id")
postID, err := strconv.ParseInt(postIDStr, 10, 64)
if err != nil {
WriteErrorResponse(w, errors.InvalidRequest("invalid post ID", err))
return
}

userID, ok := auth.GetUserIDFromContext(r.Context())
if !ok {
WriteErrorResponse(w, errors.Unauthorized("authentication required"))
return
}

if err := h.deletePost.Execute(r.Context(), postID, userID); err != nil {
		WriteErrorResponse(w, err)
}

w.WriteHeader(http.StatusNoContent)
}

// ListPosts handles GET /posts
func (h *PostHandler) ListPosts(w http.ResponseWriter, r *http.Request) {
limit := 20
offset := 0
var userID *int64

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
if parsed, err := strconv.ParseInt(u, 10, 64); err == nil {
userID = &parsed
}
}

posts, total, err := h.listPosts.Execute(r.Context(), userID, limit, offset)
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
