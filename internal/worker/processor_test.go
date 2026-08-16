package worker_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/internal/domain/mocks"
	"github.com/fireflg/gophprofile/internal/worker"
)

var thumbnailSizes = []config.Size{{Width: 100, Height: 100}, {Width: 300, Height: 300}}

func newProcessor(t *testing.T) (*worker.Processor, *mocks.MockAvatarRepository, *mocks.MockFileStorage) {
	t.Helper()

	ctrl := gomock.NewController(t)
	repo := mocks.NewMockAvatarRepository(ctrl)
	storage := mocks.NewMockFileStorage(ctrl)

	return worker.NewProcessor(repo, storage, thumbnailSizes, zap.NewNop()), repo, storage
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{B: 255, A: 255})

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

func jpegBytes(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{B: 255, A: 255})

	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))

	return buf.Bytes()
}

func uploadedEvent() domain.Event {
	id := uuid.New()

	return domain.Event{
		Type:     domain.EventAvatarUploaded,
		AvatarID: id,
		UserID:   "user-1",
		S3Key:    "avatars/user-1/" + id.String() + "/original.png",
		MimeType: "image/png",
	}
}

func expectPendingAvatar(repo *mocks.MockAvatarRepository, event domain.Event) {
	repo.EXPECT().GetByID(gomock.Any(), event.AvatarID).Return(&domain.Avatar{
		ID:               event.AvatarID,
		UserID:           event.UserID,
		S3Key:            event.S3Key,
		MimeType:         event.MimeType,
		ProcessingStatus: domain.ProcessingStatusPending,
	}, nil)
}

func TestHandleUploadedCreatesThumbnails(t *testing.T) {
	processor, repo, storage := newProcessor(t)

	event := uploadedEvent()
	original := pngBytes(t, 640, 480)

	stored := make(map[string][]byte, len(thumbnailSizes))

	expectPendingAvatar(repo, event)
	repo.EXPECT().SetProcessingStatus(gomock.Any(), event.AvatarID, domain.ProcessingStatusProcessing).Return(nil)
	storage.EXPECT().Get(gomock.Any(), event.S3Key).Return(&domain.Object{
		Body:        io.NopCloser(bytes.NewReader(original)),
		ContentType: "image/png",
	}, nil)

	storage.EXPECT().
		Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), "image/png").
		DoAndReturn(func(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
			body, err := io.ReadAll(r)
			require.NoError(t, err)
			stored[key] = body

			return nil
		}).
		Times(len(thumbnailSizes))

	repo.EXPECT().
		SaveProcessingResult(gomock.Any(), event.AvatarID, gomock.Any(), 640, 480).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, thumbnails map[string]string, _, _ int) error {
			require.Len(t, thumbnails, len(thumbnailSizes))
			require.Contains(t, thumbnails, "100x100")
			require.Contains(t, thumbnails, "300x300")

			for _, key := range thumbnails {
				require.Contains(t, stored, key)
			}

			return nil
		})

	require.NoError(t, processor.Handle(t.Context(), event))

	// PNG-оригинал остаётся PNG: каждая миниатюра — валидный квадратный PNG.
	require.Len(t, stored, len(thumbnailSizes))

	for key, body := range stored {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
		require.NoError(t, err, "key %s", key)
		require.Equal(t, "png", format)
		require.Equal(t, cfg.Width, cfg.Height)
	}
}

func TestHandleUploadedEncodesNonPNGThumbnailsAsJPEG(t *testing.T) {
	processor, repo, storage := newProcessor(t)

	event := uploadedEvent()
	event.S3Key = "avatars/user-1/" + event.AvatarID.String() + "/original.jpg"
	event.MimeType = "image/jpeg"

	expectPendingAvatar(repo, event)
	repo.EXPECT().SetProcessingStatus(gomock.Any(), event.AvatarID, domain.ProcessingStatusProcessing).Return(nil)
	storage.EXPECT().Get(gomock.Any(), event.S3Key).Return(&domain.Object{
		Body:        io.NopCloser(bytes.NewReader(jpegBytes(t, 640, 480))),
		ContentType: "image/jpeg",
	}, nil)

	storage.EXPECT().
		Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), "image/jpeg").
		Return(nil).
		Times(len(thumbnailSizes))

	repo.EXPECT().
		SaveProcessingResult(gomock.Any(), event.AvatarID, gomock.Any(), 640, 480).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, thumbnails map[string]string, _, _ int) error {
			for _, key := range thumbnails {
				require.True(t, strings.HasSuffix(key, ".jpg"), "key %s", key)
			}

			return nil
		})

	require.NoError(t, processor.Handle(t.Context(), event))
}

