// Package worker обрабатывает события аватарок: нарезку миниатюр и очистку хранилища.
package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/internal/metrics"
	"github.com/fireflg/gophprofile/internal/services"
	"github.com/fireflg/gophprofile/pkg/imageutil"
	"github.com/fireflg/gophprofile/pkg/logger"
)

var errSkipped = errors.New("event skipped")

// Processor выполняет бизнес-логику обработки события.
type Processor struct {
	repo    domain.AvatarRepository
	storage domain.FileStorage
	sizes   []config.Size
	log     *slog.Logger
}

// NewProcessor создаёт обработчик событий.
func NewProcessor(
	repo domain.AvatarRepository,
	storage domain.FileStorage,
	sizes []config.Size,
	log *slog.Logger,
) (*Processor, error) {
	if len(sizes) == 0 {
		return nil, errors.New("worker: thumbnail sizes must not be empty")
	}

	return &Processor{
		repo:    repo,
		storage: storage,
		sizes:   sizes,
		log:     logger.Component(log, "processor"),
	}, nil
}

// Handle разбирает событие и вызывает соответствующий сценарий.
func (p *Processor) Handle(ctx context.Context, event domain.Event) error {
	started := time.Now()

	var err error

	switch event.Type {
	case domain.EventAvatarUploaded:
		err = p.handleUploaded(ctx, event)
	case domain.EventAvatarDeleted:
		err = p.handleDeleted(ctx, event)
	default:
		p.log.WarnContext(ctx, "unknown event type",
			slog.String("type", string(event.Type)), slog.String("avatar_id", event.AvatarID.String()))

		err = errSkipped
	}

	metrics.ObserveProcessing(ctx, started, eventTypeLabel(event.Type), processingStatus(err))

	if errors.Is(err, errSkipped) {
		return nil
	}

	return err
}

// eventTypeLabel схлопывает чужие типы событий в unknown, чтобы не растить кардинальность.
func eventTypeLabel(eventType domain.EventType) string {
	switch eventType {
	case domain.EventAvatarUploaded, domain.EventAvatarDeleted:
		return string(eventType)
	default:
		return "unknown"
	}
}

// processingStatus - исход обработки события для лейбла метрики.
func processingStatus(err error) string {
	switch {
	case errors.Is(err, errSkipped):
		return metrics.StatusSkipped
	default:
		return metrics.Status(err)
	}
}

// handleUploaded нарезает миниатюры оригинала и сохраняет их ключи в БД.
func (p *Processor) handleUploaded(ctx context.Context, event domain.Event) error {
	ctx, span := tracer.Start(ctx, "process_uploaded")
	defer span.End()

	span.SetAttributes(
		attribute.String("avatar.id", event.AvatarID.String()),
		attribute.Int("thumbnail.count", len(p.sizes)),
	)

	avatar, err := p.repo.GetByID(ctx, event.AvatarID)
	if err != nil {
		if errors.Is(err, domain.ErrAvatarNotFound) {
			p.log.InfoContext(ctx, "avatar is gone, event skipped",
				slog.String("avatar_id", event.AvatarID.String()))

			return errSkipped
		}

		return recordError(span, err)
	}

	if avatar.ProcessingStatus == domain.ProcessingStatusReady {
		p.log.InfoContext(ctx, "avatar already processed, duplicate skipped",
			slog.String("avatar_id", event.AvatarID.String()))

		return errSkipped
	}

	if err = p.repo.SetProcessingStatus(ctx, event.AvatarID, domain.ProcessingStatusProcessing); err != nil {
		return recordError(span, err)
	}

	thumbnails, width, height, err := p.makeThumbnails(ctx, event)
	if err != nil {
		if statusErr := p.repo.SetProcessingStatus(ctx, event.AvatarID, domain.ProcessingStatusFailed); statusErr != nil {
			p.log.ErrorContext(ctx, "mark processing failed",
				slog.String("avatar_id", event.AvatarID.String()), slog.Any("error", statusErr))
		}

		return recordError(span, err)
	}

	if err = p.repo.SaveProcessingResult(ctx, event.AvatarID, thumbnails, width, height); err != nil {
		return recordError(span, err)
	}

	span.SetAttributes(
		attribute.Int("image.width", width),
		attribute.Int("image.height", height),
	)

	p.log.InfoContext(ctx, "avatar processed",
		slog.String("avatar_id", event.AvatarID.String()),
		slog.Int("thumbnails", len(thumbnails)),
		slog.Int("width", width),
		slog.Int("height", height))

	return nil
}

func (p *Processor) makeThumbnails(ctx context.Context, event domain.Event) (map[string]string, int, int, error) {
	object, err := p.storage.Get(ctx, event.S3Key)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("get original: %w", err)
	}

	original, err := io.ReadAll(object.Body)
	_ = object.Body.Close()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read original: %w", err)
	}

	source, _, err := imageutil.Decode(bytes.NewReader(original))
	if err != nil {
		return nil, 0, 0, err
	}

	bounds := source.Bounds()
	thumbnails := make(map[string]string, len(p.sizes))

	thumbMime := imageutil.ThumbnailMIME(event.MimeType)

	for _, size := range p.sizes {
		if err := p.putThumbnail(ctx, event, source, size, thumbMime); err != nil {
			return nil, 0, 0, err
		}

		thumbnails[size.String()] = services.ThumbnailKey(event.UserID, event.AvatarID, size.String(), thumbMime)
	}

	return thumbnails, bounds.Dx(), bounds.Dy(), nil
}

// putThumbnail нарезает и кладёт в хранилище одну миниатюру.
func (p *Processor) putThumbnail(
	ctx context.Context,
	event domain.Event,
	source image.Image,
	size config.Size,
	thumbMime string,
) error {
	ctx, span := tracer.Start(ctx, "make_thumbnail")
	defer span.End()

	span.SetAttributes(
		attribute.String("thumbnail.size", size.String()),
		attribute.String("thumbnail.mime_type", thumbMime),
	)

	var buf bytes.Buffer
	if err := imageutil.Encode(&buf, imageutil.Thumbnail(source, size.Width, size.Height), thumbMime); err != nil {
		return recordError(span, fmt.Errorf("encode thumbnail %s: %w", size, err))
	}

	key := services.ThumbnailKey(event.UserID, event.AvatarID, size.String(), thumbMime)
	if err := p.storage.Put(ctx, key, bytes.NewReader(buf.Bytes()), int64(buf.Len()), thumbMime); err != nil {
		return recordError(span, fmt.Errorf("put thumbnail %s: %w", size, err))
	}

	return nil
}

// handleDeleted удаляет объекты аватарки из хранилища после мягкого удаления в БД.
func (p *Processor) handleDeleted(ctx context.Context, event domain.Event) error {
	ctx, span := tracer.Start(ctx, "process_deleted")
	defer span.End()

	span.SetAttributes(attribute.String("avatar.id", event.AvatarID.String()))

	keys := make([]string, 0, len(event.ThumbnailKeys)+1)
	if event.S3Key != "" {
		keys = append(keys, event.S3Key)
	}

	for _, key := range event.ThumbnailKeys {
		keys = append(keys, key)
	}

	if len(keys) == 0 {
		return nil
	}

	span.SetAttributes(attribute.Int("storage.objects", len(keys)))

	if err := p.storage.Delete(ctx, keys...); err != nil {
		return recordError(span, fmt.Errorf("delete objects: %w", err))
	}

	p.log.InfoContext(ctx, "avatar objects removed",
		slog.String("avatar_id", event.AvatarID.String()), slog.Int("objects", len(keys)))

	return nil
}
