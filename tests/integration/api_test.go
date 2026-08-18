package integration

import (
	"bytes"
	"context"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/internal/handlers"
)

const defaultMaxFileSize = 10 << 20

type countingReader struct {
	reader io.Reader
	read   int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	c.read += int64(n)

	return n, err
}

func processAvatar(t *testing.T, env *testEnv, id string) {
	t.Helper()

	avatarID, err := uuid.Parse(id)
	require.NoError(t, err)

	avatar, err := env.repo.GetByID(context.Background(), avatarID)
	require.NoError(t, err)

	err = env.processor.Handle(context.Background(), domain.Event{
		Type:     domain.EventAvatarUploaded,
		AvatarID: avatar.ID,
		UserID:   avatar.UserID,
		S3Key:    avatar.S3Key,
		MimeType: avatar.MimeType,
	})
	require.NoError(t, err)
}

func TestUploadAvatar(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)

	response := uploadAvatar(t, env, "user-1")

	require.NotEmpty(t, response.ID)
	require.Equal(t, "user-1", response.UserID)
	require.Equal(t, domain.PublicStatusProcessing, response.Status)
	require.False(t, response.CreatedAt.IsZero())
	require.NotEmpty(t, response.URL)
}

func TestUploadAvatarWithoutUserID(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)

	rec := env.do(uploadRequest(t, "", "avatar.png", pngBytes(t, 64, 64)))

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "Unauthorized", decodeJSON[handlers.ErrorResponse](t, rec).Error)
}

func TestUploadAvatarUnsupportedFormat(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)

	rec := env.do(uploadRequest(t, "user-1", "notes.txt", []byte("это точно не картинка")))

	require.Equal(t, http.StatusBadRequest, rec.Code)

	body := decodeJSON[handlers.ErrorResponse](t, rec)
	require.Equal(t, "Invalid file format", body.Error)
	require.Contains(t, body.Details, "jpeg")
}

func TestUploadAvatarTooLarge(t *testing.T) {
	env := newTestEnv(t, 1024)

	rec := env.do(uploadRequest(t, "user-1", "avatar.png", pngBytes(t, 640, 480)))

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)

	body := decodeJSON[handlers.ErrorResponse](t, rec)
	require.Equal(t, "File too large", body.Error)
	require.Equal(t, int64(1024), body.MaxSize)
}

func TestUploadAvatarStopsReadingOversizedBody(t *testing.T) {
	env := newTestEnv(t, 1024)

	req := uploadRequest(t, "user-1", "avatar.png", bytes.Repeat([]byte("x"), 8<<20))
	counter := &countingReader{reader: req.Body}
	req.Body = io.NopCloser(counter)

	rec := env.do(req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Equal(t, "File too large", decodeJSON[handlers.ErrorResponse](t, rec).Error)
	require.Less(t, counter.read, int64(1<<20))
}

func TestGetAvatarOriginalWithCaching(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploaded := uploadAvatar(t, env, "user-1")

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+uploaded.ID, nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, "max-age=86400", rec.Header().Get("Cache-Control"))
	require.NotEmpty(t, rec.Body.Bytes())

	etag := rec.Header().Get("ETag")
	require.NotEmpty(t, etag)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+uploaded.ID, nil)
	req.Header.Set("If-None-Match", etag)

	cached := env.do(req)
	require.Equal(t, http.StatusNotModified, cached.Code)
	require.Empty(t, cached.Body.Bytes())
}

func TestGetAvatarNotFound(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+uuid.NewString(), nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "Avatar not found", decodeJSON[handlers.ErrorResponse](t, rec).Error)
}

func TestGetAvatarInvalidID(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/avatars/not-a-uuid", nil))

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetThumbnailBeforeProcessing(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploaded := uploadAvatar(t, env, "user-1")

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+uploaded.ID+"?size=100x100", nil))

	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestGetThumbnailAfterProcessing(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploaded := uploadAvatar(t, env, "user-1")
	processAvatar(t, env, uploaded.ID)

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+uploaded.ID+"?size=100x100", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))

	config, _, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, err)
	require.Equal(t, 100, config.Width)
	require.Equal(t, 100, config.Height)
}

