package handlers_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/internal/domain/mocks"
	"github.com/fireflg/gophprofile/internal/handlers"
)

const maxFileSize = 10 << 20

// newRouter поднимает те же маршруты, что и боевой роутер, но без прослоек:
// так тест проверяет ровно поведение обработчиков.
func newRouter(t *testing.T) (http.Handler, *mocks.MockAvatarUseCase) {
	t.Helper()

	service := mocks.NewMockAvatarUseCase(gomock.NewController(t))
	service.EXPECT().MaxFileSize().Return(int64(maxFileSize)).AnyTimes()
	service.EXPECT().AllowedMimeTypes().Return([]string{"image/jpeg", "image/png", "image/webp"}).AnyTimes()
	service.EXPECT().URL(gomock.Any()).DoAndReturn(func(key string) string {
		return "http://storage.test/" + key
	}).AnyTimes()

	handler := handlers.NewAvatarHandler(service, zap.NewNop())

	router := chi.NewRouter()
	router.Post("/api/v1/avatars", handler.Upload)
	router.Get("/api/v1/avatars/{id}", handler.Get)
	router.Get("/api/v1/avatars/{id}/metadata", handler.Metadata)
	router.Delete("/api/v1/avatars/{id}", handler.Delete)
	router.Get("/api/v1/users/{user_id}/avatar", handler.GetUserAvatar)
	router.Delete("/api/v1/users/{user_id}/avatar", handler.DeleteUserAvatar)
	router.Get("/api/v1/users/{user_id}/avatars", handler.ListUserAvatars)

	return router, service
}

func do(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec
}

func multipartRequest(t *testing.T, userID string, content []byte) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", "avatar.png")
	require.NoError(t, err)

	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	if userID != "" {
		req.Header.Set(handlers.HeaderUserID, userID)
	}

	return req
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var payload T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload), "body: %s", rec.Body.String())

	return payload
}

func sampleAvatar() *domain.Avatar {
	id := uuid.New()

	return &domain.Avatar{
		ID:               id,
		UserID:           "user-1",
		FileName:         "avatar.png",
		MimeType:         "image/png",
		SizeBytes:        2048,
		S3Key:            "avatars/user-1/" + id.String() + "/original.png",
		ThumbnailS3Keys:  map[string]string{"300x300": "b.jpg", "100x100": "a.jpg"},
		UploadStatus:     domain.UploadStatusUploaded,
		ProcessingStatus: domain.ProcessingStatusReady,
		Width:            640,
		Height:           480,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
}

func TestUploadReturns201(t *testing.T) {
	router, service := newRouter(t)

	avatar := sampleAvatar()
	avatar.ProcessingStatus = domain.ProcessingStatusPending

	service.EXPECT().
		Upload(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, in domain.UploadInput) (*domain.Avatar, error) {
			require.Equal(t, "user-1", in.UserID)
			require.Equal(t, "avatar.png", in.FileName)

			return avatar, nil
		})

	rec := do(router, multipartRequest(t, "user-1", []byte("содержимое файла")))

	require.Equal(t, http.StatusCreated, rec.Code)

	body := decode[handlers.AvatarResponse](t, rec)
	require.Equal(t, avatar.ID.String(), body.ID)
	require.Equal(t, domain.PublicStatusProcessing, body.Status)
	require.Contains(t, body.URL, avatar.S3Key)
}

func TestUploadWithoutUserIDReturns401(t *testing.T) {
	router, _ := newRouter(t)

	rec := do(router, multipartRequest(t, "", []byte("данные")))

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "Unauthorized", decode[handlers.ErrorResponse](t, rec).Error)
}

