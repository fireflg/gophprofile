package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/fireflg/gophprofile/pkg/ctxmeta"
)

// Trace дополняет спан, открытый otelhttp, данными маршрутизации.
func Trace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		span := trace.SpanFromContext(ctx)

		if id := RequestIDFrom(ctx); id != "" {
			span.SetAttributes(attribute.String("request_id", id))
		}

		if id := ctxmeta.UserIDFrom(ctx); id != "" {
			span.SetAttributes(attribute.String("user_id", id))
		}

		next.ServeHTTP(w, r)

		if !span.IsRecording() {
			return
		}

		routeCtx := chi.RouteContext(ctx)
		if routeCtx == nil {
			return
		}

		if pattern := routeCtx.RoutePattern(); pattern != "" {
			span.SetName(r.Method + " " + pattern)
			span.SetAttributes(attribute.String("http.route", pattern))
		}

		if ctxmeta.UserIDFrom(ctx) == "" {
			if id := routeCtx.URLParam("user_id"); id != "" {
				span.SetAttributes(attribute.String("user_id", id))
			}
		}
	})
}
