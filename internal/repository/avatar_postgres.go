package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fireflg/gophprofile/internal/domain"
)

const avatarColumns = `id, user_id, file_name, mime_type, size_bytes, s3_key, thumbnail_s3_keys,
		upload_status, processing_status, width, height, created_at, updated_at, deleted_at`

// AvatarRepository - реализация domain.AvatarRepository поверх PostgreSQL.
type AvatarRepository struct {
	pool *pgxpool.Pool
}

// NewAvatarRepository создаёт репозиторий аватарок.
func NewAvatarRepository(pool *pgxpool.Pool) *AvatarRepository {
	return &AvatarRepository{pool: pool}
}

// Create сохраняет метаданные аватарки и заполняет временные метки из БД.
func (r *AvatarRepository) Create(ctx context.Context, avatar *domain.Avatar) error {
	thumbnails, err := marshalThumbnails(avatar.ThumbnailS3Keys)
	if err != nil {
		return err
	}

	const query = `
		INSERT INTO avatars (
			id, user_id, file_name, mime_type, size_bytes, s3_key, thumbnail_s3_keys,
			upload_status, processing_status, width, height
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11)
		RETURNING created_at, updated_at`

	err = r.pool.QueryRow(ctx, query,
		avatar.ID, avatar.UserID, avatar.FileName, avatar.MimeType, avatar.SizeBytes, avatar.S3Key,
		thumbnails, string(avatar.UploadStatus), string(avatar.ProcessingStatus), avatar.Width, avatar.Height,
	).Scan(&avatar.CreatedAt, &avatar.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert avatar: %w", err)
	}

	return nil
}

// GetByID возвращает неудалённую аватарку по идентификатору.
func (r *AvatarRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
	query := `SELECT ` + avatarColumns + ` FROM avatars WHERE id = $1 AND deleted_at IS NULL`

	avatar, err := scanAvatar(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		return nil, fmt.Errorf("select avatar by id: %w", err)
	}

	return avatar, nil
}

// GetLatestByUser возвращает последнюю загруженную аватарку пользователя.
func (r *AvatarRepository) GetLatestByUser(ctx context.Context, userID string) (*domain.Avatar, error) {
	query := `SELECT ` + avatarColumns + ` FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1`

	avatar, err := scanAvatar(r.pool.QueryRow(ctx, query, userID))
	if err != nil {
		return nil, fmt.Errorf("select latest avatar: %w", err)
	}

	return avatar, nil
}

// ListByUser возвращает страницу аватарок пользователя, новые - первыми.
func (r *AvatarRepository) ListByUser(ctx context.Context, userID string, limit, offset int) ([]*domain.Avatar, error) {
	query := `SELECT ` + avatarColumns + ` FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("select user avatars: %w", err)
	}
	defer rows.Close()

	avatars := make([]*domain.Avatar, 0, limit)
	for rows.Next() {
		avatar, err := scanAvatar(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user avatar: %w", err)
		}

		avatars = append(avatars, avatar)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user avatars: %w", err)
	}

	return avatars, nil
}

// SetUploadStatus обновляет статус загрузки файла.
func (r *AvatarRepository) SetUploadStatus(ctx context.Context, id uuid.UUID, status domain.UploadStatus) error {
	const query = `UPDATE avatars SET upload_status = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`

	return r.exec(ctx, "update upload status", query, id, string(status))
}

// SetProcessingStatus обновляет статус асинхронной обработки.
func (r *AvatarRepository) SetProcessingStatus(ctx context.Context, id uuid.UUID, status domain.ProcessingStatus) error {
	const query = `UPDATE avatars SET processing_status = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`

	return r.exec(ctx, "update processing status", query, id, string(status))
}

// SaveProcessingResult сохраняет ключи миниатюр и размеры оригинала, помечая обработку завершённой.
func (r *AvatarRepository) SaveProcessingResult(
	ctx context.Context,
	id uuid.UUID,
	thumbnails map[string]string,
	width, height int,
) error {
	payload, err := marshalThumbnails(thumbnails)
	if err != nil {
		return err
	}

	const query = `UPDATE avatars
		SET thumbnail_s3_keys = $2::jsonb,
			width = $3,
			height = $4,
			processing_status = $5,
			updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`

	return r.exec(ctx, "save processing result", query,
		id, payload, width, height, string(domain.ProcessingStatusReady))
}

// SoftDelete помечает аватарку удалённой; файлы из S3 удаляет воркер.
func (r *AvatarRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	const query = `UPDATE avatars SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`

	return r.exec(ctx, "soft delete avatar", query, id)
}

// TotalStorageBytes - суммарный объём неудалённых аватарок.
func (r *AvatarRepository) TotalStorageBytes(ctx context.Context) (int64, error) {
	const query = `SELECT COALESCE(SUM(size_bytes), 0) FROM avatars WHERE deleted_at IS NULL`

	var total int64
	if err := r.pool.QueryRow(ctx, query).Scan(&total); err != nil {
		return 0, fmt.Errorf("total storage bytes: %w", err)
	}

	return total, nil
}

// Ping проверяет доступность БД (используется в /health).
func (r *AvatarRepository) Ping(ctx context.Context) error {
	if err := r.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	return nil
}

func (r *AvatarRepository) exec(ctx context.Context, op, query string, args ...any) error {
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrAvatarNotFound
	}

	return nil
}

// row абстрагирует pgx.Row и pgx.Rows для общего сканера.
type row interface {
	Scan(dest ...any) error
}

func scanAvatar(r row) (*domain.Avatar, error) {
	var (
		avatar     domain.Avatar
		thumbnails []byte
		width      *int
		height     *int
	)

	err := r.Scan(
		&avatar.ID,
		&avatar.UserID,
		&avatar.FileName,
		&avatar.MimeType,
		&avatar.SizeBytes,
		&avatar.S3Key,
		&thumbnails,
		&avatar.UploadStatus,
		&avatar.ProcessingStatus,
		&width,
		&height,
		&avatar.CreatedAt,
		&avatar.UpdatedAt,
		&avatar.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrAvatarNotFound
		}

		return nil, err
	}

	if width != nil {
		avatar.Width = *width
	}
	if height != nil {
		avatar.Height = *height
	}

	if len(thumbnails) > 0 {
		if err := json.Unmarshal(thumbnails, &avatar.ThumbnailS3Keys); err != nil {
			return nil, fmt.Errorf("unmarshal thumbnail keys: %w", err)
		}
	}
	if avatar.ThumbnailS3Keys == nil {
		avatar.ThumbnailS3Keys = map[string]string{}
	}

	return &avatar, nil
}

func marshalThumbnails(thumbnails map[string]string) (string, error) {
	if thumbnails == nil {
		thumbnails = map[string]string{}
	}

	payload, err := json.Marshal(thumbnails)
	if err != nil {
		return "", fmt.Errorf("marshal thumbnail keys: %w", err)
	}

	return string(payload), nil
}
