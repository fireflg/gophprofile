package middleware

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/fireflg/gophprofile/internal/metrics"
	"github.com/fireflg/gophprofile/pkg/logger"
)

// panicResponse - тело ответа при панике; формат совпадает с ошибками API.
const panicResponse = `{"error":"Internal server error"}`

// Recover перехватывает панику обработчика.
// Сделал отдельную мидлвару, потому что chi recover шлет мимо обработчика логов в stdout.
func Recover(log *zap.Logger) func(http.Handler) http.Handler {
	if log != nil {
		log = logger.Component(log, "recover")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				cause := recover()
				if cause == nil {
					return
				}

				if err, ok := cause.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(cause)
				}

				handlePanic(w, r, cause, log)
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// handlePanic записывает панику в лог, спан и метрику, после чего отвечает 500.
func handlePanic(w http.ResponseWriter, r *http.Request, cause any, log *zap.Logger) {
	ctx := r.Context()
	err := fmt.Errorf("panic: %v", cause)

	span := trace.SpanFromContext(ctx)
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, err.Error())

	metrics.ObservePanic(ctx, routeOf(r))

	if log != nil {
		logger.WithContext(ctx, log).Error("panic recovered",
			zap.Error(err),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.ByteString("stack", debug.Stack()))
	}

	writePanicResponse(w)
}

// writePanicResponse отвечает 500, если обработчик не успел отправить заголовки.
func writePanicResponse(w http.ResponseWriter) {
	if written, ok := w.(interface{ Written() bool }); ok && written.Written() {
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)

	_, _ = io.WriteString(w, panicResponse)
}
