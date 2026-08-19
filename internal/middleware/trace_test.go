package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/fireflg/gophprofile/internal/middleware"
)

func requestWithTrace(t *testing.T) (*http.Request, string) {
	t.Helper()

	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)

	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)

	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars", nil)

	return req.WithContext(trace.ContextWithSpanContext(req.Context(), spanCtx)), traceID.String()
}

func TestTraceReturnsTraceIDInHeader(t *testing.T) {
	req, traceID := requestWithTrace(t)

	handler := middleware.Trace(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, traceID, rec.Header().Get(middleware.HeaderTraceID))
}

func TestTraceSetsHeaderBeforeHandlerWrites(t *testing.T) {
	req, traceID := requestWithTrace(t)

	handler := middleware.Trace(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, traceID, rec.Header().Get(middleware.HeaderTraceID))
}

func TestTraceWithoutSpanLeavesHeaderEmpty(t *testing.T) {
	handler := middleware.Trace(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Empty(t, rec.Header().Get(middleware.HeaderTraceID))
}

func TestLoggerAddsTraceCorrelation(t *testing.T) {
	log, logs := observedLogger()

	req, traceID := requestWithTrace(t)

	handler := middleware.Logger(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, 1, logs.Len())

	fields := logs.All()[0].ContextMap()
	require.Equal(t, traceID, fields["trace_id"])
	require.Equal(t, "00f067aa0ba902b7", fields["span_id"])
}
