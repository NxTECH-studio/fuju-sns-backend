-- Migration: 001_initial_schema.sql
-- Description: Initial schema for FUJU backend with AuthCore alignment (ULID-based).
--
-- Identity & auth are owned by AuthCore. The SNS backend caches profile info
-- keyed by AuthCore's sub (ULID, CHAR(26)) and stores only SNS-specific fields
-- (bio, banner_url) as its own source of truth.

-- Users table: mirror cache of AuthCore profile + SNS-specific fields.
CREATE TABLE users (
  sub                   CHAR(26)      PRIMARY KEY,                    -- AuthCore sub (ULID). Source of truth: AuthCore.
  -- Mirror cache of AuthCore profile, refreshed with a 1h TTL.
  display_name_cached   VARCHAR(255)  NOT NULL DEFAULT '',
  display_id_cached     VARCHAR(64)   NOT NULL DEFAULT '',             -- @handle equivalent
  icon_url_cached       VARCHAR(1024) NOT NULL DEFAULT '',
  profile_refreshed_at  TIMESTAMPTZ   NOT NULL DEFAULT '1970-01-01',
  -- SNS-owned profile attributes.
  bio                   TEXT          NOT NULL DEFAULT '',
  banner_url            VARCHAR(1024) NOT NULL DEFAULT '',
  -- SNS-local permission flag. AuthCore does not know about this.
  is_admin              BOOLEAN       NOT NULL DEFAULT false,
  -- Metadata
  created_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  updated_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  deleted_at            TIMESTAMPTZ   NULL
);

CREATE INDEX idx_users_profile_refreshed_at ON users(profile_refreshed_at);
CREATE INDEX idx_users_display_id_cached   ON users(display_id_cached);
CREATE INDEX idx_users_deleted_at          ON users(deleted_at);

-- Posts table: placeholder with ULID keys. Post feature columns are added by
-- later migration (see 02-implement-post-feature).
CREATE TABLE posts (
  id          CHAR(26)    PRIMARY KEY,                                -- ULID, generated in application layer
  user_id     CHAR(26)    NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  content     TEXT        NOT NULL,
  image_urls  JSONB       NOT NULL DEFAULT '[]'::jsonb,
  likes_count INT         NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at  TIMESTAMPTZ NULL
);

CREATE INDEX idx_posts_user_id    ON posts(user_id);
CREATE INDEX idx_posts_created_at ON posts(created_at DESC);
CREATE INDEX idx_posts_deleted_at ON posts(deleted_at);

-- Comments table: retained temporarily with ULID-typed keys. The post feature
-- task (02-implement-post-feature) folds comments into post replies and drops
-- this table entirely.
CREATE TABLE comments (
  id         CHAR(26)    PRIMARY KEY,                                 -- ULID, generated in application layer
  post_id    CHAR(26)    NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id    CHAR(26)    NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  content    TEXT        NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX idx_comments_post_id    ON comments(post_id);
CREATE INDEX idx_comments_user_id    ON comments(user_id);
CREATE INDEX idx_comments_deleted_at ON comments(deleted_at);
