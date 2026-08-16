package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/fireflg/gophprofile/internal/domain"
)

// ErrorResponse — единый формат ошибки API.
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
	MaxSize int64  `json:"max_size,omitempty"`
}

// WriteJSON сериализует ответ с указанным HTTP-кодом.
func WriteJSON(w http.ResponseWriter, code int, payload any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)

	if payload == nil {
		return nil
	}

	return json.NewEncoder(w).Encode(payload)
}

// WriteError переводит доменную ошибку в HTTP-ответ.
func WriteError(w http.ResponseWriter, log *zap.Logger, err error, maxFileSize int64, allowed []string) {
	code, payload := MapError(err, maxFileSize, allowed)

	if code >= http.StatusInternalServerError && log != nil {
		log.Error("request failed", zap.Error(err))
	}

	if writeErr := WriteJSON(w, code, payload); writeErr != nil && log != nil {
		log.Error("write error response", zap.Error(writeErr))
	}
}

// MapError сопоставляет доменную ошибку с HTTP-кодом и телом ответа.
func MapError(err error, maxFileSize int64, allowed []string) (int, ErrorResponse) {
	switch {
	case errors.Is(err, domain.ErrAvatarNotFound), errors.Is(err, domain.ErrSizeNotFound):
		return http.StatusNotFound, ErrorResponse{Error: "Avatar not found"}

	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, ErrorResponse{
			Error:   "Forbidden",
			Details: "You can only delete your own avatars",
		}

	case errors.Is(err, domain.ErrUserIDRequired):
		return http.StatusUnauthorized, ErrorResponse{
			Error:   "Unauthorized",
			Details: "X-User-ID header is required",
		}

	case errors.Is(err, domain.ErrInvalidAvatarID):
		return http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid avatar id",
			Details: "Avatar id must be a valid UUID",
		}

	case errors.Is(err, domain.ErrUnsupportedFormat):
		return http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid file format",
			Details: "Supported formats: " + formatList(allowed),
		}

	case errors.Is(err, domain.ErrUnsupportedTargetFormat):
		return http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid target format",
			Details: "Supported values: jpeg, png",
		}

	case errors.Is(err, domain.ErrFileTooLarge):
		return http.StatusRequestEntityTooLarge, ErrorResponse{
			Error:   "File too large",
			MaxSize: maxFileSize,
		}

	case errors.Is(err, domain.ErrFileRequired), errors.Is(err, domain.ErrEmptyFile):
		return http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Details: "multipart form must contain a non-empty file field",
		}

	case errors.Is(err, domain.ErrThumbnailNotReady):
		return http.StatusConflict, ErrorResponse{
			Error:   "Avatar is still processing",
			Details: "Requested size is not available yet",
		}

	default:
		return http.StatusInternalServerError, ErrorResponse{Error: "Internal server error"}
	}
}

// formatList превращает MIME-типы в человекочитаемый перечень: "jpeg, png, webp".
func formatList(mimeTypes []string) string {
	names := make([]string, 0, len(mimeTypes))
	for _, mime := range mimeTypes {
		names = append(names, strings.TrimPrefix(mime, "image/"))
	}

	return strings.Join(names, ", ")
}

// NotFound - обработчик неизвестных маршрутов.
func NotFound(w http.ResponseWriter, _ *http.Request) {
	_ = WriteJSON(w, http.StatusNotFound, ErrorResponse{Error: "Not Found"})
}

// MethodNotAllowed - обработчик неподдерживаемых методов.
func MethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	_ = WriteJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Method Not Allowed"})
}
