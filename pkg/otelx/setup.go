package otelx

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/fireflg/gophprofile/internal/config"
)

// Telemetry держит поднятые провайдеры телеметрии.
type Telemetry struct {
	Traces  *sdktrace.TracerProvider
	Logs    *sdklog.LoggerProvider
	Meters  *sdkmetric.MeterProvider
	Metrics *MetricsServer
}

// Setup поднимает пайп телеметрии и регистрирует глобально.
func Setup(ctx context.Context, cfg config.OTel, metricsCfg config.Metrics) (*Telemetry, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !cfg.Active() && !metricsCfg.Enabled {
		return &Telemetry{}, nil
	}

	res, err := NewResource(ctx, cfg.ServiceName)
	if err != nil {
		return nil, err
	}

	tel := &Telemetry{}

	if metricsCfg.Enabled {
		meters, registry, meterErr := NewMeterProvider(res)
		if meterErr != nil {
			return nil, meterErr
		}
		tel.Meters = meters

		metricsServer, serverErr := NewMetricsServer(ctx, metricsCfg, registry)
		if serverErr != nil {
			_ = tel.Shutdown(ctx)

			return nil, serverErr
		}
		tel.Metrics = metricsServer
	}

	if !cfg.Active() {
		return tel, nil
	}

	traces, err := NewTracerProvider(ctx, cfg, res)
	if err != nil {
		_ = tel.Shutdown(ctx)

		return nil, err
	}
	tel.Traces = traces

	logs, err := NewLoggerProvider(ctx, cfg, res)
	if err != nil {
		_ = tel.Shutdown(ctx)

		return nil, err
	}
	tel.Logs = logs

	otel.SetTracerProvider(traces)
	global.SetLoggerProvider(logs)

	return tel, nil
}

// LoggerProvider возвращает провайдер логов для моста otelslog; nil - если телеметрия выключена.
func (t *Telemetry) LoggerProvider() log.LoggerProvider {
	if t == nil || t.Logs == nil {
		return nil
	}

	return t.Logs
}

// SetErrorHandler направляет внутренние ошибки.
func SetErrorHandler(logger *slog.Logger) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn("otel sdk error", slog.Any("error", err))
	}))
}
