package otelx

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// Version - версия сервиса, попадающая в атрибут service.version.
// Проставляется линковщиком: -ldflags "-X github.com/fireflg/gophprofile/pkg/otelx.Version=..."
var Version = "dev"

// NewResource собирает описание процесса: service.name, service.version и данные SDK.
func NewResource(ctx context.Context, serviceName string) (*resource.Resource, error) {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(Version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: build resource: %w", err)
	}

	return res, nil
}
