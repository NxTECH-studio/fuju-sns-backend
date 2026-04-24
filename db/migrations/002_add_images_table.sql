-- Migration: 002_add_images_table.sql
-- Description: Images table for R2 storage. IDs are ULIDs generated in the
-- application layer; user_id references users(sub).

CREATE TABLE images (
  id          CHAR(26)      PRIMARY KEY,                              -- ULID, generated in application layer
  storage_key VARCHAR(2000) NOT NULL UNIQUE,
  file_name   VARCHAR(500)  NOT NULL,
  mime_type   VARCHAR(100)  NOT NULL,
  file_size   BIGINT        NOT NULL,
  public_url  VARCHAR(2000) NOT NULL,
  user_id     CHAR(26)      NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  deleted_at  TIMESTAMPTZ   NULL
);

CREATE INDEX idx_images_user_id     ON images(user_id);
CREATE INDEX idx_images_created_at  ON images(created_at DESC);
CREATE INDEX idx_images_storage_key ON images(storage_key);
CREATE INDEX idx_images_deleted_at  ON images(deleted_at);
