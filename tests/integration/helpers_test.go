package integration

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/fireflg/gophprofile/internal/api"
	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain/mocks"
	"github.com/fireflg/gophprofile/internal/handlers"
	"github.com/fireflg/gophprofile/internal/services"
	"github.com/fireflg/gophprofile/internal/worker"
	"github.com/fireflg/gophprofile/web"
)

func nowUTC() time.Time { return time.Now().UTC() }

type testEnv struct {
	router    http.Handler
	repo      *fakeRepo
	storage   *fakeStorage
	processor *worker.Processor
}

func newTestEnv(t *testing.T, maxFileSize int64) *testEnv {
	t.Helper()

	cfg := &config.Config{
		App: config.App{Env: "test", LogLevel: "error"},
		Image: config.Image{
			MaxFileSize:      maxFileSize,
			AllowedMimeTypes: []string{"image/jpeg", "image/png", "image/webp"},
			ThumbnailSizes:   []config.Size{{Width: 100, Height: 100}, {Width: 300, Height: 300}},
		},
	}

	log := zap.NewNop()
	repo := newFakeRepo()
	storage := newFakeStorage("http://storage.test/avatars")

	publisher := mocks.NewMockEventPublisher(gomock.NewController(t))
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	service := services.NewAvatarService(repo, storage, publisher, cfg.Image, log)

	static, err := web.Static()
	require.NoError(t, err)

	router := api.NewRouter(api.Deps{
		Config:  cfg,
		Logger:  log,
		Avatars: handlers.NewAvatarHandler(service, log),
		Health:  handlers.NewHealthHandler(log, handlers.Check{Name: "postgres", Probe: repo.Ping}),
		Static:  static,
	})

	return &testEnv{
		router:    router,
		repo:      repo,
		storage:   storage,
		processor: worker.NewProcessor(repo, storage, cfg.Image.ThumbnailSizes, log),
	}
}

func (e *testEnv) do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)

	return rec
}

func uploadRequest(t *testing.T, userID, fileName string, content []byte) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", fileName)
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

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var payload T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload), "body: %s", rec.Body.String())

	return payload
}

func uploadAvatar(t *testing.T, env *testEnv, userID string) handlers.AvatarResponse {
	t.Helper()

	rec := env.do(uploadRequest(t, userID, "avatar.png", pngBytes(t, 640, 480)))
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	return decodeJSON[handlers.AvatarResponse](t, rec)
}
