package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
	domainmocks "github.com/fireflg/gophprofile/internal/domain/mocks"
	workermocks "github.com/fireflg/gophprofile/internal/worker/mocks"
)

type consumerFixture struct {
	consumer *Consumer
	reader   *workermocks.MockKafkaReader
	repo     *domainmocks.MockAvatarRepository
	storage  *domainmocks.MockFileStorage
}

func newConsumerFixture(t *testing.T) *consumerFixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	reader := workermocks.NewMockKafkaReader(ctrl)
	repo := domainmocks.NewMockAvatarRepository(ctrl)
	storage := domainmocks.NewMockFileStorage(ctrl)

	sizes := []config.Size{{Width: 100, Height: 100}}
	log := zap.NewNop()

	processor, err := NewProcessor(repo, storage, sizes, log)
	require.NoError(t, err)

	return &consumerFixture{
		consumer: &Consumer{
			reader:    reader,
			processor: processor,
			log:       log,
		},
		reader:  reader,
		repo:    repo,
		storage: storage,
	}
}

func eventMessage(t *testing.T, event domain.Event) kafka.Message {
	t.Helper()

	payload, err := json.Marshal(event)
	require.NoError(t, err)

	return kafka.Message{Topic: "avatars.events", Partition: 0, Offset: 7, Value: payload}
}

func deletedEvent() domain.Event {
	return domain.Event{Type: domain.EventAvatarDeleted, AvatarID: uuid.New(), UserID: "user-1"}
}

func uploadedMessageEvent() domain.Event {
	id := uuid.New()

	return domain.Event{
		Type:     domain.EventAvatarUploaded,
		AvatarID: id,
		UserID:   "user-1",
		S3Key:    "avatars/user-1/" + id.String() + "/original.png",
		MimeType: "image/png",
	}
}

func smallPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

func fetchThenStop(reader *workermocks.MockKafkaReader, msg kafka.Message) {
	gomock.InOrder(
		reader.EXPECT().FetchMessage(gomock.Any()).Return(msg, nil),
		reader.EXPECT().FetchMessage(gomock.Any()).Return(kafka.Message{}, io.EOF),
	)
	reader.EXPECT().Close().Return(nil)
}

func TestNewConsumerRejectsIncompleteConfig(t *testing.T) {
	log := zap.NewNop()

	_, err := NewConsumer(config.Kafka{Topic: "avatars.events", GroupID: "avatars-worker"}, nil, log)
	require.ErrorContains(t, err, "brokers")

	_, err = NewConsumer(config.Kafka{Brokers: []string{"localhost:9092"}, GroupID: "avatars-worker"}, nil, log)
	require.ErrorContains(t, err, "topic")

	_, err = NewConsumer(config.Kafka{Brokers: []string{"localhost:9092"}, Topic: "avatars.events"}, nil, log)
	require.ErrorContains(t, err, "group id")
}

func TestRunCommitsAfterSuccessfulHandling(t *testing.T) {
	fixture := newConsumerFixture(t)

	event := uploadedMessageEvent()
	msg := eventMessage(t, event)

	fixture.repo.EXPECT().
		GetByID(gomock.Any(), event.AvatarID).
		Return(&domain.Avatar{ID: event.AvatarID, S3Key: event.S3Key, ProcessingStatus: domain.ProcessingStatusPending}, nil)
	fixture.repo.EXPECT().
		SetProcessingStatus(gomock.Any(), event.AvatarID, domain.ProcessingStatusProcessing).
		Return(nil)
	fixture.storage.EXPECT().Get(gomock.Any(), event.S3Key).Return(&domain.Object{
		Body: io.NopCloser(bytes.NewReader(smallPNG(t))),
	}, nil)
	fixture.storage.EXPECT().
		Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)
	fixture.repo.EXPECT().
		SaveProcessingResult(gomock.Any(), event.AvatarID, gomock.Any(), 8, 8).
		Return(nil)

	fetchThenStop(fixture.reader, msg)
	fixture.reader.EXPECT().
		CommitMessages(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msgs ...kafka.Message) error {
			require.Len(t, msgs, 1)
			require.Equal(t, msg.Offset, msgs[0].Offset)

			return nil
		})

	require.NoError(t, fixture.consumer.Run(t.Context()))
}

func TestRunDoesNotCommitWhenHandlingFails(t *testing.T) {
	fixture := newConsumerFixture(t)

	event := uploadedMessageEvent()

	fixture.repo.EXPECT().
		GetByID(gomock.Any(), event.AvatarID).
		Return(&domain.Avatar{ID: event.AvatarID, S3Key: event.S3Key, ProcessingStatus: domain.ProcessingStatusPending}, nil)
	fixture.repo.EXPECT().
		SetProcessingStatus(gomock.Any(), event.AvatarID, domain.ProcessingStatusProcessing).
		Return(nil)
	fixture.storage.EXPECT().Get(gomock.Any(), event.S3Key).Return(nil, errors.New("объект недоступен"))
	fixture.repo.EXPECT().
		SetProcessingStatus(gomock.Any(), event.AvatarID, domain.ProcessingStatusFailed).
		Return(nil)

	fetchThenStop(fixture.reader, eventMessage(t, event))

	require.NoError(t, fixture.consumer.Run(t.Context()))
}

