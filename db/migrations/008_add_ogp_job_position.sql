-- Migration: 008_add_ogp_job_position.sql
-- Description: Carry the per-post URL position on OGP jobs so the worker
-- can attach the fetched preview at the same slot the enqueuer reserved.
-- Without this, posts with 2+ URLs racing through the queue all land at
-- position=0 and lose all but one attachment to UNIQUE(post_id, position).

ALTER TABLE ogp_jobs
  ADD COLUMN position SMALLINT NOT NULL DEFAULT 0;
