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
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.uber.org/mock/gomock"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
	domainmocks "github.com/fireflg/gophprofile/internal/domain/mocks"
	workermocks "github.com/fireflg/gophprofile/internal/worker/mocks"
	kafkaxmocks "github.com/fireflg/gophprofile/pkg/kafkax/mocks"
	"github.com/fireflg/gophprofile/pkg/logger"
)

const testTopic = "avatars.events"

type consumerFixture struct {
	consumer *Consumer
	reader   *workermocks.MockKafkaReader
	client   *kafkaxmocks.MockMetadataClient
	repo     *domainmocks.MockAvatarRepository
	storage  *domainmocks.MockFileStorage
}

func newConsumerFixture(t *testing.T) *consumerFixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	reader := workermocks.NewMockKafkaReader(ctrl)
	client := kafkaxmocks.NewMockMetadataClient(ctrl)
	repo := domainmocks.NewMockAvatarRepository(ctrl)
	storage := domainmocks.NewMockFileStorage(ctrl)

	sizes := []config.Size{{Width: 100, Height: 100}}
	log := logger.Nop()

	processor, err := NewProcessor(repo, storage, sizes, log)
	require.NoError(t, err)

	return &consumerFixture{
		consumer: &Consumer{
			reader:    reader,
			client:    client,
			topic:     testTopic,
			processor: processor,
			log:       log,
		},
		reader:  reader,
		client:  client,
		repo:    repo,
		storage: storage,
	}
}

func eventMessage(t *testing.T, event domain.Event) kafka.Message {
	t.Helper()

	payload, err := json.Marshal(event)
	require.NoError(t, err)

	return kafka.Message{Topic: testTopic, Partition: 0, Offset: 7, Value: payload}
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
	log := logger.Nop()

	_, err := NewConsumer(config.Kafka{Topic: testTopic, GroupID: "avatars-worker"}, nil, log)
	require.ErrorContains(t, err, "brokers")

	_, err = NewConsumer(config.Kafka{Brokers: []string{"localhost:9092"}, GroupID: "avatars-worker"}, nil, log)
	require.ErrorContains(t, err, "topic")

	_, err = NewConsumer(config.Kafka{Brokers: []string{"localhost:9092"}, Topic: testTopic}, nil, log)
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

	fetchThenStop(fixture.reader, kafka.Message{Topic: testTopic, Value: []byte("{не json")})
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

func TestPingFailsWhenLoopIsNotRunning(t *testing.T) {
	fixture := newConsumerFixture(t)

	require.ErrorContains(t, fixture.consumer.Ping(t.Context()), "not running")

	fixture.reader.EXPECT().FetchMessage(gomock.Any()).Return(kafka.Message{}, io.EOF)
	fixture.reader.EXPECT().Close().Return(nil)

	require.NoError(t, fixture.consumer.Run(t.Context()))

	require.ErrorContains(t, fixture.consumer.Ping(t.Context()), "not running")
}

func TestPingChecksTopicMetadataWhileRunning(t *testing.T) {
	fixture := newConsumerFixture(t)

	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})

	fixture.reader.EXPECT().
		FetchMessage(gomock.Any()).
		DoAndReturn(func(ctx context.Context) (kafka.Message, error) {
			close(started)
			<-ctx.Done()

			return kafka.Message{}, ctx.Err()
		})
	fixture.reader.EXPECT().Close().Return(nil)

	stopped := make(chan error, 1)
	go func() { stopped <- fixture.consumer.Run(ctx) }()

	<-started

	fixture.client.EXPECT().Metadata(gomock.Any(), gomock.Any()).Return(topicMetadata(kafka.Topic{
		Name:       testTopic,
		Partitions: []kafka.Partition{{Topic: testTopic, ID: 0}},
	}), nil)
	require.NoError(t, fixture.consumer.Ping(ctx))

	fixture.client.EXPECT().Metadata(gomock.Any(), gomock.Any()).Return(topicMetadata(kafka.Topic{
		Name: testTopic,
	}), nil)
	require.ErrorContains(t, fixture.consumer.Ping(ctx), "no partitions")

	cancel()
	require.NoError(t, <-stopped)
}

func topicMetadata(topic kafka.Topic) *kafka.MetadataResponse {
	return &kafka.MetadataResponse{
		Brokers: []kafka.Broker{{ID: 1, Host: "localhost", Port: 9092}},
		Topics:  []kafka.Topic{topic},
	}
}

func TestHandleMessageSkipsMalformedPayload(t *testing.T) {
	fixture := newConsumerFixture(t)

	msg := kafka.Message{Topic: testTopic, Partition: 3, Offset: 42, Value: []byte("{не json")}

	require.NoError(t, fixture.consumer.HandleMessage(t.Context(), msg))
}

func TestHandleMessageReturnsProcessingFailure(t *testing.T) {
	fixture := newConsumerFixture(t)

	event := uploadedMessageEvent()

	fixture.repo.EXPECT().
		GetByID(gomock.Any(), event.AvatarID).
		Return(nil, errors.New("база недоступна"))

	err := fixture.consumer.HandleMessage(t.Context(), eventMessage(t, event))
	require.ErrorContains(t, err, "база недоступна")
}

func TestHandleMessageSpanFollowsMessagingConvention(t *testing.T) {
	recorder := recordSpans(t)
	fixture := newConsumerFixture(t)

	msg := kafka.Message{Topic: testTopic, Partition: 3, Offset: 42, Value: []byte("{не json")}
	require.NoError(t, fixture.consumer.HandleMessage(t.Context(), msg))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "process avatars.events", spans[0].Name())
	require.Contains(t, spans[0].Attributes(), semconv.MessagingOperationName("process"))
	require.Contains(t, spans[0].Attributes(), semconv.MessagingOperationTypeProcess)
	require.Contains(t, spans[0].Attributes(), semconv.MessagingDestinationName(testTopic))
}

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	previous := tracer
	tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")

	t.Cleanup(func() { tracer = previous })

	return recorder
}
