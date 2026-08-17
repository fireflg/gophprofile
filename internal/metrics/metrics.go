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
	statusSuccess = "success"
	statusError   = "error"
)

// scopeName - instrumentation scope; попадает в otel_scope_name при экспорте.
const scopeName = "github.com/fireflg/gophprofile/internal/metrics"

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
)

// ObserveUpload записывает исход и длительность загрузки аватарки.
func ObserveUpload(ctx context.Context, started time.Time, err error) {
	attrs := metric.WithAttributes(attribute.String("status", status(err)))

	uploadsTotal.Add(ctx, 1, attrs)
	uploadDuration.Record(ctx, time.Since(started).Seconds(), attrs)
}

// ObserveProcessing записывает исход и длительность нарезки миниатюр.
func ObserveProcessing(ctx context.Context, started time.Time, err error) {
	attrs := metric.WithAttributes(attribute.String("status", status(err)))

	thumbnailsTotal.Add(ctx, 1, attrs)
	processingDuration.Record(ctx, time.Since(started).Seconds(), attrs)
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
	_, err := meter.Int64ObservableGauge("avatars_storage",
		metric.WithDescription("Total storage used by avatars"),
		metric.WithUnit("By"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			value, err := observe(ctx)
			if err != nil {
				return err
			}

			o.Observe(value)

			return nil
		}),
	)

	return err
}

func status(err error) string {
	if err != nil {
		return statusError
	}

	return statusSuccess
}

func must[T any](instrument T, err error) T {
	if err != nil {
		panic(err)
	}

	return instrument
}
