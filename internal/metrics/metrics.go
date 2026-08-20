// Package metrics объявляет метрики сервиса и точки их записи.
package metrics

import (
	"context"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Значения лейбла status у прикладных метрик.
const (
	StatusSuccess     = "success"
	StatusClientError = "client_error"
	StatusSkipped     = "skipped"
	StatusError       = "error"
	StatusMalformed   = "malformed"
)

// scopeName - instrumentation scope; попадает в otel_scope_name при экспорте.
const scopeName = "github.com/fireflg/gophprofile/internal/metrics"

// storageRefreshInterval - как часто пересчитывается суммарный объём хранилища.
const storageRefreshInterval = time.Minute

// storageQueryTimeout - предел на один поход в БД за объёмом хранилища.
const storageQueryTimeout = 5 * time.Second

var meter = otel.Meter(scopeName)

var (
	uploadsTotal = must(meter.Int64Counter("avatars_uploads",
		metric.WithDescription("Total number of avatar uploads")))

	uploadDuration = must(meter.Float64Histogram("avatars_upload_duration",
		metric.WithDescription("Avatar upload duration"),
		metric.WithUnit("s")))

	thumbnailsTotal = must(meter.Int64Counter("avatars_thumbnails",
		metric.WithDescription("Total number of processed avatars")))

	processingDuration = must(meter.Float64Histogram("avatars_processing_duration",
		metric.WithDescription("Avatar processing duration"),
		metric.WithUnit("s")))

	httpRequests = must(meter.Int64Counter("avatars_http_requests",
		metric.WithDescription("Total number of HTTP requests")))

	httpDuration = must(meter.Float64Histogram("avatars_http_request_duration",
		metric.WithDescription("HTTP request duration"),
		metric.WithUnit("s")))

	httpErrors = must(meter.Int64Counter("avatars_http_errors",
		metric.WithDescription("Total number of HTTP responses with status 4xx or 5xx")))

	httpPanics = must(meter.Int64Counter("avatars_http_panics",
		metric.WithDescription("Total number of panics recovered in HTTP handlers")))

	consumerMessages = must(meter.Int64Counter("avatars_consumer_messages",
		metric.WithDescription("Total number of consumed Kafka messages")))

	dependencyUp = must(meter.Int64Gauge("avatars_dependency_up",
		metric.WithDescription("Dependency state seen by the health check: 1 - ok, 0 - fail")))
)

// ObserveUpload записывает исход и длительность загрузки аватарки.
func ObserveUpload(ctx context.Context, started time.Time, status string) {
	attrs := metric.WithAttributes(attribute.String("status", status))

	uploadsTotal.Add(ctx, 1, attrs)
	uploadDuration.Record(ctx, time.Since(started).Seconds(), attrs)
}

// ObserveProcessing записывает исход и длительность обработки события воркером.
func ObserveProcessing(ctx context.Context, started time.Time, eventType, status string) {
	attrs := metric.WithAttributes(
		attribute.String("event_type", eventType),
		attribute.String("status", status),
	)

	thumbnailsTotal.Add(ctx, 1, attrs)

	if status == StatusSkipped {
		return
	}

	processingDuration.Record(ctx, time.Since(started).Seconds(), attrs)
}

// ObserveConsumedMessage считает сообщение, прочитанное из брокера.
func ObserveConsumedMessage(ctx context.Context, status string) {
	consumerMessages.Add(ctx, 1, metric.WithAttributes(attribute.String("status", status)))
}

// ObserveDependency публикует состояние внешней зависимости по данным хелсчека.
func ObserveDependency(ctx context.Context, component string, up bool) {
	var value int64
	if up {
		value = 1
	}

	dependencyUp.Record(ctx, value, metric.WithAttributes(attribute.String("component", component)))
}

// ObservePanic считает панику, перехваченную HTTP-прослойкой.
func ObservePanic(ctx context.Context, route string) {
	httpPanics.Add(ctx, 1, metric.WithAttributes(attribute.String("route", route)))
}

// ObserveRequest записывает обслуженный HTTP-запрос.
func ObserveRequest(ctx context.Context, method, route string, code int, started time.Time) {
	statusCode := strconv.Itoa(code)

	httpRequests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("method", method),
		attribute.String("route", route),
		attribute.String("status", statusCode),
	))

	httpDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(
		attribute.String("method", method),
		attribute.String("route", route),
	))

	if code < 400 {
		return
	}

	httpErrors.Add(ctx, 1, metric.WithAttributes(
		attribute.String("method", method),
		attribute.String("route", route),
		attribute.String("status", statusCode),
	))
}

// RegisterStorageGauge подписывает асинхронный gauge на суммарный объём хранилища.
func RegisterStorageGauge(observe func(context.Context) (int64, error)) error {
	cached := Cached(observe, storageRefreshInterval)

	_, err := meter.Int64ObservableGauge("avatars_storage",
		metric.WithDescription("Total storage used by avatars"),
		metric.WithUnit("By"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			ctx, cancel := context.WithTimeout(ctx, storageQueryTimeout)
			defer cancel()

			value, err := cached(ctx)
			if err != nil {
				return err
			}

			o.Observe(value)

			return nil
		}),
	)

	return err
}

// RegisterConsumerLagGauge регистрирует асинхронный gauge на отставание консьюмера.
func RegisterConsumerLagGauge(observe func() int64) error {
	_, err := meter.Int64ObservableGauge("avatars_consumer_lag",
		metric.WithDescription("Consumer group lag in messages"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(observe())

			return nil
		}),
	)

	return err
}

// Status - исход операции по ошибке: success либо error.
func Status(err error) string {
	if err != nil {
		return StatusError
	}

	return StatusSuccess
}

func must[T any](instrument T, err error) T {
	if err != nil {
		panic(err)
	}

	return instrument
}