func TestGetAvatarConvertedFormat(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploaded := uploadAvatar(t, env, "user-1")

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+uploaded.ID+"?format=jpeg", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/jpeg", rec.Header().Get("Content-Type"))

	_, format, err := image.Decode(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
}

func TestGetAvatarConvertedToWebP(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploaded := uploadAvatar(t, env, "user-1")

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+uploaded.ID+"?format=webp", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/webp", rec.Header().Get("Content-Type"))

	config, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, err)
	require.Equal(t, "webp", format)
	require.Equal(t, 640, config.Width)
	require.Equal(t, 480, config.Height)
}

func TestMetadataAfterProcessing(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploaded := uploadAvatar(t, env, "user-1")
	processAvatar(t, env, uploaded.ID)

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+uploaded.ID+"/metadata", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeJSON[handlers.MetadataResponse](t, rec)
	require.Equal(t, uploaded.ID, body.ID)
	require.Equal(t, "avatar.png", body.FileName)
	require.Equal(t, "image/png", body.MimeType)
	require.Equal(t, domain.PublicStatusReady, body.Status)
	require.Equal(t, 640, body.Dimensions.Width)
	require.Equal(t, 480, body.Dimensions.Height)
	require.Len(t, body.Thumbnails, 2)
	require.Equal(t, "100x100", body.Thumbnails[0].Size)
	require.Equal(t, "300x300", body.Thumbnails[1].Size)
}

func TestListUserAvatars(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploadAvatar(t, env, "user-1")
	uploadAvatar(t, env, "user-1")
	uploadAvatar(t, env, "user-2")

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/users/user-1/avatars", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeJSON[handlers.ListResponse](t, rec)
	require.Len(t, body.Items, 2)

	for _, item := range body.Items {
		require.Equal(t, "user-1", item.UserID)
	}
}

func TestGetUserAvatar(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploadAvatar(t, env, "user-1")

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/users/user-1/avatar", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
}

func TestDeleteAvatarForbiddenForOtherUser(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploaded := uploadAvatar(t, env, "user-1")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/"+uploaded.ID, nil)
	req.Header.Set(handlers.HeaderUserID, "user-2")

	rec := env.do(req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "Forbidden", decodeJSON[handlers.ErrorResponse](t, rec).Error)
}

func TestDeleteAvatar(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploaded := uploadAvatar(t, env, "user-1")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/"+uploaded.ID, nil)
	req.Header.Set(handlers.HeaderUserID, "user-1")

	require.Equal(t, http.StatusNoContent, env.do(req).Code)

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+uploaded.ID, nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeleteUserAvatarRemovesObjects(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploaded := uploadAvatar(t, env, "user-1")
	processAvatar(t, env, uploaded.ID)

	avatarID, err := uuid.Parse(uploaded.ID)
	require.NoError(t, err)

	avatar, err := env.repo.GetByID(context.Background(), avatarID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/user-1/avatar", nil)
	req.Header.Set(handlers.HeaderUserID, "user-1")
	require.Equal(t, http.StatusNoContent, env.do(req).Code)

	err = env.processor.Handle(context.Background(), domain.Event{
		Type:          domain.EventAvatarDeleted,
		AvatarID:      avatar.ID,
		UserID:        avatar.UserID,
		S3Key:         avatar.S3Key,
		ThumbnailKeys: avatar.ThumbnailS3Keys,
	})
	require.NoError(t, err)

	for _, key := range avatar.AllS3Keys() {
		_, err := env.storage.Get(context.Background(), key)
		require.ErrorIs(t, err, domain.ErrAvatarNotFound, "key %s", key)
	}
}

func TestHealth(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)

	rec := env.do(httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeJSON[handlers.HealthResponse](t, rec)
	require.Equal(t, "ok", body.Status)
	require.Equal(t, "ok", body.Components["postgres"].Status)
}

func TestStaticPageIsServed(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)

	rec := env.do(httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	require.Contains(t, rec.Body.String(), "/api/v1/avatars")
}

func TestUnknownRoute(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "Not Found", decodeJSON[handlers.ErrorResponse](t, rec).Error)
}

func TestQueryEscapingIsIrrelevantForUserID(t *testing.T) {
	env := newTestEnv(t, defaultMaxFileSize)
	uploadAvatar(t, env, "user with space")

	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/v1/users/"+url.PathEscape("user with space")+"/avatars", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeJSON[handlers.ListResponse](t, rec)
	require.Len(t, body.Items, 1)
}
