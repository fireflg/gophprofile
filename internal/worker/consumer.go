package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/internal/metrics"
	"github.com/fireflg/gophprofile/pkg/ctxmeta"
	"github.com/fireflg/gophprofile/pkg/logger"
	"github.com/fireflg/gophprofile/pkg/otelx"
)

//go:generate mockgen -source=consumer.go -destination=mocks/consumer_mock.gen.go -package=mocks

// processOperation - имя операции в спане; по конвенциям messaging спан
// называется "{операция} {назначение}", например "process avatars.events".
const processOperation = "process"

// KafkaReader - часть kafka.Reader, которой пользуется потребитель.
type KafkaReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// statsReader - источник статистики чтения; его реализует kafka.Reader, но не мок.
type statsReader interface {
	Stats() kafka.ReaderStats
}

// Consumer - потребитель событий из Kafka.
type Consumer struct {
	reader    KafkaReader
	processor *Processor
	log       *slog.Logger
}

// NewConsumer создаёт консьюмера.
func NewConsumer(cfg config.Kafka, processor *Processor, log *slog.Logger) (*Consumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka: brokers are required")
	}

	if cfg.Topic == "" {
		return nil, errors.New("kafka: topic is required")
	}

	if cfg.GroupID == "" {
		return nil, errors.New("kafka: group id is required")
	}

	readerLog := logger.Component(log, "kafka_reader")

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     cfg.Brokers,
		Topic:       cfg.Topic,
		GroupID:     cfg.GroupID,
		Logger:      kafkaLogger(readerLog.Debug),
		ErrorLogger: kafkaLogger(readerLog.Error),
	})

	consumer := &Consumer{reader: reader, processor: processor, log: logger.Component(log, "consumer")}

	if err := metrics.RegisterConsumerLagGauge(consumer.lag); err != nil {
		return nil, fmt.Errorf("register consumer lag gauge: %w", err)
	}

	return consumer, nil
}

func kafkaLogger(write func(msg string, args ...any)) kafka.LoggerFunc {
	return func(msg string, args ...any) {
		write(strings.TrimSpace(fmt.Sprintf(msg, args...)))
	}
}

func (c *Consumer) lag() int64 {
	reader, ok := c.reader.(statsReader)
	if !ok {
		return 0
	}

	return reader.Stats().Lag
}

// Run читает события из брокера до отмены контекста или закрытия reader.
func (c *Consumer) Run(ctx context.Context) error {
	defer func() {
		if err := c.reader.Close(); err != nil {
			c.log.Error("close kafka reader", slog.Any("error", err))
		}
	}()

	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) {
				return nil
			}

			return fmt.Errorf("fetch message: %w", err)
		}

		if err := c.HandleMessage(ctx, msg); err != nil {
			continue
		}

		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return fmt.Errorf("commit message: %w", err)
		}
	}
}

// HandleMessage разбирает пейлоад и передаёт её процессору.
func (c *Consumer) HandleMessage(ctx context.Context, msg kafka.Message) error {
	ctx = otel.GetTextMapPropagator().Extract(ctx, &otelx.HeaderCarrier{Headers: &msg.Headers})

	ctx, span := tracer.Start(ctx, processOperation+" "+msg.Topic, trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()

	span.SetAttributes(
		semconv.MessagingSystemKafka,
		semconv.MessagingOperationName(processOperation),
		semconv.MessagingOperationTypeProcess,
		semconv.MessagingDestinationName(msg.Topic),
		semconv.MessagingDestinationPartitionID(strconv.Itoa(msg.Partition)),
		attribute.Int64("messaging.kafka.offset", msg.Offset),
	)

	var event domain.Event
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		metrics.ObserveConsumedMessage(ctx, metrics.StatusMalformed)
		c.logMessageError(ctx, msg, "malformed event payload", err)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil
	}

	span.SetAttributes(
		attribute.String("event.type", string(event.Type)),
		attribute.String("avatar.id", event.AvatarID.String()),
	)

	if event.UserID != "" {
		ctx = ctxmeta.WithUserID(ctx, event.UserID)

		span.SetAttributes(attribute.String("user_id", event.UserID))
	}

	err := c.processor.Handle(ctx, event)

	metrics.ObserveConsumedMessage(ctx, metrics.Status(err))

	if err != nil {
		err = fmt.Errorf("handle event %s (%s): %w", event.Type, event.AvatarID, err)
		c.logMessageError(ctx, msg, "handle message", err)

		return recordError(span, err)
	}

	return nil
}

func (c *Consumer) logMessageError(ctx context.Context, msg kafka.Message, message string, err error) {
	c.log.ErrorContext(ctx, message,
		slog.Any("error", err),
		slog.String("topic", msg.Topic),
		slog.Int("partition", msg.Partition),
		slog.Int64("offset", msg.Offset))
}
