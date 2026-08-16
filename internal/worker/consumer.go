package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
)

//go:generate mockgen -source=consumer.go -destination=mocks/consumer_mock.go -package=mocks

// KafkaReader - часть kafka.Reader, которой пользуется потребитель.
type KafkaReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// Consumer - потребитель событий из Kafka.
type Consumer struct {
	reader    KafkaReader
	processor *Processor
	log       *zap.Logger
}

// NewConsumer создаёт консьюмера.
func NewConsumer(cfg config.Kafka, processor *Processor, log *zap.Logger) (*Consumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka: brokers are required")
	}

	if cfg.Topic == "" {
		return nil, errors.New("kafka: topic is required")
	}

	if cfg.GroupID == "" {
		return nil, errors.New("kafka: group id is required")
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: cfg.Brokers,
		Topic:   cfg.Topic,
		GroupID: cfg.GroupID,
	})

	return &Consumer{reader: reader, processor: processor, log: log}, nil
}

// Run читает события из брокера до отмены контекста или закрытия reader.
func (c *Consumer) Run(ctx context.Context) error {
	defer func() {
		if err := c.reader.Close(); err != nil {
			c.log.Error("close kafka reader", zap.Error(err))
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

		if err := c.HandleMessage(ctx, msg.Value); err != nil {
			c.log.Error("handle message",
				zap.Error(err),
				zap.String("topic", msg.Topic),
				zap.Int("partition", msg.Partition),
				zap.Int64("offset", msg.Offset))

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

// HandleMessage разбирает пейлоад и передаёт в процессор.
func (c *Consumer) HandleMessage(ctx context.Context, payload []byte) error {
	var event domain.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		c.log.Error("malformed event payload", zap.Error(err))

		return nil
	}

	if err := c.processor.Handle(ctx, event); err != nil {
		return fmt.Errorf("handle event %s (%s): %w", event.Type, event.AvatarID, err)
	}

	return nil
}
