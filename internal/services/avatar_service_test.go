package services_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/internal/domain/mocks"
	"github.com/fireflg/gophprofile/internal/services"
)

const testMaxFileSize = 10 << 20

// serviceMocks — набор моков портов, из которых собран сервис.
type serviceMocks struct {
	repo      *mocks.MockAvatarRepository
	storage   *mocks.MockFileStorage
	publisher *mocks.MockEventPublisher
}

func newService(t *testing.T) (*services.AvatarService, serviceMocks) {
	t.Helper()

	ctrl := gomock.NewController(t)
	deps := serviceMocks{
		repo:      mocks.NewMockAvatarRepository(ctrl),
		storage:   mocks.NewMockFileStorage(ctrl),
		publisher: mocks.NewMockEventPublisher(ctrl),
	}

	cfg := config.Image{
		MaxFileSize:      testMaxFileSize,
		AllowedMimeTypes: []string{"image/jpeg", "image/png", "image/webp"},
		ThumbnailSizes:   []config.Size{{Width: 100, Height: 100}},
	}

	service, err := services.NewAvatarService(deps.repo, deps.storage, deps.publisher, cfg, zap.NewNop())
	require.NoError(t, err)

	return service, deps
}

func TestNewAvatarServiceRejectsInvalidImageConfig(t *testing.T) {
	tests := map[string]struct {
		cfg  config.Image
		want string
	}{
		"нулевой размер файла": {
			cfg:  config.Image{AllowedMimeTypes: []string{"image/png"}},
			want: "max_file_size must be positive",
		},
		"пустые mime-типы": {
			cfg:  config.Image{MaxFileSize: testMaxFileSize},
			want: "allowed_mime_types must not be empty",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			_, err := services.NewAvatarService(
				mocks.NewMockAvatarRepository(ctrl),
				mocks.NewMockFileStorage(ctrl),
				mocks.NewMockEventPublisher(ctrl),
				tc.cfg,
				zap.NewNop(),
			)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

func TestUploadStoresOriginalAndPublishesEvent(t *testing.T) {
	service, deps := newService(t)
	content := pngBytes(t, 64, 64)

	var created *domain.Avatar

	gomock.InOrder(
		deps.repo.EXPECT().
			Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, avatar *domain.Avatar) error {
				created = avatar

				return nil
			}),
		deps.storage.EXPECT().
			Put(gomock.Any(), gomock.Any(), gomock.Any(), int64(len(content)), "image/png").
			DoAndReturn(func(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
				// В хранилище должен уйти файл целиком, включая байты, прочитанные для сниффинга.
				body, err := io.ReadAll(r)
				require.NoError(t, err)
				require.Equal(t, content, body)
				require.Equal(t, created.S3Key, key)

				return nil
			}),
		deps.repo.EXPECT().SetUploadStatus(gomock.Any(), gomock.Any(), domain.UploadStatusUploaded).Return(nil),
		deps.publisher.EXPECT().
			Publish(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, event domain.Event) error {
				require.Equal(t, domain.EventAvatarUploaded, event.Type)
				require.Equal(t, "user-1", event.UserID)

				return nil
			}),
	)

	avatar, err := service.Upload(t.Context(), domain.UploadInput{
		UserID:   "user-1",
		FileName: "avatar.png",
		Size:     int64(len(content)),
		Reader:   bytes.NewReader(content),
	})

	require.NoError(t, err)
	service.Wait()

	require.Equal(t, "image/png", avatar.MimeType)
	require.Equal(t, domain.UploadStatusUploaded, avatar.UploadStatus)
	require.Equal(t, domain.ProcessingStatusPending, avatar.ProcessingStatus)
	require.Contains(t, avatar.S3Key, "avatars/user-1/"+avatar.ID.String())
}

