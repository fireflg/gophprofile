package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/internal/metrics"
	"github.com/fireflg/gophprofile/pkg/imageutil"
	"github.com/fireflg/gophprofile/pkg/logger"
)

// sniffSize - сколько байт читаем для определения MIME-типа.
const sniffSize = 512

// maxPublishInFlight - предел одновременных фоновых публикаций событий.
const maxPublishInFlight = 32

// AvatarService - сценарии работы с аватарками: загрузка, выдача, удаление.
type AvatarService struct {
	repo      domain.AvatarRepository
	storage   domain.FileStorage
	publisher domain.EventPublisher
	cfg       config.Image
	log       *slog.Logger
	sem       chan struct{}
	wg        sync.WaitGroup
}

// NewAvatarService собирает сервис из зависимостей-портов.
func NewAvatarService(
	repo domain.AvatarRepository,
	storage domain.FileStorage,
	publisher domain.EventPublisher,
	cfg config.Image,
	log *slog.Logger,
) (*AvatarService, error) {
	if cfg.MaxFileSize <= 0 {
		return nil, errors.New("image: max_file_size must be positive")
	}

	if len(cfg.AllowedMimeTypes) == 0 {
		return nil, errors.New("image: allowed_mime_types must not be empty")
	}

	return &AvatarService{
		repo:      repo,
		storage:   storage,
		publisher: publisher,
		cfg:       cfg,
		log:       logger.Component(log, "avatar_service"),
		sem:       make(chan struct{}, maxPublishInFlight),
	}, nil
}

// Upload валидирует файл, кладёт оригинал в хранилище и ставит задачу на обработку.
func (s *AvatarService) Upload(ctx context.Context, in domain.UploadInput) (*domain.Avatar, error) {
	ctx, span := tracer.Start(ctx, "upload_avatar")
	defer span.End()

	span.SetAttributes(attribute.Int64("file.size", in.Size))

	started := time.Now()

	avatar, err := s.upload(ctx, in)

	metrics.ObserveUpload(ctx, started, uploadStatus(err))

	if err != nil {
		return nil, recordError(span, err)
	}

	span.SetAttributes(attribute.String("avatar.id", avatar.ID.String()))

	return avatar, nil
}

func uploadStatus(err error) string {
	switch {
	case err == nil:
		return metrics.StatusSuccess

	case domain.IsClientError(err):
		return metrics.StatusClientError

	default:
		return metrics.StatusError
	}
}

func (s *AvatarService) upload(ctx context.Context, in domain.UploadInput) (*domain.Avatar, error) {
	if in.UserID == "" {
		return nil, domain.ErrUserIDRequired
	}
	if in.Size > s.cfg.MaxFileSize {
		return nil, domain.ErrFileTooLarge
	}
	if in.Size == 0 {
		return nil, domain.ErrEmptyFile
	}

	head := make([]byte, sniffSize)
	n, err := io.ReadFull(in.Reader, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("read file head: %w", err)
	}
	head = head[:n]

	if len(head) == 0 {
		return nil, domain.ErrEmptyFile
	}

	mimeType := imageutil.DetectMIME(head)
	if !slices.Contains(s.cfg.AllowedMimeTypes, mimeType) {
		return nil, fmt.Errorf("%w: %s", domain.ErrUnsupportedFormat, mimeType)
	}

	avatar := &domain.Avatar{
		ID:               uuid.New(),
		UserID:           in.UserID,
		FileName:         imageutil.SanitizeFileName(in.FileName),
		MimeType:         mimeType,
		SizeBytes:        in.Size,
		ThumbnailS3Keys:  map[string]string{},
		UploadStatus:     domain.UploadStatusUploading,
		ProcessingStatus: domain.ProcessingStatusPending,
	}
	avatar.S3Key = originalKey(avatar.UserID, avatar.ID, mimeType)

	if err := s.repo.Create(ctx, avatar); err != nil {
		return nil, err
	}

	body := io.MultiReader(bytes.NewReader(head), in.Reader)

	if err := s.storage.Put(ctx, avatar.S3Key, body, in.Size, mimeType); err != nil {
		if statusErr := s.repo.SetUploadStatus(ctx, avatar.ID, domain.UploadStatusFailed); statusErr != nil {
			s.log.ErrorContext(ctx, "mark upload failed",
				slog.String("avatar_id", avatar.ID.String()), slog.Any("error", statusErr))
		}

		return nil, err
	}

	if err := s.repo.SetUploadStatus(ctx, avatar.ID, domain.UploadStatusUploaded); err != nil {
		return nil, err
	}
	avatar.UploadStatus = domain.UploadStatusUploaded

	s.publish(ctx, domain.Event{
		Type:       domain.EventAvatarUploaded,
		AvatarID:   avatar.ID,
		UserID:     avatar.UserID,
		S3Key:      avatar.S3Key,
		MimeType:   avatar.MimeType,
		OccurredAt: time.Now().UTC(),
	})

	return avatar, nil
}