func TestRunCommitsMalformedMessage(t *testing.T) {
	fixture := newConsumerFixture(t)

	fetchThenStop(fixture.reader, kafka.Message{Topic: "avatars.events", Value: []byte("{не json")})
	fixture.reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).Return(nil)

	require.NoError(t, fixture.consumer.Run(t.Context()))
}

func TestRunStopsOnContextCancellation(t *testing.T) {
	fixture := newConsumerFixture(t)

	ctx, cancel := context.WithCancel(t.Context())

	fixture.reader.EXPECT().
		FetchMessage(gomock.Any()).
		DoAndReturn(func(ctx context.Context) (kafka.Message, error) {
			cancel()

			return kafka.Message{}, ctx.Err()
		})
	fixture.reader.EXPECT().Close().Return(nil)

	require.NoError(t, fixture.consumer.Run(ctx))
}

func TestRunStopsProcessingAfterCancellation(t *testing.T) {
	fixture := newConsumerFixture(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	fixture.reader.EXPECT().FetchMessage(gomock.Any()).Return(kafka.Message{}, context.Canceled)
	fixture.reader.EXPECT().Close().Return(nil)

	require.NoError(t, fixture.consumer.Run(ctx))
}

func TestRunReturnsFetchError(t *testing.T) {
	fixture := newConsumerFixture(t)

	fetchErr := errors.New("брокер недоступен")

	fixture.reader.EXPECT().FetchMessage(gomock.Any()).Return(kafka.Message{}, fetchErr)
	fixture.reader.EXPECT().Close().Return(nil)

	require.ErrorIs(t, fixture.consumer.Run(t.Context()), fetchErr)
}

func TestRunReturnsCommitError(t *testing.T) {
	fixture := newConsumerFixture(t)

	commitErr := errors.New("коммит не прошёл")

	fixture.reader.EXPECT().
		FetchMessage(gomock.Any()).
		Return(eventMessage(t, deletedEvent()), nil)
	fixture.reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).Return(commitErr)
	fixture.reader.EXPECT().Close().Return(nil)

	require.ErrorIs(t, fixture.consumer.Run(t.Context()), commitErr)
}

func TestRunClosesReaderOnExit(t *testing.T) {
	fixture := newConsumerFixture(t)

	fixture.reader.EXPECT().FetchMessage(gomock.Any()).Return(kafka.Message{}, io.EOF)
	fixture.reader.EXPECT().Close().Return(nil).Times(1)

	require.NoError(t, fixture.consumer.Run(t.Context()))
}

func TestHandleMessageSkipsMalformedPayload(t *testing.T) {
	fixture := newConsumerFixture(t)

	require.NoError(t, fixture.consumer.HandleMessage(t.Context(), kafka.Message{Value: []byte("{не json")}))
}

func TestHandleMessageLogsMalformedPayloadWithCoordinates(t *testing.T) {
	fixture := newConsumerFixture(t)

	core, logs := observer.New(zapcore.ErrorLevel)
	fixture.consumer.log = zap.New(core)

	msg := kafka.Message{Topic: "avatars.events", Partition: 3, Offset: 42, Value: []byte("{не json")}

	require.NoError(t, fixture.consumer.HandleMessage(t.Context(), msg))
	require.Equal(t, 1, logs.Len())

	fields := logs.All()[0].ContextMap()
	require.Equal(t, "malformed event payload", logs.All()[0].Message)
	require.Equal(t, "avatars.events", fields["topic"])
	require.EqualValues(t, 3, fields["partition"])
	require.EqualValues(t, 42, fields["offset"])
}

func TestHandleMessageLogsFailureWithUserID(t *testing.T) {
	fixture := newConsumerFixture(t)

	core, logs := observer.New(zapcore.ErrorLevel)
	fixture.consumer.log = zap.New(core)

	event := uploadedMessageEvent()

	fixture.repo.EXPECT().
		GetByID(gomock.Any(), event.AvatarID).
		Return(nil, errors.New("база недоступна"))

	require.Error(t, fixture.consumer.HandleMessage(t.Context(), eventMessage(t, event)))
	require.Equal(t, 1, logs.Len())

	fields := logs.All()[0].ContextMap()
	require.Equal(t, "handle message", logs.All()[0].Message)
	require.Equal(t, event.UserID, fields["user_id"])
	require.Contains(t, fields["error"], "база недоступна")
}
