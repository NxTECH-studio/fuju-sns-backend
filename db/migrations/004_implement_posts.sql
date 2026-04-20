-- Migration: 004_implement_posts.sql
-- Description: Expand the posts table into a full Post feature (threads,
-- likes, tag extraction, image attachments) and retire the legacy comments
-- table in favour of self-referential reply Posts.
--
-- Depends on migration 001 having shipped posts (id/user_id CHAR(26),
-- content TEXT, likes_count, timestamps) and images (CHAR(26) keys).

-- Extend posts: drop the legacy inline image_urls column and add thread /
-- visibility / replies_count columns.
ALTER TABLE posts
    DROP COLUMN IF EXISTS image_urls,
    ADD COLUMN parent_post_id CHAR(26)    NULL REFERENCES posts(id) ON DELETE SET NULL,
    ADD COLUMN root_post_id   CHAR(26)    NULL REFERENCES posts(id) ON DELETE SET NULL,
    ADD COLUMN replies_count  BIGINT      NOT NULL DEFAULT 0,
    ADD COLUMN visibility     VARCHAR(16) NOT NULL DEFAULT 'public';

-- Length bound matching domain.MaxContentLen (120 runes). SQL char_length()
-- counts code points, so the rune check is tight for well-formed UTF-8.
-- The app layer also validates via utf8.RuneCountInString.
ALTER TABLE posts
    ADD CONSTRAINT posts_content_len CHECK (char_length(content) >= 1 AND char_length(content) <= 120);

-- Drop the original posts_comments_count column from migration 001 — replies
-- live on a dedicated counter now and comments themselves are gone.
ALTER TABLE posts DROP COLUMN IF EXISTS comments_count;

-- Cursor-friendly indexes: (user_id, id DESC) for user timelines, id DESC
-- for the global feed. Partial index on non-deleted rows keeps the global
-- feed fast while still allowing tombstone queries against the full table.
CREATE INDEX IF NOT EXISTS idx_posts_user_id_id_desc ON posts(user_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_posts_parent_post_id  ON posts(parent_post_id);
CREATE INDEX IF NOT EXISTS idx_posts_root_post_id    ON posts(root_post_id);
CREATE INDEX IF NOT EXISTS idx_posts_id_desc         ON posts(id DESC) WHERE deleted_at IS NULL;

-- Post <-> Image many-to-many. position is 0..3 (X parity); the unique
-- constraint prevents duplicated slots within a post.
CREATE TABLE post_images (
    post_id  CHAR(26) NOT NULL REFERENCES posts(id)  ON DELETE CASCADE,
    image_id CHAR(26) NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    position SMALLINT NOT NULL,
    PRIMARY KEY (post_id, image_id),
    UNIQUE (post_id, position),
    CHECK (position >= 0 AND position < 4)
);
CREATE INDEX idx_post_images_post_id ON post_images(post_id);

-- Likes are a composite-PK table. (user_id, created_at DESC) powers the
-- "posts I liked recently" query planned for profile screens.
CREATE TABLE likes (
    user_id    CHAR(26)    NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
    post_id    CHAR(26)    NOT NULL REFERENCES posts(id)  ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, post_id)
);
CREATE INDEX idx_likes_post_id             ON likes(post_id);
CREATE INDEX idx_likes_user_id_created_at  ON likes(user_id, created_at DESC);

-- Tags. Names are stored normalized (lower-cased, trimmed) so UNIQUE(name)
-- is meaningful. Tag ingestion is handled by the TagExtractor in the app.
CREATE TABLE tags (
    id         CHAR(26)    PRIMARY KEY,
    name       VARCHAR(64) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE post_tags (
    post_id CHAR(26) NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    tag_id  CHAR(26) NOT NULL REFERENCES tags(id)  ON DELETE CASCADE,
    PRIMARY KEY (post_id, tag_id)
);
CREATE INDEX idx_post_tags_tag_id ON post_tags(tag_id);

-- comments is being collapsed into self-referential post replies. Dev data
-- is disposable; drop the table outright.
DROP TABLE IF EXISTS comments;
