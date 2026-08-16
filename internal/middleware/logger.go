package middleware

import (
	"net/http"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger пишет в zap строку доступа по каждому запросу.
func Logger(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if log == nil {
				next.ServeHTTP(w, r)

				return
			}

			start := time.Now()
			rw := wrapWriter(w)

			next.ServeHTTP(rw, r)

			fields := []zap.Field{
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", rw.Status()),
				zap.Int("bytes", rw.bytes),
				zap.Duration("duration", time.Since(start)),
				zap.String("remote_addr", r.RemoteAddr),
			}

			if id := RequestIDFrom(r.Context()); id != "" {
				fields = append(fields, zap.String("request_id", id))
			}

			log.Check(levelFor(rw.Status()), "request").Write(fields...)
		})
	}
}

func levelFor(status int) zapcore.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return zapcore.ErrorLevel
	case status >= http.StatusBadRequest:
		return zapcore.WarnLevel
	default:
		return zapcore.InfoLevel
	}
}
