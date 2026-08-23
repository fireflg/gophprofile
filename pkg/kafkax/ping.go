// Package kafkax проверяет общие для продюсера и консьюмера обращения к брокеру.
package kafkax

import (
	"context"
	"errors"
	"fmt"

	"github.com/segmentio/kafka-go"
)

//go:generate mockgen -source=ping.go -destination=mocks/kafkax_mock.gen.go -package=mocks

// MetadataClient доступ к метаданным кластера.
type MetadataClient interface {
	Metadata(ctx context.Context, req *kafka.MetadataRequest) (*kafka.MetadataResponse, error)
}

// PingTopic проверяет, что брокер отвечает и топик существует с партициями.
func PingTopic(ctx context.Context, client MetadataClient, topic string) error {
	resp, err := client.Metadata(ctx, &kafka.MetadataRequest{Topics: []string{topic}})
	if err != nil {
		return fmt.Errorf("kafka metadata: %w", err)
	}

	if len(resp.Brokers) == 0 {
		return errors.New("kafka: cluster reported no brokers")
	}

	for _, meta := range resp.Topics {
		if meta.Name != topic {
			continue
		}

		if meta.Error != nil {
			return fmt.Errorf("kafka topic %s: %w", topic, meta.Error)
		}

		if len(meta.Partitions) == 0 {
			return fmt.Errorf("kafka topic %s has no partitions", topic)
		}

		return nil
	}

	return fmt.Errorf("kafka topic %s is missing", topic)
}
