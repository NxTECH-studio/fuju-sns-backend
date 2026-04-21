-- Migration: 006_implement_follow_and_timeline.sql
-- Description: User-to-user follow relations plus denormalized counter
-- columns on users. The timeline endpoints are read-side only — the posts
-- table itself is unchanged.

CREATE TABLE follows (
  follower_sub CHAR(26)    NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  followee_sub CHAR(26)    NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (follower_sub, followee_sub),
  CHECK (follower_sub <> followee_sub)
);

-- Followers of A: rows where followee_sub = A. Composite cursor (created_at
-- DESC, peer DESC) scans benefit from keying all three fields in order.
CREATE INDEX idx_follows_followee_sub_created_at ON follows(followee_sub, created_at DESC, follower_sub DESC);
-- Following list of A: rows where follower_sub = A.
CREATE INDEX idx_follows_follower_sub_created_at ON follows(follower_sub, created_at DESC, followee_sub DESC);

-- Denormalized counters (01 deferred these to this task). Backfill is a
-- no-op at this stage because posts / follows are empty; the triggers for
-- keeping them in sync live in the application layer.
ALTER TABLE users
    ADD COLUMN followers_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN following_count BIGINT NOT NULL DEFAULT 0;
