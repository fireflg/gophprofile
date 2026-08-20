// Package handlers содержит HTTP-обработчики API и веб-интерфейса.
package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/pkg/logger"
)

const (
	// HeaderUserID - заголовок с идентификатором пользователя.
	HeaderUserID = "X-User-ID"

	defaultLimit = 50
	maxLimit     = 200

	// cacheMaxAge - время жизни файла аватарки в кеше клиента, сутки.
	cacheMaxAge = 86400
)

//go:generate mockgen -source=avatar.go -destination=mocks/usecase_mock.gen.go -package=mocks

// AvatarUseCase - то, что HTTP-слой ожидает от сценариев работы с аватарками.
type AvatarUseCase interface {
	Upload(ctx context.Context, in domain.UploadInput) (*domain.Avatar, error)
	GetFile(ctx context.Context, id uuid.UUID, size, format string) (*domain.FileResult, error)
	GetUserFile(ctx context.Context, userID, size, format string) (*domain.FileResult, error)
	GetMetadata(ctx context.Context, id uuid.UUID) (*domain.Avatar, error)
	ListByUser(ctx context.Context, userID string, limit, offset int) ([]*domain.Avatar, error)
	Delete(ctx context.Context, id uuid.UUID, requesterID string) error
	DeleteUserAvatar(ctx context.Context, userID, requesterID string) error
	URL(key string) string
	MaxFileSize() int64
	AllowedMimeTypes() []string
}

// AvatarHandler - HTTP-обработчики /api/v1.
type AvatarHandler struct {
	service AvatarUseCase
	log     *slog.Logger
}

// NewAvatarHandler создаёт обработчики аватарок.
func NewAvatarHandler(service AvatarUseCase, log *slog.Logger) *AvatarHandler {
	return &AvatarHandler{service: service, log: logger.Component(log, "avatar_handler")}
}

// Upload обрабатывает POST /api/v1/avatars.
func (h *AvatarHandler) Upload(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromRequest(r)
	if userID == "" {
		h.writeError(w, r, domain.ErrUserIDRequired)

		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		h.writeError(w, r, formFileError(err))

		return
	}
	defer func() { _ = file.Close() }()

	if fileHeader.Size > h.service.MaxFileSize() {
		h.writeError(w, r, domain.ErrFileTooLarge)

		return
	}

	avatar, err := h.service.Upload(r.Context(), domain.UploadInput{
		UserID:   userID,
		FileName: fileHeader.Filename,
		Size:     fileHeader.Size,
		Reader:   file,
	})
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	h.writeJSON(w, r, http.StatusCreated, newAvatarResponse(avatar, h.service.URL))
}

// Get обрабатывает GET /api/v1/avatars/{avatar_id}.
func (h *AvatarHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseAvatarID(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	query := r.URL.Query()

	result, err := h.service.GetFile(r.Context(), id, query.Get("size"), query.Get("format"))
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	h.writeFile(w, r, result)
}

// GetUserAvatar обрабатывает GET /api/v1/users/{user_id}/avatar.
func (h *AvatarHandler) GetUserAvatar(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		h.writeError(w, r, domain.ErrAvatarNotFound)

		return
	}

	query := r.URL.Query()

	result, err := h.service.GetUserFile(r.Context(), userID, query.Get("size"), query.Get("format"))
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	h.writeFile(w, r, result)
}

// Metadata обрабатывает GET /api/v1/avatars/{avatar_id}/metadata.
func (h *AvatarHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	id, err := parseAvatarID(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	avatar, err := h.service.GetMetadata(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	h.writeJSON(w, r, http.StatusOK, newMetadataResponse(avatar, h.service.URL))
}

// ListUserAvatars обрабатывает GET /api/v1/users/{user_id}/avatars.
func (h *AvatarHandler) ListUserAvatars(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	limit, offset := pagination(r)

	avatars, err := h.service.ListByUser(r.Context(), userID, limit, offset)
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	items := make([]AvatarResponse, 0, len(avatars))
	for _, avatar := range avatars {
		items = append(items, newAvatarResponse(avatar, h.service.URL))
	}

	h.writeJSON(w, r, http.StatusOK, ListResponse{Items: items, Limit: limit, Offset: offset})
}

// Delete обрабатывает DELETE /api/v1/avatars/{avatar_id}.
func (h *AvatarHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseAvatarID(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	if err := h.service.Delete(r.Context(), id, userIDFromRequest(r)); err != nil {
		h.writeError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteUserAvatar обрабатывает DELETE /api/v1/users/{user_id}/avatar.
func (h *AvatarHandler) DeleteUserAvatar(w http.ResponseWriter, r *http.Request) {
	err := h.service.DeleteUserAvatar(r.Context(), chi.URLParam(r, "user_id"), userIDFromRequest(r))
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// writeFile отдаёт бинарное содержимое с кеширующими заголовками.
func (h *AvatarHandler) writeFile(w http.ResponseWriter, r *http.Request, result *domain.FileResult) {
	defer func() { _ = result.Body.Close() }()

	header := w.Header()
	header.Set("Cache-Control", "max-age="+strconv.Itoa(cacheMaxAge))

	if etag := quoteETag(result.ETag); etag != "" {
		header.Set("ETag", etag)

		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)

			return
		}
	}

	if !result.LastModified.IsZero() {
		header.Set("Last-Modified", result.LastModified.UTC().Format(http.TimeFormat))
	}
	if result.Size > 0 {
		header.Set("Content-Length", strconv.FormatInt(result.Size, 10))
	}

	contentType := result.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType)

	w.WriteHeader(http.StatusOK)

	if r.Method == http.MethodHead {
		return
	}

	if _, err := io.Copy(w, result.Body); err != nil {
		h.log.ErrorContext(r.Context(), "stream avatar file", slog.Any("error", err))
	}
}

func (h *AvatarHandler) writeJSON(w http.ResponseWriter, r *http.Request, code int, payload any) {
	if err := WriteJSON(w, code, payload); err != nil {
		h.log.ErrorContext(r.Context(), "write json response", slog.Any("error", err))
	}
}

func (h *AvatarHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	WriteError(r.Context(), w, h.log, err, Limits{
		MaxFileSize:      h.service.MaxFileSize(),
		AllowedMimeTypes: h.service.AllowedMimeTypes(),
	})
}

// userIDFromRequest достаёт идентификатор пользователя из заголовка X-User-ID.
func userIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(HeaderUserID))
}

func formFileError(err error) error {
	if errors.As(err, new(*http.MaxBytesError)) {
		return domain.ErrFileTooLarge
	}

	return domain.ErrFileRequired
}

func parseAvatarID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, domain.ErrInvalidAvatarID
	}

	return id, nil
}

func pagination(r *http.Request) (limit, offset int) {
	query := r.URL.Query()
	limit = defaultLimit

	if parsed, err := strconv.Atoi(query.Get("limit")); err == nil && parsed > 0 {
		limit = min(parsed, maxLimit)
	}

	if parsed, err := strconv.Atoi(query.Get("offset")); err == nil && parsed > 0 {
		offset = parsed
	}

	return limit, offset
}

func quoteETag(etag string) string {
	etag = strings.Trim(etag, `"`)
	if etag == "" {
		return ""
	}

	return `"` + etag + `"`
}

// etagMatches проверяет заголовок If-None-Match, включая список значений и "*".
func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	if strings.TrimSpace(ifNoneMatch) == "*" {
		return true
	}

	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")

		if candidate == etag {
			return true
		}
	}

	return false
}