func TestHandleUploadedMarksFailedOnBrokenImage(t *testing.T) {
	processor, repo, storage := newProcessor(t)

	event := uploadedEvent()

	expectPendingAvatar(repo, event)
	repo.EXPECT().SetProcessingStatus(gomock.Any(), event.AvatarID, domain.ProcessingStatusProcessing).Return(nil)
	storage.EXPECT().Get(gomock.Any(), event.S3Key).Return(&domain.Object{
		Body: io.NopCloser(bytes.NewReader([]byte("это не изображение"))),
	}, nil)
	repo.EXPECT().SetProcessingStatus(gomock.Any(), event.AvatarID, domain.ProcessingStatusFailed).Return(nil)

	require.Error(t, processor.Handle(t.Context(), event))
}

func TestHandleUploadedPropagatesStorageError(t *testing.T) {
	processor, repo, storage := newProcessor(t)

	event := uploadedEvent()
	storageErr := errors.New("объект недоступен")

	expectPendingAvatar(repo, event)
	repo.EXPECT().SetProcessingStatus(gomock.Any(), event.AvatarID, domain.ProcessingStatusProcessing).Return(nil)
	storage.EXPECT().Get(gomock.Any(), event.S3Key).Return(nil, storageErr)
	repo.EXPECT().SetProcessingStatus(gomock.Any(), event.AvatarID, domain.ProcessingStatusFailed).Return(nil)

	require.ErrorIs(t, processor.Handle(t.Context(), event), storageErr)
}

func TestHandleUploadedSkipsAlreadyProcessedAvatar(t *testing.T) {
	processor, repo, _ := newProcessor(t)

	event := uploadedEvent()

	repo.EXPECT().GetByID(gomock.Any(), event.AvatarID).Return(&domain.Avatar{
		ID:               event.AvatarID,
		UserID:           event.UserID,
		S3Key:            event.S3Key,
		ProcessingStatus: domain.ProcessingStatusReady,
		ThumbnailS3Keys:  map[string]string{"100x100": "avatars/user-1/100x100.png"},
	}, nil)

	require.NoError(t, processor.Handle(t.Context(), event))
}

func TestHandleUploadedSkipsDeletedAvatar(t *testing.T) {
	processor, repo, _ := newProcessor(t)

	event := uploadedEvent()

	repo.EXPECT().GetByID(gomock.Any(), event.AvatarID).Return(nil, domain.ErrAvatarNotFound)

	require.NoError(t, processor.Handle(t.Context(), event))
}

func TestHandleUploadedTwiceProcessesOnlyOnce(t *testing.T) {
	processor, repo, storage := newProcessor(t)

	event := uploadedEvent()
	ready := false

	repo.EXPECT().
		GetByID(gomock.Any(), event.AvatarID).
		DoAndReturn(func(_ context.Context, id uuid.UUID) (*domain.Avatar, error) {
			status := domain.ProcessingStatusPending
			if ready {
				status = domain.ProcessingStatusReady
			}

			return &domain.Avatar{ID: id, UserID: event.UserID, S3Key: event.S3Key, ProcessingStatus: status}, nil
		}).
		Times(2)

	repo.EXPECT().SetProcessingStatus(gomock.Any(), event.AvatarID, domain.ProcessingStatusProcessing).Return(nil)
	storage.EXPECT().Get(gomock.Any(), event.S3Key).Return(&domain.Object{
		Body:        io.NopCloser(bytes.NewReader(pngBytes(t, 640, 480))),
		ContentType: "image/png",
	}, nil)
	storage.EXPECT().Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		Times(len(thumbnailSizes))
	repo.EXPECT().
		SaveProcessingResult(gomock.Any(), event.AvatarID, gomock.Any(), 640, 480).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, _ map[string]string, _, _ int) error {
			ready = true

			return nil
		})

	require.NoError(t, processor.Handle(t.Context(), event))
	require.NoError(t, processor.Handle(t.Context(), event))
}

func TestHandleDeletedRemovesAllObjects(t *testing.T) {
	processor, _, storage := newProcessor(t)

	event := domain.Event{
		Type:          domain.EventAvatarDeleted,
		AvatarID:      uuid.New(),
		UserID:        "user-1",
		S3Key:         "avatars/user-1/original.png",
		ThumbnailKeys: map[string]string{"100x100": "avatars/user-1/100x100.jpg"},
	}

	storage.EXPECT().
		Delete(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, keys ...string) error {
			require.ElementsMatch(t, []string{event.S3Key, "avatars/user-1/100x100.jpg"}, keys)

			return nil
		})

	require.NoError(t, processor.Handle(t.Context(), event))
}

func TestHandleDeletedWithoutKeysDoesNothing(t *testing.T) {
	processor, _, _ := newProcessor(t)

	event := domain.Event{Type: domain.EventAvatarDeleted, AvatarID: uuid.New()}

	require.NoError(t, processor.Handle(t.Context(), event))
}

func TestHandleUnknownEventTypeIsIgnored(t *testing.T) {
	processor, _, _ := newProcessor(t)

	event := domain.Event{Type: "avatar.unknown", AvatarID: uuid.New()}

	require.NoError(t, processor.Handle(t.Context(), event))
}
