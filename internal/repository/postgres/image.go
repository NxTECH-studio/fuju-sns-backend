package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/fuju/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ImageRepository is the postgres-backed repository.ImageRepository.
// Schema comes from migration 002 (images table); post_images (from
// migration 004) holds the post-to-image ordering used by the
// ListByPost* paths.
type ImageRepository struct {
	pool *pgxpool.Pool
}

// NewImageRepository constructs an ImageRepository bound to pool.
func NewImageRepository(pool *pgxpool.Pool) *ImageRepository {
	return &ImageRepository{pool: pool}
}

const imageSelectColumns = `id, storage_key, file_name, mime_type, file_size,
	public_url, user_id, created_at, updated_at, deleted_at`

// imageJoinColumns mirrors imageSelectColumns with the "i." alias used
// by the post_images JOIN queries. Paired constants avoid a runtime
// helper and match the pattern in badge.go's badgeJoinColumns.
const imageJoinColumns = `i.id, i.storage_key, i.file_name, i.mime_type, i.file_size,
	i.public_url, i.user_id, i.created_at, i.updated_at, i.deleted_at`

func scanImage(row pgx.Row) (*domain.Image, error) {
	var img domain.Image
	err := row.Scan(
		&img.ID,
		&img.StorageKey,
		&img.FileName,
		&img.MimeType,
		&img.FileSize,
		&img.PublicURL,
		&img.UserID,
		&img.CreatedAt,
		&img.UpdatedAt,
		&img.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &img, nil
}

// GetByID returns (nil, nil) for missing or soft-deleted rows.
func (r *ImageRepository) GetByID(ctx context.Context, id string) (*domain.Image, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+imageSelectColumns+`
		FROM images
		WHERE id = $1 AND deleted_at IS NULL`, id)
	img, err := scanImage(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: get image: %w", err)
	}
	return img, nil
}

// GetByUserID returns the user's non-deleted images in creation order
// (newest first). Matches inmemory's "only live rows" behaviour.
func (r *ImageRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.Image, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+imageSelectColumns+`
		FROM images
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: get images by user: %w", err)
	}
	defer rows.Close()

	var out []*domain.Image
	for rows.Next() {
		img, err := scanImage(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan image: %w", err)
		}
		out = append(out, img)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: get images by user iterate: %w", err)
	}
	return out, nil
}

// Create inserts a new image row. created_at / updated_at are stamped
// with the DB clock; callers may leave them zero.
func (r *ImageRepository) Create(ctx context.Context, image *domain.Image) (*domain.Image, error) {
	var createdAtArg any
	if !image.CreatedAt.IsZero() {
		createdAtArg = image.CreatedAt
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO images (id, storage_key, file_name, mime_type, file_size,
		                    public_url, user_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, NOW()), NOW())
		RETURNING `+imageSelectColumns,
		image.ID, image.StorageKey, image.FileName, image.MimeType, image.FileSize,
		image.PublicURL, image.UserID, createdAtArg)
	out, err := scanImage(row)
	if err != nil {
		return nil, fmt.Errorf("postgres: create image: %w", err)
	}
	return out, nil
}

// Delete soft-deletes the image. Re-deleting a soft-deleted row is a
// no-op.
func (r *ImageRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE images SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("postgres: delete image: %w", err)
	}
	return nil
}

// ListByPostID returns images attached to postID ordered by
// post_images.position ASC. Soft-deleted images are filtered so a
// deleted image is invisible even if the post_images link still
// exists.
func (r *ImageRepository) ListByPostID(ctx context.Context, postID string) ([]*domain.Image, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+imageJoinColumns+`
		FROM images i
		JOIN post_images pi ON pi.image_id = i.id
		WHERE pi.post_id = $1 AND i.deleted_at IS NULL
		ORDER BY pi.position ASC`, postID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list images by post: %w", err)
	}
	defer rows.Close()

	var out []*domain.Image
	for rows.Next() {
		img, err := scanImage(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan post image: %w", err)
		}
		out = append(out, img)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list images by post iterate: %w", err)
	}
	return out, nil
}

// ListByPostIDs batches ListByPostID. Posts with no images are absent
// from the returned map; ordering within each post is position ASC.
func (r *ImageRepository) ListByPostIDs(ctx context.Context, postIDs []string) (map[string][]*domain.Image, error) {
	out := make(map[string][]*domain.Image, len(postIDs))
	if len(postIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT pi.post_id, pi.position, `+imageJoinColumns+`
		FROM images i
		JOIN post_images pi ON pi.image_id = i.id
		WHERE pi.post_id = ANY($1) AND i.deleted_at IS NULL
		ORDER BY pi.post_id, pi.position ASC`, postIDs)
	if err != nil {
		return nil, fmt.Errorf("postgres: list images by posts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var postID string
		var position int
		var img domain.Image
		if err := rows.Scan(&postID, &position,
			&img.ID, &img.StorageKey, &img.FileName, &img.MimeType, &img.FileSize,
			&img.PublicURL, &img.UserID, &img.CreatedAt, &img.UpdatedAt, &img.DeletedAt,
		); err != nil {
			return nil, fmt.Errorf("postgres: scan post image: %w", err)
		}
		cp := img
		out[postID] = append(out[postID], &cp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list images by posts iterate: %w", err)
	}
	return out, nil
}
