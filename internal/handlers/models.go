// Package handlers содержит HTTP-обработчики API и веб-интерфейса.
package handlers

import (
	"sort"
	"time"

	"github.com/fireflg/gophprofile/internal/domain"
)

// AvatarResponse - краткое представление аватарки (ответ на загрузку и в списках).
type AvatarResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	URL       string    `json:"url"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// DimensionsResponse - размеры оригинала в пикселях.
type DimensionsResponse struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ThumbnailResponse - ссылка на миниатюру.
type ThumbnailResponse struct {
	Size string `json:"size"`
	URL  string `json:"url"`
}

// MetadataResponse - полные метаданные аватарки.
type MetadataResponse struct {
	ID         string              `json:"id"`
	UserID     string              `json:"user_id"`
	FileName   string              `json:"file_name"`
	MimeType   string              `json:"mime_type"`
	Size       int64               `json:"size"`
	Status     string              `json:"status"`
	Dimensions DimensionsResponse  `json:"dimensions"`
	Thumbnails []ThumbnailResponse `json:"thumbnails"`
	CreatedAt  time.Time           `json:"created_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
}

// ListResponse - страница списка аватарок пользователя.
type ListResponse struct {
	Items  []AvatarResponse `json:"items"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

// urlBuilder строит публичную ссылку по ключу объекта в хранилище.
type urlBuilder func(key string) string

func newAvatarResponse(avatar *domain.Avatar, url urlBuilder) AvatarResponse {
	return AvatarResponse{
		ID:        avatar.ID.String(),
		UserID:    avatar.UserID,
		URL:       url(avatar.S3Key),
		Status:    avatar.PublicStatus(),
		CreatedAt: avatar.CreatedAt,
	}
}

func newMetadataResponse(avatar *domain.Avatar, url urlBuilder) MetadataResponse {
	thumbnails := make([]ThumbnailResponse, 0, len(avatar.ThumbnailS3Keys))
	for size, key := range avatar.ThumbnailS3Keys {
		thumbnails = append(thumbnails, ThumbnailResponse{Size: size, URL: url(key)})
	}

	sort.Slice(thumbnails, func(i, j int) bool { return thumbnails[i].Size < thumbnails[j].Size })

	return MetadataResponse{
		ID:         avatar.ID.String(),
		UserID:     avatar.UserID,
		FileName:   avatar.FileName,
		MimeType:   avatar.MimeType,
		Size:       avatar.SizeBytes,
		Status:     avatar.PublicStatus(),
		Dimensions: DimensionsResponse{Width: avatar.Width, Height: avatar.Height},
		Thumbnails: thumbnails,
		CreatedAt:  avatar.CreatedAt,
		UpdatedAt:  avatar.UpdatedAt,
	}
}
