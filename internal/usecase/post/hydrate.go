package post

import (
	"context"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/errors"
)

// PostDetail bundles a Post with its images, tags, per-viewer liked flag,
// the author's user row (for display_name / icon_url) and a per-viewer
// following_author flag. Author / FollowingAuthor are populated by the
// Hydrator; GetPostUseCase also fills them.
type PostDetail struct {
	Post            *domain.Post
	Images          []*domain.Image
	Tags            []*domain.Tag
	LikedByViewer   bool
	Author          *domain.User
	FollowingAuthor bool
}

// Hydrator batches the image / tag / like / author / follow lookups
// needed to produce PostDetail slices from a list of Post. It is shared
// by the post list endpoints and the timeline endpoints so the N+1
// avoidance strategy lives in one place.
type Hydrator struct {
	imageRepo  repository.ImageRepository
	tagRepo    repository.TagRepository
	likeRepo   repository.LikeRepository
	userRepo   repository.UserRepository
	followRepo repository.FollowRepository
}

// NewHydrator constructs a Hydrator. All repos are required.
func NewHydrator(
	imageRepo repository.ImageRepository,
	tagRepo repository.TagRepository,
	likeRepo repository.LikeRepository,
	userRepo repository.UserRepository,
	followRepo repository.FollowRepository,
) *Hydrator {
	return &Hydrator{
		imageRepo:  imageRepo,
		tagRepo:    tagRepo,
		likeRepo:   likeRepo,
		userRepo:   userRepo,
		followRepo: followRepo,
	}
}

// Hydrate resolves a page of posts into PostDetail records. Five (or six,
// with a viewer) database calls total, regardless of page size.
func (h *Hydrator) Hydrate(ctx context.Context, posts []*domain.Post, viewerSub *string) ([]*PostDetail, error) {
	if len(posts) == 0 {
		return []*PostDetail{}, nil
	}

	postIDs := make([]string, len(posts))
	authorSet := make(map[string]struct{}, len(posts))
	for i, p := range posts {
		postIDs[i] = p.ID
		authorSet[p.UserID] = struct{}{}
	}
	authorSubs := make([]string, 0, len(authorSet))
	for s := range authorSet {
		authorSubs = append(authorSubs, s)
	}

	imagesByPost, err := h.imageRepo.ListByPostIDs(ctx, postIDs)
	if err != nil {
		return nil, errors.DatabaseError("failed to load images", err)
	}
	tagsByPost, err := h.tagRepo.ListByPostIDs(ctx, postIDs)
	if err != nil {
		return nil, errors.DatabaseError("failed to load tags", err)
	}
	authorsBySub, err := h.userRepo.ListBySubs(ctx, authorSubs)
	if err != nil {
		return nil, errors.DatabaseError("failed to load authors", err)
	}

	var likedByMe map[string]bool
	var followingByMe map[string]bool
	if viewerSub != nil && *viewerSub != "" {
		likedByMe, err = h.likeRepo.ListLikedPostIDsByUser(ctx, *viewerSub, postIDs)
		if err != nil {
			return nil, errors.DatabaseError("failed to load like state", err)
		}
		followingByMe, err = h.followRepo.AreFollowing(ctx, *viewerSub, authorSubs)
		if err != nil {
			return nil, errors.DatabaseError("failed to load follow state", err)
		}
	}

	out := make([]*PostDetail, len(posts))
	for i, p := range posts {
		out[i] = &PostDetail{
			Post:            p,
			Images:          imagesByPost[p.ID],
			Tags:            tagsByPost[p.ID],
			LikedByViewer:   likedByMe[p.ID],
			Author:          authorsBySub[p.UserID],
			FollowingAuthor: followingByMe[p.UserID],
		}
	}
	return out, nil
}

// HydrateOne is a convenience wrapper for endpoints that fetched exactly
// one post (GetPostUseCase). Returns nil when the input slice is empty.
func (h *Hydrator) HydrateOne(ctx context.Context, post *domain.Post, viewerSub *string) (*PostDetail, error) {
	details, err := h.Hydrate(ctx, []*domain.Post{post}, viewerSub)
	if err != nil {
		return nil, err
	}
	if len(details) == 0 {
		return nil, nil
	}
	return details[0], nil
}
