package domain

import (
	"time"

	"github.com/google/uuid"
)

// UploadStatus - статус загрузки файла в S3.
type UploadStatus string

const (
	UploadStatusUploading UploadStatus = "uploading"
	UploadStatusUploaded  UploadStatus = "uploaded"
	UploadStatusFailed    UploadStatus = "failed"
)

// ProcessingStatus - статус асинхронной обработки (миниатюры, метаданные).
type ProcessingStatus string

const (
	ProcessingStatusPending    ProcessingStatus = "pending"
	ProcessingStatusProcessing ProcessingStatus = "processing"
	ProcessingStatusReady      ProcessingStatus = "ready"
	ProcessingStatusFailed     ProcessingStatus = "failed"
)

// Публичные статусы аватарки, отдаваемые в API.
const (
	PublicStatusProcessing = "processing"
	PublicStatusReady      = "ready"
	PublicStatusFailed     = "failed"
)

// SizeOriginal - псевдоразмер, указывающий на исходный файл.
const SizeOriginal = "original"

// Avatar - метаданные аватарки (таблица avatars).
type Avatar struct {
	ID               uuid.UUID
	UserID           string
	FileName         string
	MimeType         string
	SizeBytes        int64
	S3Key            string
	ThumbnailS3Keys  map[string]string
	UploadStatus     UploadStatus
	ProcessingStatus ProcessingStatus
	Width            int
	Height           int
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

// PublicStatus сворачивает два внутренних статуса в один статус для клиента.
func (a *Avatar) PublicStatus() string {
	switch {
	case a.UploadStatus == UploadStatusFailed, a.ProcessingStatus == ProcessingStatusFailed:
		return PublicStatusFailed
	case a.ProcessingStatus == ProcessingStatusReady:
		return PublicStatusReady
	default:
		return PublicStatusProcessing
	}
}

// KeyForSize возвращает S3-ключ для запрошенного размера.
// Для "original" (или пустого значения) - ключ исходного файла.
func (a *Avatar) KeyForSize(size string) (string, error) {
	if size == "" || size == SizeOriginal {
		return a.S3Key, nil
	}

	key, ok := a.ThumbnailS3Keys[size]
	if !ok || key == "" {
		if a.ProcessingStatus != ProcessingStatusReady {
			return "", ErrThumbnailNotReady
		}

		return "", ErrSizeNotFound
	}

	return key, nil
}

// AllS3Keys - все объекты аватарки в хранилище (оригинал + миниатюры).
func (a *Avatar) AllS3Keys() []string {
	keys := make([]string, 0, len(a.ThumbnailS3Keys)+1)
	if a.S3Key != "" {
		keys = append(keys, a.S3Key)
	}

	for _, key := range a.ThumbnailS3Keys {
		if key != "" {
			keys = append(keys, key)
		}
	}

	return keys
}
