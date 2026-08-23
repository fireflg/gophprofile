package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/fireflg/gophprofile/pkg/ctxmeta"
	"github.com/fireflg/gophprofile/pkg/logger"
)

func observedLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	return slog.New(logger.ContextHandler(handler)), buf
}

func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	var found []map[string]any

	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}

		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))

		found = append(found, record)
	}

	return found
}

func TestComponentAddsField(t *testing.T) {
	log, buf := observedLogger()

	logger.Component(log, "avatar_service").Info("ready")

	entries := records(t, buf)
	require.Len(t, entries, 1)
	require.Equal(t, "avatar_service", entries[0]["component"])
}

func TestComponentKeepsParentFields(t *testing.T) {
	log, buf := observedLogger()

	logger.Component(log.With(slog.String("env", "test")), "consumer").Info("ready")

	entry := records(t, buf)[0]
	require.Equal(t, "test", entry["env"])
	require.Equal(t, "consumer", entry["component"])
}

func TestComponentAcceptsNil(t *testing.T) {
	log := logger.Component(nil, "consumer")

	require.NotNil(t, log)
	log.Info("ready")
}

func TestContextHandlerAddsCorrelation(t *testing.T) {
	log, buf := observedLogger()

	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)

	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)

	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	log.ErrorContext(ctxmeta.WithUserID(ctx, "user-1"), "request failed")

	entry := records(t, buf)[0]
	require.Equal(t, traceID.String(), entry["trace_id"])
	require.Equal(t, spanID.String(), entry["span_id"])
	require.Equal(t, "user-1", entry["user_id"])
}

func TestContextHandlerWithoutContextValues(t *testing.T) {
	log, buf := observedLogger()

	log.Info("ready")

	entry := records(t, buf)[0]
	require.NotContains(t, entry, "trace_id")
	require.NotContains(t, entry, "span_id")
	require.NotContains(t, entry, "user_id")
}

func TestNewRespectsLevel(t *testing.T) {
	log := logger.New("production", "error", nil)

	require.False(t, log.Enabled(context.Background(), slog.LevelInfo))
	require.True(t, log.Enabled(context.Background(), slog.LevelError))
}

func TestNewFallsBackToInfoLevel(t *testing.T) {
	log := logger.New("development", "unknown", nil)

	require.False(t, log.Enabled(context.Background(), slog.LevelDebug))
	require.True(t, log.Enabled(context.Background(), slog.LevelInfo))
}
