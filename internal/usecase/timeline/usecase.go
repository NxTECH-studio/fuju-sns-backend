// Package timeline contains home / user / global timeline use cases. All
// three return the same post.Detail shape, differing only in the source
// set of posts.
package timeline

import (
	"context"

	"github.com/fuju/backend/internal/repository"
	postusecase "github.com/fuju/backend/internal/usecase/post"
	"github.com/fuju/backend/pkg/errors"
)

// HomeTimelineUseCase returns posts from the users the caller follows,
// plus the caller's own posts, newest first.
type HomeTimelineUseCase struct {
	postRepo   repository.PostRepository
	followRepo repository.FollowRepository
	hydrator   *postusecase.Hydrator
}

// NewHomeTimelineUseCase constructs a HomeTimelineUseCase.
func NewHomeTimelineUseCase(
	postRepo repository.PostRepository,
	followRepo repository.FollowRepository,
	hydrator *postusecase.Hydrator,
) *HomeTimelineUseCase {
	return &HomeTimelineUseCase{postRepo: postRepo, followRepo: followRepo, hydrator: hydrator}
}

// Execute materializes the home timeline for mySub. Fan-out on read: one
// query for the followee set, one for the post page, plus the hydrator's
// batch calls.
func (uc *HomeTimelineUseCase) Execute(ctx context.Context, mySub string, cursor *string, limit int) ([]*postusecase.Detail, string, error) {
	if mySub == "" {
		return nil, "", errors.Unauthorized("authentication required")
	}
	limit = postusecase.NormalizeLimit(limit)

	followees, err := uc.followRepo.ListFollowingSubs(ctx, mySub)
	if err != nil {
		return nil, "", errors.DatabaseError("failed to list follows", err)
	}
	// Always include self so the user sees their own posts in the feed.
	// Allocate a fresh slice so we do not mutate the backing array of
	// followees (which came from the repository and may be reused).
	authorSubs := make([]string, 0, len(followees)+1)
	authorSubs = append(authorSubs, followees...)
	authorSubs = append(authorSubs, mySub)

	posts, nextCursor, err := uc.postRepo.ListByUserIDs(ctx, authorSubs, cursor, limit)
	if err != nil {
		return nil, "", errors.DatabaseError("failed to list posts", err)
	}
	viewer := mySub
	details, err := uc.hydrator.Hydrate(ctx, posts, &viewer)
	if err != nil {
		return nil, "", err
	}
	return details, nextCursor, nil
}

// UserTimelineUseCase returns a target user's own posts (top-level only,
// matching the main feed filter). Public read — viewerSub may be nil.
type UserTimelineUseCase struct {
	postRepo repository.PostRepository
	hydrator *postusecase.Hydrator
}

// NewUserTimelineUseCase constructs a UserTimelineUseCase.
func NewUserTimelineUseCase(postRepo repository.PostRepository, hydrator *postusecase.Hydrator) *UserTimelineUseCase {
	return &UserTimelineUseCase{postRepo: postRepo, hydrator: hydrator}
}

// Execute returns posts authored by targetSub.
func (uc *UserTimelineUseCase) Execute(ctx context.Context, targetSub string, cursor *string, limit int, viewerSub *string) ([]*postusecase.Detail, string, error) {
	if targetSub == "" {
		return nil, "", errors.InvalidRequest("sub is required", nil)
	}
	limit = postusecase.NormalizeLimit(limit)
	posts, nextCursor, err := uc.postRepo.List(ctx, &targetSub, cursor, limit)
	if err != nil {
		return nil, "", errors.DatabaseError("failed to list posts", err)
	}
	details, err := uc.hydrator.Hydrate(ctx, posts, viewerSub)
	if err != nil {
		return nil, "", err
	}
	return details, nextCursor, nil
}

// GlobalTimelineUseCase returns the most recent public posts across all
// users (the "explore" feed).
type GlobalTimelineUseCase struct {
	postRepo repository.PostRepository
	hydrator *postusecase.Hydrator
}

// NewGlobalTimelineUseCase constructs a GlobalTimelineUseCase.
func NewGlobalTimelineUseCase(postRepo repository.PostRepository, hydrator *postusecase.Hydrator) *GlobalTimelineUseCase {
	return &GlobalTimelineUseCase{postRepo: postRepo, hydrator: hydrator}
}

// Execute returns the newest posts globally.
func (uc *GlobalTimelineUseCase) Execute(ctx context.Context, cursor *string, limit int, viewerSub *string) ([]*postusecase.Detail, string, error) {
	limit = postusecase.NormalizeLimit(limit)
	posts, nextCursor, err := uc.postRepo.List(ctx, nil, cursor, limit)
	if err != nil {
		return nil, "", errors.DatabaseError("failed to list posts", err)
	}
	details, err := uc.hydrator.Hydrate(ctx, posts, viewerSub)
	if err != nil {
		return nil, "", err
	}
	return details, nextCursor, nil
}
