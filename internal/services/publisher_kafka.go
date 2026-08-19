package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/pkg/otelx"
)

//go:generate mockgen -source=publisher_kafka.go -destination=mocks/publisher_mock.gen.go -package=mocks

// KafkaWriter — продюсер сообщений Kafka.
type KafkaWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// KafkaMetadataClient — доступ к метаданным кластера.
type KafkaMetadataClient interface {
	Metadata(ctx context.Context, req *kafka.MetadataRequest) (*kafka.MetadataResponse, error)
}

// KafkaPublisher — адаптер публикации событий в Kafka.
//
// Потребитель этих событий — internal/worker/consumer.go.
type KafkaPublisher struct {
	writer KafkaWriter
	client KafkaMetadataClient
	topic  string
}

var _ domain.EventPublisher = (*KafkaPublisher)(nil)

// NewKafkaPublisher создаёт продюсер событий.
func NewKafkaPublisher(cfg config.Kafka) (*KafkaPublisher, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka: brokers are required")
	}

	if cfg.Topic == "" {
		return nil, errors.New("kafka: topic is required")
	}

	if cfg.WriteTimeout <= 0 {
		return nil, errors.New("kafka: write_timeout must be positive")
	}

	addr := kafka.TCP(cfg.Brokers...)

	return &KafkaPublisher{
		writer: &kafka.Writer{
			Addr:                   addr,
			Topic:                  cfg.Topic,
			Balancer:               &kafka.Hash{},
			RequiredAcks:           kafka.RequireAll,
			BatchTimeout:           10 * time.Millisecond,
			AllowAutoTopicCreation: false,
			WriteTimeout:           cfg.WriteTimeout,
		},
		client: &kafka.Client{Addr: addr},
		topic:  cfg.Topic,
	}, nil
}

// Publish отправляет событие в топик.
func (p *KafkaPublisher) Publish(ctx context.Context, event domain.Event) error {
	ctx, span := tracer.Start(ctx, p.topic+" publish", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	span.SetAttributes(
		semconv.MessagingSystemKafka,
		semconv.MessagingDestinationName(p.topic),
		attribute.String("event.type", string(event.Type)),
		attribute.String("avatar.id", event.AvatarID.String()),
	)

	payload, err := json.Marshal(event)
	if err != nil {
		return recordError(span, err)
	}
	msg := kafka.Message{
		Key:   []byte(event.AvatarID.String()),
		Value: payload,
		Headers: []kafka.Header{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "schema-version", Value: []byte("v1")},
		},
	}
	otel.GetTextMapPropagator().Inject(ctx, &otelx.HeaderCarrier{Headers: &msg.Headers})

	return recordError(span, p.writer.WriteMessages(ctx, msg))
}

// Ping проверяет доступность брокера запросом метаданных топика.
func (p *KafkaPublisher) Ping(ctx context.Context) error {
	resp, err := p.client.Metadata(ctx, &kafka.MetadataRequest{Topics: []string{p.topic}})
	if err != nil {
		return fmt.Errorf("kafka metadata: %w", err)
	}

	if len(resp.Brokers) == 0 {
		return errors.New("kafka: cluster reported no brokers")
	}

	for _, topic := range resp.Topics {
		if topic.Name != p.topic {
			continue
		}

		if topic.Error != nil {
			return fmt.Errorf("kafka topic %s: %w", p.topic, topic.Error)
		}

		if len(topic.Partitions) == 0 {
			return fmt.Errorf("kafka topic %s has no partitions", p.topic)
		}

		return nil
	}

	return fmt.Errorf("kafka topic %s is missing", p.topic)
}

// Close закрывает продюсер.
func (p *KafkaPublisher) Close() error {
	return p.writer.Close()
}
