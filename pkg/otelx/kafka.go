package otelx

import (
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/propagation"
)

// HeaderCarrier переносит контекст трассировки через заголовки Kafka.
type HeaderCarrier struct {
	Headers *[]kafka.Header
}

var _ propagation.TextMapCarrier = (*HeaderCarrier)(nil)

// Get возвращает значение заголовка.
func (c *HeaderCarrier) Get(key string) string {
	if c.Headers == nil {
		return ""
	}

	for _, header := range *c.Headers {
		if header.Key == key {
			return string(header.Value)
		}
	}

	return ""
}

// Set проставляет заголовок, заменяя существующий с тем же ключом.
func (c *HeaderCarrier) Set(key, value string) {
	if c.Headers == nil {
		return
	}

	for i, header := range *c.Headers {
		if header.Key == key {
			(*c.Headers)[i].Value = []byte(value)

			return
		}
	}

	*c.Headers = append(*c.Headers, kafka.Header{Key: key, Value: []byte(value)})
}

// Keys перечисляет имена заголовков.
func (c *HeaderCarrier) Keys() []string {
	if c.Headers == nil {
		return nil
	}

	keys := make([]string, 0, len(*c.Headers))
	for _, header := range *c.Headers {
		keys = append(keys, header.Key)
	}

	return keys
}
