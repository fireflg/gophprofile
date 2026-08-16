package domain

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"
)

// AvatarRepository - хранилище метаданных аватарок.
type AvatarRepository interface {
	Create(ctx context.Context, avatar *Avatar) error
	GetByID(ctx context.Context, id uuid.UUID) (*Avatar, error)
	GetLatestByUser(ctx context.Context, userID string) (*Avatar, error)
	ListByUser(ctx context.Context, userID string, limit, offset int) ([]*Avatar, error)
	SetUploadStatus(ctx context.Context, id uuid.UUID, status UploadStatus) error
	SetProcessingStatus(ctx context.Context, id uuid.UUID, status ProcessingStatus) error
	SaveProcessingResult(ctx context.Context, id uuid.UUID, thumbnails map[string]string, width, height int) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	Ping(ctx context.Context) error
}

// Object - объект, прочитанный из файлового хранилища.
type Object struct {
	Body         io.ReadCloser
	ContentType  string
	Size         int64
	ETag         string
	LastModified time.Time
}

// FileStorage - файловое хранилище.
type FileStorage interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (*Object, error)
	Delete(ctx context.Context, keys ...string) error
	URL(key string) string
	Ping(ctx context.Context) error
}

// EventPublisher - публикация событий в брокере.
type EventPublisher interface {
	Publish(ctx context.Context, event Event) error
	Ping(ctx context.Context) error
	Close() error
}

// UploadInput - входные данные загрузки аватарки.
type UploadInput struct {
	UserID   string
	FileName string
	Size     int64
	Reader   io.Reader
}

// FileResult - файл аватарки, готовый к отдаче в HTTP-ответ.
type FileResult struct {
	Body         io.ReadCloser
	ContentType  string
	Size         int64
	ETag         string
	LastModified time.Time
	Avatar       *Avatar
}

// AvatarUseCase - сценарии работы с аватарками, которые использует HTTP-слой.
type AvatarUseCase interface {
	Upload(ctx context.Context, in UploadInput) (*Avatar, error)
	GetFile(ctx context.Context, id uuid.UUID, size, format string) (*FileResult, error)
	GetUserFile(ctx context.Context, userID, size, format string) (*FileResult, error)
	GetMetadata(ctx context.Context, id uuid.UUID) (*Avatar, error)
	ListByUser(ctx context.Context, userID string, limit, offset int) ([]*Avatar, error)
	Delete(ctx context.Context, id uuid.UUID, requesterID string) error
	DeleteUserAvatar(ctx context.Context, userID, requesterID string) error
	URL(key string) string
	MaxFileSize() int64
	AllowedMimeTypes() []string
}
