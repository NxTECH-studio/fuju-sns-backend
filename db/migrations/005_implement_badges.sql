-- Migration: 005_implement_badges.sql
-- Description: Badges master + user_badges join table for admin-granted
-- profile decorations (verified_celebrity, developer). Includes MVP seed
-- data and the initial admin promotion.

CREATE TABLE badges (
  id          CHAR(26)      PRIMARY KEY,                    -- ULID, generated in application layer
  key         VARCHAR(64)   NOT NULL UNIQUE,                 -- Machine-readable key (e.g. "developer")
  label       VARCHAR(64)   NOT NULL,
  description TEXT          NOT NULL DEFAULT '',
  icon_url    VARCHAR(1024) NOT NULL DEFAULT '',
  color       VARCHAR(16)   NOT NULL DEFAULT '',
  priority    INT           NOT NULL DEFAULT 100,
  created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE TABLE user_badges (
  user_id    CHAR(26)     NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  badge_id   CHAR(26)     NOT NULL REFERENCES badges(id) ON DELETE CASCADE,
  granted_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  granted_by CHAR(26)     NOT NULL,                         -- Admin sub that granted the badge
  expires_at TIMESTAMPTZ  NULL,
  reason     VARCHAR(255) NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, badge_id)
);

CREATE INDEX idx_user_badges_user_id    ON user_badges(user_id);
CREATE INDEX idx_user_badges_badge_id   ON user_badges(badge_id);
CREATE INDEX idx_user_badges_expires_at ON user_badges(expires_at) WHERE expires_at IS NOT NULL;

-- MVP seed: developer (gold) is top-priority, verified_celebrity (blue) next.
-- Seed IDs are fixed Crockford Base32 ULIDs (0-9, A-Z without I/L/O/U, first
-- char 0-7). They encode no real timestamp — reissue if a timestamped ULID
-- is required operationally.
INSERT INTO badges (id, key, label, description, color, priority) VALUES
  ('01HXBADGE00000000000000DEV', 'developer',          '開発者',         'Fuju の開発者',              'gold', 5),
  ('01HXBADGE0000000000VER1FY0', 'verified_celebrity', '著名人認証済み', '運営が本人性を確認した著名人', 'blue', 10);

-- Initial admin promotion.
-- TODO: replace '01HXADMIN00000000000000000' with the real AuthCore sub per
-- environment (dev / stg / prod) before applying. If the admin's users row
-- has not been lazy-created yet, this UPDATE matches 0 rows — re-run after
-- their first login.
UPDATE users SET is_admin = true WHERE sub = '01HXADMIN00000000000000000';