func TestUploadWithoutFileReturns400(t *testing.T) {
	router, _ := newRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=zzz")
	req.Header.Set(handlers.HeaderUserID, "user-1")

	rec := do(router, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadTooLargeReturns413(t *testing.T) {
	router, service := newRouter(t)

	service.EXPECT().Upload(gomock.Any(), gomock.Any()).Return(nil, domain.ErrFileTooLarge)

	rec := do(router, multipartRequest(t, "user-1", []byte("данные")))

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)

	body := decode[handlers.ErrorResponse](t, rec)
	require.Equal(t, "File too large", body.Error)
	require.Equal(t, int64(maxFileSize), body.MaxSize)
}

func TestUploadUnsupportedFormatReturns400(t *testing.T) {
	router, service := newRouter(t)

	service.EXPECT().Upload(gomock.Any(), gomock.Any()).Return(nil, domain.ErrUnsupportedFormat)

	rec := do(router, multipartRequest(t, "user-1", []byte("текст")))

	require.Equal(t, http.StatusBadRequest, rec.Code)

	body := decode[handlers.ErrorResponse](t, rec)
	require.Equal(t, "Invalid file format", body.Error)
	require.Equal(t, "Supported formats: jpeg, png, webp", body.Details)
}

func TestGetFileSetsCachingHeaders(t *testing.T) {
	router, service := newRouter(t)

	avatar := sampleAvatar()
	modified := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	service.EXPECT().
		GetFile(gomock.Any(), avatar.ID, "100x100", "webp").
		Return(&domain.FileResult{
			Body:         io.NopCloser(bytes.NewReader([]byte("картинка"))),
			ContentType:  "image/png",
			Size:         int64(len("картинка")),
			ETag:         "abc123",
			LastModified: modified,
			Avatar:       avatar,
		}, nil)

	url := "/api/v1/avatars/" + avatar.ID.String() + "?size=100x100&format=webp"
	rec := do(router, httptest.NewRequest(http.MethodGet, url, nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, "max-age=86400", rec.Header().Get("Cache-Control"))
	require.Equal(t, `"abc123"`, rec.Header().Get("ETag"))
	require.Equal(t, modified.Format(http.TimeFormat), rec.Header().Get("Last-Modified"))
	require.Equal(t, "картинка", rec.Body.String())
}

func TestGetFileReturns304OnMatchingETag(t *testing.T) {
	tests := map[string]string{
		"точное совпадение": `"abc123"`,
		"слабый валидатор":  `W/"abc123"`,
		"список значений":   `"other", "abc123"`,
		"звёздочка":         "*",
	}

	for name, ifNoneMatch := range tests {
		t.Run(name, func(t *testing.T) {
			router, service := newRouter(t)

			avatar := sampleAvatar()
			service.EXPECT().
				GetFile(gomock.Any(), avatar.ID, "", "").
				Return(&domain.FileResult{
					Body:        io.NopCloser(bytes.NewReader([]byte("картинка"))),
					ContentType: "image/png",
					ETag:        "abc123",
				}, nil)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+avatar.ID.String(), nil)
			req.Header.Set("If-None-Match", ifNoneMatch)

			rec := do(router, req)

			require.Equal(t, http.StatusNotModified, rec.Code)
			require.Empty(t, rec.Body.String())
		})
	}
}

func TestGetFileInvalidIDReturns400(t *testing.T) {
	router, _ := newRouter(t)

	rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/avatars/not-a-uuid", nil))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "Invalid avatar id", decode[handlers.ErrorResponse](t, rec).Error)
}

func TestGetFileNotFoundReturns404(t *testing.T) {
	router, service := newRouter(t)

	id := uuid.New()
	service.EXPECT().GetFile(gomock.Any(), id, "", "").Return(nil, domain.ErrAvatarNotFound)

	rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+id.String(), nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "Avatar not found", decode[handlers.ErrorResponse](t, rec).Error)
}

func TestGetFileNotReadyReturns409(t *testing.T) {
	router, service := newRouter(t)

	id := uuid.New()
	service.EXPECT().GetFile(gomock.Any(), id, "100x100", "").Return(nil, domain.ErrThumbnailNotReady)

	rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+id.String()+"?size=100x100", nil))

	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestGetFileUnknownErrorReturns500(t *testing.T) {
	router, service := newRouter(t)

	id := uuid.New()
	service.EXPECT().GetFile(gomock.Any(), id, "", "").Return(nil, errors.New("хранилище недоступно"))

	rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+id.String(), nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "Internal server error", decode[handlers.ErrorResponse](t, rec).Error)
}

func TestGetUserAvatar(t *testing.T) {
	router, service := newRouter(t)

	service.EXPECT().
		GetUserFile(gomock.Any(), "user-1", "", "").
		Return(&domain.FileResult{
			Body:        io.NopCloser(bytes.NewReader([]byte("картинка"))),
			ContentType: "image/jpeg",
		}, nil)

	rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/users/user-1/avatar", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/jpeg", rec.Header().Get("Content-Type"))
}