func TestUploadValidationErrors(t *testing.T) {
	tests := map[string]struct {
		input   domain.UploadInput
		wantErr error
	}{
		"без user_id": {
			input:   domain.UploadInput{Size: 10, Reader: bytes.NewReader([]byte("x"))},
			wantErr: domain.ErrUserIDRequired,
		},
		"слишком большой файл": {
			input:   domain.UploadInput{UserID: "u", Size: testMaxFileSize + 1},
			wantErr: domain.ErrFileTooLarge,
		},
		"пустой файл": {
			input:   domain.UploadInput{UserID: "u", Size: 0},
			wantErr: domain.ErrEmptyFile,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			service, _ := newService(t)

			_, err := service.Upload(t.Context(), tc.input)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestUploadRejectsUnsupportedFormat(t *testing.T) {
	service, _ := newService(t)

	payload := []byte("это точно не картинка, а обычный текст")

	_, err := service.Upload(t.Context(), domain.UploadInput{
		UserID:   "user-1",
		FileName: "notes.txt",
		Size:     int64(len(payload)),
		Reader:   bytes.NewReader(payload),
	})

	require.ErrorIs(t, err, domain.ErrUnsupportedFormat)
}

func TestUploadMarksFailedWhenStorageBreaks(t *testing.T) {
	service, deps := newService(t)
	content := pngBytes(t, 32, 32)
	storageErr := errors.New("s3 недоступен")

	deps.repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	deps.storage.EXPECT().
		Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(storageErr)
	deps.repo.EXPECT().SetUploadStatus(gomock.Any(), gomock.Any(), domain.UploadStatusFailed).Return(nil)

	_, err := service.Upload(t.Context(), domain.UploadInput{
		UserID:   "user-1",
		FileName: "avatar.png",
		Size:     int64(len(content)),
		Reader:   bytes.NewReader(content),
	})

	require.ErrorIs(t, err, storageErr)
}

func TestGetFileReturnsOriginal(t *testing.T) {
	service, deps := newService(t)

	avatar := readyAvatar()
	content := pngBytes(t, 16, 16)

	deps.repo.EXPECT().GetByID(gomock.Any(), avatar.ID).Return(avatar, nil)
	deps.storage.EXPECT().Get(gomock.Any(), avatar.S3Key).Return(&domain.Object{
		Body:         io.NopCloser(bytes.NewReader(content)),
		ContentType:  "image/png",
		Size:         int64(len(content)),
		ETag:         "etag-1",
		LastModified: time.Now().UTC(),
	}, nil)

	result, err := service.GetFile(t.Context(), avatar.ID, "", "")
	require.NoError(t, err)

	defer func() { _ = result.Body.Close() }()

	require.Equal(t, "image/png", result.ContentType)
	require.Equal(t, "etag-1", result.ETag)
}

func TestGetFileConvertsFormat(t *testing.T) {
	service, deps := newService(t)

	avatar := readyAvatar()
	content := pngBytes(t, 16, 16)

	deps.repo.EXPECT().GetByID(gomock.Any(), avatar.ID).Return(avatar, nil)
	deps.storage.EXPECT().Get(gomock.Any(), avatar.S3Key).Return(&domain.Object{
		Body:        io.NopCloser(bytes.NewReader(content)),
		ContentType: "image/png",
		Size:        int64(len(content)),
		ETag:        "etag-1",
	}, nil)

	result, err := service.GetFile(t.Context(), avatar.ID, "", "jpeg")
	require.NoError(t, err)

	defer func() { _ = result.Body.Close() }()

	require.Equal(t, "image/jpeg", result.ContentType)
	// Тело перекодировано — ETag обязан отличаться от исходного.
	require.NotEqual(t, "etag-1", result.ETag)

	_, format, err := image.Decode(result.Body)
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
}

func TestGetFileRejectsUnsupportedTargetFormat(t *testing.T) {
	service, deps := newService(t)

	avatar := readyAvatar()

	deps.repo.EXPECT().GetByID(gomock.Any(), avatar.ID).Return(avatar, nil)
	deps.storage.EXPECT().Get(gomock.Any(), avatar.S3Key).Return(&domain.Object{
		Body:        io.NopCloser(bytes.NewReader(pngBytes(t, 8, 8))),
		ContentType: "image/png",
	}, nil)

	_, err := service.GetFile(t.Context(), avatar.ID, "", "tiff")
	require.ErrorIs(t, err, domain.ErrUnsupportedTargetFormat)
}

func TestGetFileThumbnailNotReady(t *testing.T) {
	service, deps := newService(t)

	avatar := readyAvatar()
	avatar.ProcessingStatus = domain.ProcessingStatusPending
	avatar.ThumbnailS3Keys = map[string]string{}

	deps.repo.EXPECT().GetByID(gomock.Any(), avatar.ID).Return(avatar, nil)

	_, err := service.GetFile(t.Context(), avatar.ID, "100x100", "")
	require.ErrorIs(t, err, domain.ErrThumbnailNotReady)
}

func TestDeleteForbiddenForOtherUser(t *testing.T) {
	service, deps := newService(t)

	avatar := readyAvatar()
	deps.repo.EXPECT().GetByID(gomock.Any(), avatar.ID).Return(avatar, nil)

	require.ErrorIs(t, service.Delete(t.Context(), avatar.ID, "user-2"), domain.ErrForbidden)
}

func TestDeleteRequiresRequester(t *testing.T) {
	service, _ := newService(t)

	require.ErrorIs(t, service.Delete(t.Context(), uuid.New(), ""), domain.ErrUserIDRequired)
}

func TestDeleteSoftDeletesAndPublishes(t *testing.T) {
	service, deps := newService(t)

	avatar := readyAvatar()

	gomock.InOrder(
		deps.repo.EXPECT().GetByID(gomock.Any(), avatar.ID).Return(avatar, nil),
		deps.repo.EXPECT().SoftDelete(gomock.Any(), avatar.ID).Return(nil),
		deps.publisher.EXPECT().
			Publish(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, event domain.Event) error {
				require.Equal(t, domain.EventAvatarDeleted, event.Type)
				require.Equal(t, avatar.ThumbnailS3Keys, event.ThumbnailKeys)

				return nil
			}),
	)

	require.NoError(t, service.Delete(t.Context(), avatar.ID, avatar.UserID))
	service.Wait()
}

func TestDeleteUserAvatarChecksOwnership(t *testing.T) {
	service, _ := newService(t)

	require.ErrorIs(t, service.DeleteUserAvatar(t.Context(), "user-1", "user-2"), domain.ErrForbidden)
}

// Сбой брокера не должен ломать пользовательский запрос: событие догонит воркер позже.
func TestPublishFailureDoesNotBreakDelete(t *testing.T) {
	service, deps := newService(t)

	avatar := readyAvatar()

	deps.repo.EXPECT().GetByID(gomock.Any(), avatar.ID).Return(avatar, nil)
	deps.repo.EXPECT().SoftDelete(gomock.Any(), avatar.ID).Return(nil)
	deps.publisher.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(errors.New("kafka недоступна"))

	require.NoError(t, service.Delete(t.Context(), avatar.ID, avatar.UserID))
	service.Wait()
}

func TestServiceLogsAreTaggedWithComponent(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := mocks.NewMockAvatarRepository(ctrl)
	storage := mocks.NewMockFileStorage(ctrl)
	publisher := mocks.NewMockEventPublisher(ctrl)

	core, logs := observer.New(zapcore.DebugLevel)

	service, err := services.NewAvatarService(repo, storage, publisher, config.Image{
		MaxFileSize:      testMaxFileSize,
		AllowedMimeTypes: []string{"image/png"},
	}, zap.New(core))
	require.NoError(t, err)

	avatar := readyAvatar()

	repo.EXPECT().GetByID(gomock.Any(), avatar.ID).Return(avatar, nil)
	repo.EXPECT().SoftDelete(gomock.Any(), avatar.ID).Return(nil)
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(errors.New("kafka is down"))

	require.NoError(t, service.Delete(t.Context(), avatar.ID, avatar.UserID))
	service.Wait()

	require.Equal(t, 1, logs.Len())
	require.Equal(t, "avatar_service", logs.All()[0].ContextMap()["component"])
}

func TestDeleteDoesNotWaitForPublish(t *testing.T) {
	service, deps := newService(t)

	avatar := readyAvatar()
	started := make(chan struct{})
	release := make(chan struct{})

	deps.repo.EXPECT().GetByID(gomock.Any(), avatar.ID).Return(avatar, nil)
	deps.repo.EXPECT().SoftDelete(gomock.Any(), avatar.ID).Return(nil)
	deps.publisher.EXPECT().
		Publish(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ domain.Event) error {
			close(started)
			<-release

			return nil
		})

	require.NoError(t, service.Delete(t.Context(), avatar.ID, avatar.UserID))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("publish did not start")
	}

	close(release)
	service.Wait()
}

