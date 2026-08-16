// Package worker обрабатывает события аватарок: нарезку миниатюр и очистку хранилища.
package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"go.uber.org/zap"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/internal/services"
	"github.com/fireflg/gophprofile/pkg/imageutil"
)

// Processor выполняет бизнес-логику обработки события.
type Processor struct {
	repo    domain.AvatarRepository
	storage domain.FileStorage
	sizes   []config.Size
	log     *zap.Logger
}

// NewProcessor создаёт обработчик событий.
func NewProcessor(
	repo domain.AvatarRepository,
	storage domain.FileStorage,
	sizes []config.Size,
	log *zap.Logger,
) *Processor {
	return &Processor{repo: repo, storage: storage, sizes: sizes, log: log}
}

// Handle разбирает событие и вызывает соответствующий сценарий.
func (p *Processor) Handle(ctx context.Context, event domain.Event) error {
	switch event.Type {
	case domain.EventAvatarUploaded:
		return p.handleUploaded(ctx, event)
	case domain.EventAvatarDeleted:
		return p.handleDeleted(ctx, event)
	default:
		p.log.Warn("unknown event type",
			zap.String("type", string(event.Type)), zap.String("avatar_id", event.AvatarID.String()))

		return nil
	}
}

// handleUploaded нарезает миниатюры оригинала и сохраняет их ключи в БД.
func (p *Processor) handleUploaded(ctx context.Context, event domain.Event) error {
	avatar, err := p.repo.GetByID(ctx, event.AvatarID)
	if err != nil {
		if errors.Is(err, domain.ErrAvatarNotFound) {
			p.log.Info("avatar is gone, event skipped",
				zap.String("avatar_id", event.AvatarID.String()))

			return nil
		}

		return err
	}

	if avatar.ProcessingStatus == domain.ProcessingStatusReady {
		p.log.Info("avatar already processed, duplicate skipped",
			zap.String("avatar_id", event.AvatarID.String()))

		return nil
	}

	if err = p.repo.SetProcessingStatus(ctx, event.AvatarID, domain.ProcessingStatusProcessing); err != nil {
		return err
	}

	thumbnails, width, height, err := p.makeThumbnails(ctx, event)
	if err != nil {
		if statusErr := p.repo.SetProcessingStatus(ctx, event.AvatarID, domain.ProcessingStatusFailed); statusErr != nil {
			p.log.Error("mark processing failed",
				zap.String("avatar_id", event.AvatarID.String()), zap.Error(statusErr))
		}

		return err
	}

	if err := p.repo.SaveProcessingResult(ctx, event.AvatarID, thumbnails, width, height); err != nil {
		return err
	}

	p.log.Info("avatar processed",
		zap.String("avatar_id", event.AvatarID.String()),
		zap.Int("thumbnails", len(thumbnails)),
		zap.Int("width", width),
		zap.Int("height", height))

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
		var buf bytes.Buffer
		if err := imageutil.Encode(&buf, imageutil.Thumbnail(source, size.Width, size.Height), thumbMime); err != nil {
			return nil, 0, 0, fmt.Errorf("encode thumbnail %s: %w", size, err)
		}

		key := services.ThumbnailKey(event.UserID, event.AvatarID, size.String(), thumbMime)
		if err := p.storage.Put(ctx, key, bytes.NewReader(buf.Bytes()), int64(buf.Len()), thumbMime); err != nil {
			return nil, 0, 0, fmt.Errorf("put thumbnail %s: %w", size, err)
		}

		thumbnails[size.String()] = key
	}

	return thumbnails, bounds.Dx(), bounds.Dy(), nil
}

// handleDeleted удаляет объекты аватарки из хранилища после мягкого удаления в БД.
func (p *Processor) handleDeleted(ctx context.Context, event domain.Event) error {
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

	if err := p.storage.Delete(ctx, keys...); err != nil {
		return fmt.Errorf("delete objects: %w", err)
	}

	p.log.Info("avatar objects removed",
		zap.String("avatar_id", event.AvatarID.String()), zap.Int("objects", len(keys)))

	return nil
}