func TestMetadataSortsThumbnails(t *testing.T) {
	router, service := newRouter(t)

	avatar := sampleAvatar()
	service.EXPECT().GetMetadata(gomock.Any(), avatar.ID).Return(avatar, nil)

	rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+avatar.ID.String()+"/metadata", nil))

	require.Equal(t, http.StatusOK, rec.Code)

	body := decode[handlers.MetadataResponse](t, rec)
	require.Equal(t, avatar.ID.String(), body.ID)
	require.Equal(t, "image/png", body.MimeType)
	require.Equal(t, int64(2048), body.Size)
	require.Equal(t, 640, body.Dimensions.Width)
	require.Equal(t, 480, body.Dimensions.Height)
	require.Len(t, body.Thumbnails, 2)
	require.Equal(t, "100x100", body.Thumbnails[0].Size)
	require.Equal(t, "300x300", body.Thumbnails[1].Size)
}

func TestListUserAvatarsPagination(t *testing.T) {
	tests := map[string]struct {
		query      string
		wantLimit  int
		wantOffset int
	}{
		"по умолчанию":       {query: "", wantLimit: 50, wantOffset: 0},
		"явные значения":     {query: "?limit=10&offset=20", wantLimit: 10, wantOffset: 20},
		"limit выше предела": {query: "?limit=10000", wantLimit: 200, wantOffset: 0},
		"мусор в параметрах": {query: "?limit=abc&offset=-5", wantLimit: 50, wantOffset: 0},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			router, service := newRouter(t)

			service.EXPECT().
				ListByUser(gomock.Any(), "user-1", tc.wantLimit, tc.wantOffset).
				Return([]*domain.Avatar{sampleAvatar()}, nil)

			rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/users/user-1/avatars"+tc.query, nil))

			require.Equal(t, http.StatusOK, rec.Code)

			body := decode[handlers.ListResponse](t, rec)
			require.Equal(t, tc.wantLimit, body.Limit)
			require.Equal(t, tc.wantOffset, body.Offset)
			require.Len(t, body.Items, 1)
		})
	}
}

func TestListUserAvatarsEmptyIsArrayNotNull(t *testing.T) {
	router, service := newRouter(t)

	service.EXPECT().ListByUser(gomock.Any(), "user-1", 50, 0).Return(nil, nil)

	rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/users/user-1/avatars", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"items":[]`)
}

func TestDeleteReturns204(t *testing.T) {
	router, service := newRouter(t)

	id := uuid.New()
	service.EXPECT().Delete(gomock.Any(), id, "user-1").Return(nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/"+id.String(), nil)
	req.Header.Set(handlers.HeaderUserID, "user-1")

	rec := do(router, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, rec.Body.String())
}

func TestDeleteForeignAvatarReturns403(t *testing.T) {
	router, service := newRouter(t)

	id := uuid.New()
	service.EXPECT().Delete(gomock.Any(), id, "user-2").Return(domain.ErrForbidden)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/"+id.String(), nil)
	req.Header.Set(handlers.HeaderUserID, "user-2")

	rec := do(router, req)

	require.Equal(t, http.StatusForbidden, rec.Code)

	body := decode[handlers.ErrorResponse](t, rec)
	require.Equal(t, "Forbidden", body.Error)
	require.Equal(t, "You can only delete your own avatars", body.Details)
}

func TestDeleteUserAvatarReturns204(t *testing.T) {
	router, service := newRouter(t)

	service.EXPECT().DeleteUserAvatar(gomock.Any(), "user-1", "user-1").Return(nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/user-1/avatar", nil)
	req.Header.Set(handlers.HeaderUserID, "user-1")

	require.Equal(t, http.StatusNoContent, do(router, req).Code)
}

func TestUserIDHeaderIsTrimmed(t *testing.T) {
	router, service := newRouter(t)

	id := uuid.New()
	service.EXPECT().Delete(gomock.Any(), id, "user-1").Return(nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/"+id.String(), nil)
	req.Header.Set(handlers.HeaderUserID, "  user-1  ")

	require.Equal(t, http.StatusNoContent, do(router, req).Code)
}
