// Package handler provides HTTP request handlers.
package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/fuju/backend/internal/domain"
	postusecase "github.com/fuju/backend/internal/usecase/post"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
)

// maxPostBodyBytes caps the POST /posts JSON body. 120 runes of content
// plus a handful of ULIDs fits in well under a kilobyte; 16 KiB is very
// generous and still blocks abuse.
const maxPostBodyBytes = 16 * 1024

// PostHandler wires the HTTP layer onto the post use cases.
type PostHandler struct {
	getPost     *postusecase.GetPostUseCase
	createPost  *postusecase.CreatePostUseCase
	deletePost  *postusecase.DeletePostUseCase
	listPosts   *postusecase.ListPostsUseCase
	listReplies *postusecase.ListRepliesUseCase
	likePost    *postusecase.LikePostUseCase
	unlikePost  *postusecase.UnlikePostUseCase
}

// NewPostHandler constructs a PostHandler.
func NewPostHandler(
	getPost *postusecase.GetPostUseCase,
	createPost *postusecase.CreatePostUseCase,
	deletePost *postusecase.DeletePostUseCase,
	listPosts *postusecase.ListPostsUseCase,
	listReplies *postusecase.ListRepliesUseCase,
	likePost *postusecase.LikePostUseCase,
	unlikePost *postusecase.UnlikePostUseCase,
) *PostHandler {
	return &PostHandler{
		getPost:     getPost,
		createPost:  createPost,
		deletePost:  deletePost,
		listPosts:   listPosts,
		listReplies: listReplies,
		likePost:    likePost,
		unlikePost:  unlikePost,
	}
}

// postImageView is the image sub-object on a post response.
type postImageView struct {
	ID        string `json:"id"`
	PublicURL string `json:"public_url"`
	Position  int    `json:"position"`
}

// postTagView is the tag sub-object on a post response.
type postTagView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// postAuthorView is the author sub-object on a post response. Only the
// fields the UI needs for an avatar + handle on a feed card.
type postAuthorView struct {
	Sub               string `json:"sub"`
	DisplayNameCached string `json:"display_name_cached"`
	DisplayIDCached   string `json:"display_id_cached"`
	IconURLCached     string `json:"icon_url_cached"`
}

// postOGPView is a single OGP preview card on a post response.
type postOGPView struct {
	URL          string `json:"url"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	ImageURL     string `json:"image_url"`
	SiteName     string `json:"site_name"`
	CanonicalURL string `json:"canonical_url"`
}

// postDetailView is the JSON response shape for a single post (with its
// images, tags, author, OGP previews, and viewer-liked /
// viewer-following flags).
type postDetailView struct {
	ID              string          `json:"id"`
	UserID          string          `json:"user_id"`
	Content         string          `json:"content"`
	ParentPostID    *string         `json:"parent_post_id"`
	RootPostID      *string         `json:"root_post_id"`
	LikesCount      int64           `json:"likes_count"`
	RepliesCount    int64           `json:"replies_count"`
	Visibility      string          `json:"visibility"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
	Images          []postImageView `json:"images"`
	Tags            []postTagView   `json:"tags"`
	Author          *postAuthorView `json:"author"`
	OGPPreviews     []postOGPView   `json:"ogp_previews"`
	LikedByViewer   bool            `json:"liked_by_viewer"`
	FollowingAuthor bool            `json:"following_author"`
}

func toPostDetailView(d *postusecase.PostDetail) postDetailView {
	images := make([]postImageView, len(d.Images))
	for i, img := range d.Images {
		images[i] = postImageView{ID: img.ID, PublicURL: img.PublicURL, Position: i}
	}
	tags := make([]postTagView, len(d.Tags))
	for i, t := range d.Tags {
		tags[i] = postTagView{ID: t.ID, Name: t.Name}
	}
	var author *postAuthorView
	if d.Author != nil {
		author = &postAuthorView{
			Sub:               d.Author.Sub,
			DisplayNameCached: d.Author.DisplayNameCached,
			DisplayIDCached:   d.Author.DisplayIDCached,
			IconURLCached:     d.Author.IconURLCached,
		}
	}
	previews := make([]postOGPView, 0, len(d.OGPPreviews))
	for _, p := range d.OGPPreviews {
		previews = append(previews, postOGPView{
			URL:          p.URL,
			Title:        p.Title,
			Description:  p.Description,
			ImageURL:     p.ImageURL,
			SiteName:     p.SiteName,
			CanonicalURL: p.CanonicalURL,
		})
	}
	return postDetailView{
		ID:              d.Post.ID,
		UserID:          d.Post.UserID,
		Content:         d.Post.Content,
		ParentPostID:    d.Post.ParentPostID,
		RootPostID:      d.Post.RootPostID,
		LikesCount:      d.Post.LikesCount,
		RepliesCount:    d.Post.RepliesCount,
		Visibility:      d.Post.Visibility,
		CreatedAt:       d.Post.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:       d.Post.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		Images:          images,
		Tags:            tags,
		Author:          author,
		OGPPreviews:     previews,
		LikedByViewer:   d.LikedByViewer,
		FollowingAuthor: d.FollowingAuthor,
	}
}