// Контекст запроса отменяется, как только обработчик вернул ответ, — фоновая публикация это переживает.
func TestPublishSurvivesCancelledRequestContext(t *testing.T) {
	service, deps := newService(t)

	avatar := readyAvatar()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var publishErr error

	deps.repo.EXPECT().GetByID(gomock.Any(), avatar.ID).Return(avatar, nil)
	deps.repo.EXPECT().SoftDelete(gomock.Any(), avatar.ID).Return(nil)
	deps.publisher.EXPECT().
		Publish(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ domain.Event) error {
			publishErr = ctx.Err()

			return publishErr
		})

	require.NoError(t, service.Delete(ctx, avatar.ID, avatar.UserID))
	service.Wait()

	require.NoError(t, publishErr)
}

func TestURLDelegatesToStorage(t *testing.T) {
	service, deps := newService(t)

	deps.storage.EXPECT().URL("avatars/user-1/original.png").Return("http://storage.test/avatars/user-1/original.png")

	require.Equal(t, "http://storage.test/avatars/user-1/original.png",
		service.URL("avatars/user-1/original.png"))
}

func readyAvatar() *domain.Avatar {
	id := uuid.New()

	return &domain.Avatar{
		ID:               id,
		UserID:           "user-1",
		FileName:         "avatar.png",
		MimeType:         "image/png",
		S3Key:            "avatars/user-1/" + id.String() + "/original.png",
		ThumbnailS3Keys:  map[string]string{"100x100": "avatars/user-1/" + id.String() + "/100x100.jpg"},
		UploadStatus:     domain.UploadStatusUploaded,
		ProcessingStatus: domain.ProcessingStatusReady,
		CreatedAt:        time.Now().UTC(),
	}
}
