-- Migration: 007_implement_ogp_cache.sql
-- Description: OGP preview cache, post-to-OGP join, and a DB-backed job
-- queue for the background worker. The worker is in-process (single
-- goroutine); SELECT ... FOR UPDATE SKIP LOCKED keeps the claim path safe
-- against future multi-worker setups.

CREATE TABLE ogp_cache (
  url_hash      CHAR(64)      PRIMARY KEY,                 -- SHA256 hex of normalized URL
  url           VARCHAR(2048) NOT NULL,                    -- Normalized URL (kept for debugging)
  title         VARCHAR(512)  NOT NULL DEFAULT '',
  description   VARCHAR(1024) NOT NULL DEFAULT '',
  image_url     VARCHAR(2048) NOT NULL DEFAULT '',
  site_name     VARCHAR(255)  NOT NULL DEFAULT '',
  canonical_url VARCHAR(2048) NOT NULL DEFAULT '',
  fetched_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  expires_at    TIMESTAMPTZ   NOT NULL,
  status        VARCHAR(16)   NOT NULL DEFAULT 'ok',       -- ok / error / pending
  error_reason  VARCHAR(255)  NOT NULL DEFAULT ''
);

CREATE INDEX idx_ogp_cache_expires_at ON ogp_cache(expires_at);

-- Many-to-many between posts and OGP previews. The MVP UI renders only
-- the first preview (position 0, matching the Twitter/X convention), but
-- the schema already supports multiple previews per post for future use.
CREATE TABLE post_ogp (
  post_id  CHAR(26) NOT NULL REFERENCES posts(id)      ON DELETE CASCADE,
  url_hash CHAR(64) NOT NULL REFERENCES ogp_cache(url_hash) ON DELETE RESTRICT,
  position SMALLINT NOT NULL,
  PRIMARY KEY (post_id, url_hash),
  UNIQUE (post_id, position)
);

CREATE INDEX idx_post_ogp_post_id ON post_ogp(post_id);

-- Job queue. post_id is nullable so a re-fetch of an expired cache entry
-- doesn't need to pin a post. ON DELETE SET NULL ensures a deleted post
-- doesn't cascade-abort an in-flight job.
CREATE TABLE ogp_jobs (
  id           CHAR(26)      PRIMARY KEY,
  url_hash     CHAR(64)      NOT NULL,
  url          VARCHAR(2048) NOT NULL,
  post_id      CHAR(26)      NULL REFERENCES posts(id) ON DELETE SET NULL,
  enqueued_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  started_at   TIMESTAMPTZ   NULL,
  finished_at  TIMESTAMPTZ   NULL,
  status       VARCHAR(16)   NOT NULL DEFAULT 'queued',  -- queued / running / done / failed
  attempts     INT           NOT NULL DEFAULT 0,
  last_error   VARCHAR(512)  NOT NULL DEFAULT ''
);

CREATE INDEX idx_ogp_jobs_status_enqueued_at ON ogp_jobs(status, enqueued_at);
