// fuju_hooks.go: closures bridging post / like / follow use case
// commit hooks to the fuju dispatcher. When the dispatcher is nil
// (FUJU_MODEL_BASE_URL unset) every hook is a no-op so the rest of
// main.go can wire them unconditionally.

package main

import (
	"context"
	"time"

	"github.com/fuju/backend/internal/domain"
	followusecase "github.com/fuju/backend/internal/usecase/follow"
	fujuusecase "github.com/fuju/backend/internal/usecase/fuju"
	postusecase "github.com/fuju/backend/internal/usecase/post"
	"github.com/fuju/backend/pkg/fujumodel"
)

// composeCreatePostHooks chains multiple post-create hooks. nil hooks
// are skipped — convenient when the fuju integration is disabled.
func composeCreatePostHooks(hooks ...postusecase.CommitHook) postusecase.CommitHook {
	return func(ctx context.Context, post *domain.Post) {
		for _, h := range hooks {
			if h != nil {
				h(ctx, post)
			}
		}
	}
}

// fujuPostCommitHook turns a Post create into:
//
//   - root post     → /contents registration only
//   - reply post    → /contents registration + a `comment` event whose
//     item_id targets the parent post
//
// SNS does not pre-extract hashtags / entities — fuju does that in
// its daily text_embed step (RFC-LT-003 Option D). We pass the raw
// Content body through.
func fujuPostCommitHook(d *fujuusecase.Dispatcher) postusecase.CommitHook {
	if d == nil {
		return nil
	}
	return func(ctx context.Context, post *domain.Post) {
		if post == nil {
			return
		}
		text := post.Content
		createdAt := post.CreatedAt
		d.EnqueueContent(ctx, fujumodel.Content{
			ContentID: post.ID,
			AuthorID:  post.UserID,
			Text:      &text,
			CreatedAt: &createdAt,
		})
		if post.ParentPostID != nil {
			d.EnqueueEvent(ctx, fujumodel.Event{
				UserID:    post.UserID,
				ItemID:    *post.ParentPostID,
				EventType: fujumodel.EventComment,
				Timestamp: post.CreatedAt,
				Text:      &text,
			})
		}
	}
}

// fujuLikeCommitHook turns a fresh Like (state 0→1) into a `like`
// event keyed on the post id.
func fujuLikeCommitHook(d *fujuusecase.Dispatcher) postusecase.LikeHook {
	if d == nil {
		return nil
	}
	return func(ctx context.Context, userSub, postID string) {
		d.EnqueueEvent(ctx, fujumodel.Event{
			UserID:    userSub,
			ItemID:    postID,
			EventType: fujumodel.EventLike,
			Timestamp: time.Now(),
		})
	}
}

// fujuFollowCommitHook turns a fresh Follow (state 0→1) into a
// `follow` event. RFC-LT-003 §5.9: item_id carries the followee's
// user_id, not a post id, for follow events.
func fujuFollowCommitHook(d *fujuusecase.Dispatcher) followusecase.CommitHook {
	if d == nil {
		return nil
	}
	return func(ctx context.Context, followerSub, followeeSub string) {
		d.EnqueueEvent(ctx, fujumodel.Event{
			UserID:    followerSub,
			ItemID:    followeeSub,
			EventType: fujumodel.EventFollow,
			Timestamp: time.Now(),
		})
	}
}
