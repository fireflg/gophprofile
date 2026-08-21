package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireflg/gophprofile/pkg/ctxmeta"
	"github.com/fireflg/gophprofile/pkg/logger"
)

// Logger пишет в slog строку доступа по каждому запросу.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	if log != nil {
		log = logger.Component(log, "http")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if log == nil {
				next.ServeHTTP(w, r)

				return
			}

			start := time.Now()
			rw := wrapWriter(w)

			next.ServeHTTP(rw, r)

			ctx := r.Context()

			level := levelFor(rw.Status())
			if !log.Enabled(ctx, level) {
				return
			}

			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.Status()),
				slog.Int("bytes", rw.bytes),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote_addr", r.RemoteAddr),
			}

			if id := RequestIDFrom(ctx); id != "" {
				attrs = append(attrs, slog.String("request_id", id))
			}

			if ctxmeta.UserIDFrom(ctx) == "" {
				if id := chi.URLParam(r, "user_id"); id != "" {
					attrs = append(attrs, slog.String("user_id", id))
				}
			}

			log.LogAttrs(ctx, level, "request", attrs...)
		})
	}
}

func levelFor(status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}