// GetMetadata возвращает метаданные аватарки.
func (s *AvatarService) GetMetadata(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
	ctx, span := tracer.Start(ctx, "get_metadata")
	defer span.End()

	span.SetAttributes(attribute.String("avatar.id", id.String()))

	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, recordError(span, err)
	}

	span.SetAttributes(attribute.String("user_id", avatar.UserID))

	return avatar, nil
}

// ListByUser возвращает страницу аватарок пользователя.
func (s *AvatarService) ListByUser(ctx context.Context, userID string, limit, offset int) ([]*domain.Avatar, error) {
	ctx, span := tracer.Start(ctx, "list_by_user")
	defer span.End()

	span.SetAttributes(
		attribute.String("user_id", userID),
		attribute.Int("limit", limit),
		attribute.Int("offset", offset),
	)

	avatars, err := s.repo.ListByUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, recordError(span, err)
	}

	span.SetAttributes(attribute.Int("avatars.count", len(avatars)))

	return avatars, nil
}

// GetFile отдаёт файл аватарки в запрошенном размере и формате.
func (s *AvatarService) GetFile(ctx context.Context, id uuid.UUID, size, format string) (*domain.FileResult, error) {
	ctx, span := tracer.Start(ctx, "get_avatar_file")
	defer span.End()

	span.SetAttributes(
		attribute.String("avatar.id", id.String()),
		attribute.String("size", size),
		attribute.String("format", format),
	)

	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, recordError(span, err)
	}

	result, err := s.fileFor(ctx, avatar, size, format)

	return result, recordError(span, err)
}

// GetUserFile отдаёт файл последней аватарки пользователя.
func (s *AvatarService) GetUserFile(ctx context.Context, userID, size, format string) (*domain.FileResult, error) {
	ctx, span := tracer.Start(ctx, "get_user_avatar_file")
	defer span.End()

	span.SetAttributes(
		attribute.String("user_id", userID),
		attribute.String("size", size),
		attribute.String("format", format),
	)

	avatar, err := s.repo.GetLatestByUser(ctx, userID)
	if err != nil {
		return nil, recordError(span, err)
	}

	result, err := s.fileFor(ctx, avatar, size, format)

	return result, recordError(span, err)
}

// Delete мягко удаляет аватарку и ставит задачу на очистку хранилища.
func (s *AvatarService) Delete(ctx context.Context, id uuid.UUID, requesterID string) error {
	ctx, span := tracer.Start(ctx, "delete_avatar")
	defer span.End()

	span.SetAttributes(
		attribute.String("avatar.id", id.String()),
		attribute.String("requester_id", requesterID),
	)

	if requesterID == "" {
		return recordError(span, domain.ErrUserIDRequired)
	}

	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return recordError(span, err)
	}

	if avatar.UserID != requesterID {
		return recordError(span, domain.ErrForbidden)
	}

	if err := s.repo.SoftDelete(ctx, avatar.ID); err != nil {
		return recordError(span, err)
	}

	s.publish(ctx, domain.Event{
		Type:          domain.EventAvatarDeleted,
		AvatarID:      avatar.ID,
		UserID:        avatar.UserID,
		S3Key:         avatar.S3Key,
		ThumbnailKeys: avatar.ThumbnailS3Keys,
		OccurredAt:    time.Now().UTC(),
	})

	return nil
}

