package domain

import (
	"time"

	"github.com/google/uuid"
)

// EventType - тип события, публикуемого в брокере.
type EventType string

const (
	// EventAvatarUploaded - оригинал загружен в S3, нужна нарезка миниатюр.
	EventAvatarUploaded EventType = "avatar.uploaded"
	// EventAvatarDeleted - запись помечена удалённой, нужно убрать объекты из S3.
	EventAvatarDeleted EventType = "avatar.deleted"
)

// Event - сообщение очереди обработки аватарок.
type Event struct {
	Type          EventType         `json:"type"`
	AvatarID      uuid.UUID         `json:"avatar_id"`
	UserID        string            `json:"user_id"`
	S3Key         string            `json:"s3_key"`
	MimeType      string            `json:"mime_type,omitempty"`
	ThumbnailKeys map[string]string `json:"thumbnail_keys,omitempty"`
	OccurredAt    time.Time         `json:"occurred_at"`
}

// RoutingKey - ключ маршрутизации события в брокере.
func (e Event) RoutingKey() string {
	return string(e.Type)
}