// postListResponse is the list endpoint envelope — a custom shape because
// the cursor pagination has no integer total.
type postListResponse struct {
	Data       []postDetailView `json:"data"`
	NextCursor *string          `json:"next_cursor"`
}

func toPostListResponse(details []*postusecase.PostDetail, nextCursor string) postListResponse {
	views := make([]postDetailView, len(details))
	for i, d := range details {
		views[i] = toPostDetailView(d)
	}
	var next *string
	if nextCursor != "" {
		nc := nextCursor
		next = &nc
	}
	return postListResponse{Data: views, NextCursor: next}
}

// GetPost handles GET /posts/{id}. Viewer sub is optional (public read).
func (h *PostHandler) GetPost(w http.ResponseWriter, r *http.Request) {
	postID, ok := parsePostIDFromPath(w, r)
	if !ok {
		return
	}
	viewerSub := optionalSubFromContext(r.Context())

	detail, err := h.getPost.Execute(r.Context(), postID, viewerSub)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	WriteSuccessResponse(w, toPostDetailView(detail), http.StatusOK)
}

// CreatePost handles POST /posts (also used for replies via parent_post_id).
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

	detail, err := h.createPost.Execute(r.Context(), sub, req)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	WriteSuccessResponse(w, toPostDetailView(detail), http.StatusCreated)
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

// ListPosts handles GET /posts. Cursor pagination; user_id query scopes to
// a single author.
func (h *PostHandler) ListPosts(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseOptionalUserIDQuery(w, r)
	if !ok {
		return
	}
	cursor := parseCursorQuery(r)
	limit := parseLimitQuery(r)
	viewerSub := optionalSubFromContext(r.Context())

	details, next, err := h.listPosts.Execute(r.Context(), userID, cursor, limit, viewerSub)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	writeJSONResponse(w, toPostListResponse(details, next), http.StatusOK)
}

// ListReplies handles GET /posts/{id}/replies.
func (h *PostHandler) ListReplies(w http.ResponseWriter, r *http.Request) {
	postID, ok := parsePostIDFromPath(w, r)
	if !ok {
		return
	}
	cursor := parseCursorQuery(r)
	limit := parseLimitQuery(r)
	viewerSub := optionalSubFromContext(r.Context())

	details, next, err := h.listReplies.Execute(r.Context(), postID, cursor, limit, viewerSub)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}
	writeJSONResponse(w, toPostListResponse(details, next), http.StatusOK)
}

// LikePost handles POST /posts/{id}/like (idempotent).
func (h *PostHandler) LikePost(w http.ResponseWriter, r *http.Request) {
	postID, ok := parsePostIDFromPath(w, r)
	if !ok {
		return
	}
	sub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}
	if err := h.likePost.Execute(r.Context(), sub, postID); err != nil {
		WriteErrorResponse(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UnlikePost handles DELETE /posts/{id}/like (idempotent).
func (h *PostHandler) UnlikePost(w http.ResponseWriter, r *http.Request) {
	postID, ok := parsePostIDFromPath(w, r)
	if !ok {
		return
	}
	sub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}
	if err := h.unlikePost.Execute(r.Context(), sub, postID); err != nil {
		WriteErrorResponse(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parsePostIDFromPath extracts and validates the ULID post ID from URL path.
func parsePostIDFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	return parseULIDFromPath(w, r, "id", "invalid post ID")
}

// parseCreatePostRequest parses and validates the create post request.
func parseCreatePostRequest(w http.ResponseWriter, r *http.Request) (*domain.CreatePostRequest, bool) {
	var req domain.CreatePostRequest
	if err := decodeJSONBody(w, r, &req, maxPostBodyBytes); err != nil {
		WriteErrorResponse(w, err)
		return nil, false
	}
	return &req, true
}

// parseOptionalUserIDQuery extracts an optional user_id ULID from the query
// string. Absence is OK; malformed values surface as 400.
func parseOptionalUserIDQuery(w http.ResponseWriter, r *http.Request) (*string, bool) {
	raw := r.URL.Query().Get("user_id")
	if raw == "" {
		return nil, true
	}
	if !ulidPattern.MatchString(raw) {
		WriteErrorResponse(w, errors.InvalidRequest("invalid user_id (expected ULID)", nil))
		return nil, false
	}
	return &raw, true
}

// parseCursorQuery returns a *string cursor (nil when absent). A malformed
// cursor is silently ignored — list endpoints treat an unparseable cursor
// as "start from the top" rather than 400ing.
func parseCursorQuery(r *http.Request) *string {
	raw := r.URL.Query().Get("cursor")
	if raw == "" || !ulidPattern.MatchString(raw) {
		return nil
	}
	return &raw
}

// parseLimitQuery returns the requested limit (or 0 so the usecase applies
// its default).
func parseLimitQuery(r *http.Request) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return n
}

// optionalSubFromContext returns a non-nil *string only for authenticated
// requests. Public reads call it with no auth context and get nil back.
func optionalSubFromContext(ctx context.Context) *string {
	sub, ok := auth.GetSubFromContext(ctx)
	if !ok {
		return nil
	}
	return &sub
}