// DeleteUserAvatar мягко удаляет последнюю аватарку пользователя.
func (s *AvatarService) DeleteUserAvatar(ctx context.Context, userID, requesterID string) error {
	ctx, span := tracer.Start(ctx, "delete_user_avatar")
	defer span.End()

	span.SetAttributes(
		attribute.String("user_id", userID),
		attribute.String("requester_id", requesterID),
	)

	if requesterID == "" {
		return recordError(span, domain.ErrUserIDRequired)
	}

	if userID != requesterID {
		return recordError(span, domain.ErrForbidden)
	}

	avatar, err := s.repo.GetLatestByUser(ctx, userID)
	if err != nil {
		return recordError(span, err)
	}

	span.SetAttributes(attribute.String("avatar.id", avatar.ID.String()))

	return recordError(span, s.Delete(ctx, avatar.ID, requesterID))
}

// URL возвращает публичную ссылку на объект хранилища.
func (s *AvatarService) URL(key string) string {
	return s.storage.URL(key)
}

// MaxFileSize — лимит размера загружаемого файла.
func (s *AvatarService) MaxFileSize() int64 {
	return s.cfg.MaxFileSize
}

// AllowedMimeTypes — разрешённые MIME-типы загрузки.
func (s *AvatarService) AllowedMimeTypes() []string {
	return s.cfg.AllowedMimeTypes
}

func (s *AvatarService) fileFor(
	ctx context.Context,
	avatar *domain.Avatar,
	size, format string,
) (*domain.FileResult, error) {
	key, err := avatar.KeyForSize(size)
	if err != nil {
		return nil, err
	}

	object, err := s.storage.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	contentType := object.ContentType
	if contentType == "" {
		contentType = avatar.MimeType
	}

	result := &domain.FileResult{
		Body:         object.Body,
		ContentType:  contentType,
		Size:         object.Size,
		ETag:         object.ETag,
		LastModified: object.LastModified,
		Avatar:       avatar,
	}

	targetMime, ok := imageutil.NormalizeFormat(format)
	if !ok {
		_ = object.Body.Close()

		return nil, fmt.Errorf("%w: %s", domain.ErrUnsupportedTargetFormat, format)
	}
	if targetMime == "" || targetMime == contentType {
		return result, nil
	}

	return convert(result, targetMime)
}

// convert перекодирует уже прочитанный объект в запрошенный формат.
func convert(src *domain.FileResult, targetMime string) (*domain.FileResult, error) {
	defer func() { _ = src.Body.Close() }()

	if !imageutil.CanEncode(targetMime) {
		return nil, fmt.Errorf("%w: %s", domain.ErrUnsupportedTargetFormat, targetMime)
	}

	img, _, err := imageutil.Decode(src.Body)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := imageutil.Encode(&buf, img, targetMime); err != nil {
		return nil, err
	}

	etag := src.ETag
	if etag != "" {
		etag = strings.Trim(etag, `"`) + "-" + strings.TrimPrefix(targetMime, "image/")
	}

	return &domain.FileResult{
		Body:         io.NopCloser(&buf),
		ContentType:  targetMime,
		Size:         int64(buf.Len()),
		ETag:         etag,
		LastModified: src.LastModified,
		Avatar:       src.Avatar,
	}, nil
}

// publish отправляет событие в фоновом режиме
func (s *AvatarService) publish(ctx context.Context, event domain.Event) {
	ctx = context.WithoutCancel(ctx)

	select {
	case s.sem <- struct{}{}:
	default:
		s.log.WarnContext(ctx, "publish queue is full, sending inline",
			slog.String("type", string(event.Type)),
			slog.String("avatar_id", event.AvatarID.String()))
		s.send(ctx, event)

		return
	}

	s.wg.Add(1)

	go func() {
		defer s.wg.Done()
		defer func() { <-s.sem }()

		s.send(ctx, event)
	}()
}

func (s *AvatarService) send(ctx context.Context, event domain.Event) {
	if err := s.publisher.Publish(ctx, event); err != nil {
		s.log.ErrorContext(ctx, "publish event",
			slog.String("type", string(event.Type)),
			slog.String("avatar_id", event.AvatarID.String()),
			slog.Any("error", err))
	}
}

// Wait дожидается фоновых публикаций
func (s *AvatarService) Wait() {
	s.wg.Wait()
}

func originalKey(userID string, id uuid.UUID, mimeType string) string {
	return path.Join("avatars", userID, id.String(), "original"+imageutil.ExtByMIME(mimeType))
}

// ThumbnailKey — ключ миниатюры указанного размера; используется воркером.
func ThumbnailKey(userID string, id uuid.UUID, size, mimeType string) string {
	return path.Join("avatars", userID, id.String(), size+imageutil.ExtByMIME(mimeType))
}
