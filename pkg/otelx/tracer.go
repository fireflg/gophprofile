// Package otelx собирает трубы телеметрии OpenTelemetry: трейсы и логи.
package otelx

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// scopeBase - префикс имени instrumentation scope.
const scopeBase = "github.com/fireflg/gophprofile/"

// Tracer возвращает трейсер конкретного модуля, например otelx.Tracer("internal/worker").
func Tracer(module string) trace.Tracer {
	return otel.Tracer(scopeBase + module)
}

// TraceIDFrom возвращает идентификатор трейса из контекста; "" - если спана нет.
func TraceIDFrom(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}

	return sc.TraceID().String()
}

// SpanIDFrom возвращает идентификатор текущего спана из контекста; "" - если спана нет.
func SpanIDFrom(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasSpanID() {
		return ""
	}

	return sc.SpanID().String()
}
