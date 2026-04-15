// Package handler - Comment handlers
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/fuju/backend/internal/domain"
	commentusecase "github.com/fuju/backend/internal/usecase/comment"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
)

// CommentHandlerImpl contains handlers for comment endpoints
type CommentHandlerImpl struct {
	addComment    *commentusecase.AddCommentUseCase
	deleteComment *commentusecase.DeleteCommentUseCase
}

// NewCommentHandlerImpl creates a new CommentHandlerImpl
func NewCommentHandlerImpl(
	addComment *commentusecase.AddCommentUseCase,
	deleteComment *commentusecase.DeleteCommentUseCase,
) *CommentHandlerImpl {
	return &CommentHandlerImpl{
		addComment:    addComment,
		deleteComment: deleteComment,
	}
}

// AddComment handles POST /posts/{id}/comments
func (h *CommentHandlerImpl) AddComment(w http.ResponseWriter, r *http.Request) {
	postID, ok := parsePostIDFromPath(w, r)
	if !ok {
		return
	}

	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	req, ok := parseCreateCommentRequest(w, r)
	if !ok {
		return
	}

	comment, err := h.addComment.Execute(r.Context(), postID, userID, req)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, comment, http.StatusCreated)
}

// parseCreateCommentRequest parses and validates the create comment request
func parseCreateCommentRequest(w http.ResponseWriter, r *http.Request) (*domain.CreateCommentRequest, bool) {
	var req domain.CreateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid request body", err))
		return nil, false
	}
	return &req, true
}

// DeleteComment handles DELETE /posts/{post_id}/comments/{comment_id}
func (h *CommentHandlerImpl) DeleteComment(w http.ResponseWriter, r *http.Request) {
	commentID, ok := parseCommentIDFromPath(w, r)
	if !ok {
		return
	}

	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	if err := h.deleteComment.Execute(r.Context(), commentID, userID); err != nil {
		WriteErrorResponse(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parseCommentIDFromPath extracts and parses the comment ID from URL path
func parseCommentIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	commentIDStr := r.PathValue("comment_id")
	commentID, err := strconv.ParseInt(commentIDStr, 10, 64)
	if err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid comment ID", err))
		return 0, false
	}
	return commentID, true
}
