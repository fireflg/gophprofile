package otelx

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/fireflg/gophprofile/internal/config"
)

// NewLoggerProvider собирает пайп логов: экспортёр -> процессор -> провайдер.
func NewLoggerProvider(
	ctx context.Context,
	cfg config.OTel,
	res *resource.Resource,
) (*sdklog.LoggerProvider, error) {
	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}

	exporter, err := otlploggrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otel: log exporter: %w", err)
	}

	return sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	), nil
}
