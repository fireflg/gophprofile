package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireflg/gophprofile/internal/metrics"
)

// routeUnmatched - лейбл для запросов, не попавших ни в один маршрут.
const routeUnmatched = "unmatched"

// Metrics считает запросы, их длительность и ответы с кодом 4xx и 5xx.
func Metrics(skipPaths ...string) func(http.Handler) http.Handler {
	skip := make(map[string]struct{}, len(skipPaths))
	for _, path := range skipPaths {
		skip[path] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, skipped := skip[r.URL.Path]; skipped {
				next.ServeHTTP(w, r)

				return
			}

			start := time.Now()
			rw := wrapWriter(w)

			next.ServeHTTP(rw, r)

			metrics.ObserveRequest(r.Context(), r.Method, routeOf(r), rw.Status(), start)
		})
	}
}

// routeOf возвращает шаблон маршрута; chi заполняет его только после обработки.
func routeOf(r *http.Request) string {
	routeCtx := chi.RouteContext(r.Context())
	if routeCtx == nil {
		return routeUnmatched
	}

	if pattern := routeCtx.RoutePattern(); pattern != "" {
		return pattern
	}

	return routeUnmatched
}
