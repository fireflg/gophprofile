package services

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/fireflg/gophprofile/internal/services/mocks"
)

const testTopic = "avatars.events"

func newTestPublisher(t *testing.T) (*KafkaPublisher, *mocks.MockKafkaWriter, *mocks.MockKafkaMetadataClient) {
	t.Helper()

	ctrl := gomock.NewController(t)
	writer := mocks.NewMockKafkaWriter(ctrl)
	client := mocks.NewMockKafkaMetadataClient(ctrl)

	return &KafkaPublisher{writer: writer, client: client, topic: testTopic}, writer, client
}

func testEvent() domain.Event {
	return domain.Event{
		Type:     domain.EventAvatarUploaded,
		AvatarID: uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"),
		UserID:   "user-1",
		S3Key:    "avatars/user-1/original.png",
		MimeType: "image/png",
	}
}

func metadataWithTopic(topic kafka.Topic) *kafka.MetadataResponse {
	return &kafka.MetadataResponse{
		Brokers: []kafka.Broker{{ID: 1, Host: "kafka", Port: 9092}},
		Topics:  []kafka.Topic{topic},
	}
}

func TestNewKafkaPublisherRejectsIncompleteConfig(t *testing.T) {
	_, err := NewKafkaPublisher(config.Kafka{Topic: testTopic})
	require.ErrorContains(t, err, "brokers")

	_, err = NewKafkaPublisher(config.Kafka{Brokers: []string{"localhost:9092"}})
	require.ErrorContains(t, err, "topic")

	_, err = NewKafkaPublisher(config.Kafka{Brokers: []string{"localhost:9092"}, Topic: testTopic})
	require.ErrorContains(t, err, "write_timeout")
}

func TestPublishSendsEventAsJSON(t *testing.T) {
	publisher, writer, _ := newTestPublisher(t)
	event := testEvent()

	writer.EXPECT().
		WriteMessages(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msgs ...kafka.Message) error {
			require.Len(t, msgs, 1)
			require.Equal(t, event.AvatarID.String(), string(msgs[0].Key))

			var decoded domain.Event
			require.NoError(t, json.Unmarshal(msgs[0].Value, &decoded))
			require.Equal(t, event, decoded)

			return nil
		})

	require.NoError(t, publisher.Publish(t.Context(), event))
}

func TestPublishSetsHeaders(t *testing.T) {
	publisher, writer, _ := newTestPublisher(t)

	writer.EXPECT().
		WriteMessages(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msgs ...kafka.Message) error {
			headers := make(map[string]string, len(msgs[0].Headers))
			for _, header := range msgs[0].Headers {
				headers[header.Key] = string(header.Value)
			}

			require.Equal(t, "application/json", headers["content-type"])
			require.Equal(t, "v1", headers["schema-version"])

			return nil
		})

	require.NoError(t, publisher.Publish(t.Context(), testEvent()))
}

func TestPublishReturnsWriterError(t *testing.T) {
	publisher, writer, _ := newTestPublisher(t)
	writeErr := errors.New("брокер недоступен")

	writer.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Return(writeErr)

	require.ErrorIs(t, publisher.Publish(t.Context(), testEvent()), writeErr)
}

func TestPublishSpanFollowsMessagingConvention(t *testing.T) {
	recorder := recordSpans(t)
	publisher, writer, _ := newTestPublisher(t)

	writer.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Return(nil)

	require.NoError(t, publisher.Publish(t.Context(), testEvent()))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "publish "+testTopic, spans[0].Name())
	require.Contains(t, spans[0].Attributes(), semconv.MessagingOperationName("publish"))
	require.Contains(t, spans[0].Attributes(), semconv.MessagingOperationTypeSend)
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

func TestPingAsksMetadataForTopic(t *testing.T) {
	publisher, _, client := newTestPublisher(t)

	client.EXPECT().
		Metadata(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, req *kafka.MetadataRequest) (*kafka.MetadataResponse, error) {
			require.Equal(t, []string{testTopic}, req.Topics)

			return metadataWithTopic(kafka.Topic{
				Name:       testTopic,
				Partitions: []kafka.Partition{{Topic: testTopic, ID: 0}},
			}), nil
		})

	require.NoError(t, publisher.Ping(t.Context()))
}

func TestPingDoesNotWriteToTopic(t *testing.T) {
	publisher, _, client := newTestPublisher(t)

	client.EXPECT().Metadata(gomock.Any(), gomock.Any()).Return(metadataWithTopic(kafka.Topic{
		Name:       testTopic,
		Partitions: []kafka.Partition{{Topic: testTopic, ID: 0}},
	}), nil)

	require.NoError(t, publisher.Ping(t.Context()))
}

func TestPingFailsWhenMetadataUnavailable(t *testing.T) {
	publisher, _, client := newTestPublisher(t)
	metadataErr := errors.New("connection refused")

	client.EXPECT().Metadata(gomock.Any(), gomock.Any()).Return(nil, metadataErr)

	require.ErrorIs(t, publisher.Ping(t.Context()), metadataErr)
}

func TestPingFailsWhenClusterHasNoBrokers(t *testing.T) {
	publisher, _, client := newTestPublisher(t)

	client.EXPECT().Metadata(gomock.Any(), gomock.Any()).Return(&kafka.MetadataResponse{}, nil)

	require.ErrorContains(t, publisher.Ping(t.Context()), "no brokers")
}

func TestPingFailsWhenTopicReportsError(t *testing.T) {
	publisher, _, client := newTestPublisher(t)

	client.EXPECT().Metadata(gomock.Any(), gomock.Any()).Return(metadataWithTopic(kafka.Topic{
		Name:  testTopic,
		Error: kafka.UnknownTopicOrPartition,
	}), nil)

	require.ErrorIs(t, publisher.Ping(t.Context()), kafka.UnknownTopicOrPartition)
}

func TestPingFailsWhenTopicHasNoPartitions(t *testing.T) {
	publisher, _, client := newTestPublisher(t)

	client.EXPECT().Metadata(gomock.Any(), gomock.Any()).Return(metadataWithTopic(kafka.Topic{
		Name: testTopic,
	}), nil)

	require.ErrorContains(t, publisher.Ping(t.Context()), "no partitions")
}

func TestPingFailsWhenTopicIsMissing(t *testing.T) {
	publisher, _, client := newTestPublisher(t)

	client.EXPECT().Metadata(gomock.Any(), gomock.Any()).Return(metadataWithTopic(kafka.Topic{
		Name:       "other.topic",
		Partitions: []kafka.Partition{{Topic: "other.topic", ID: 0}},
	}), nil)

	require.ErrorContains(t, publisher.Ping(t.Context()), "is missing")
}

func TestCloseClosesWriter(t *testing.T) {
	publisher, writer, _ := newTestPublisher(t)
	closeErr := errors.New("не удалось дозаписать буфер")

	writer.EXPECT().Close().Return(closeErr)

	require.ErrorIs(t, publisher.Close(), closeErr)
}
