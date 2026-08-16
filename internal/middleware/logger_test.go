package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/fireflg/gophprofile/internal/middleware"
)

// observedLogger — логгер, чьи записи можно проверить в тесте.
func observedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)

	return zap.New(core), logs
}

func TestLoggerWritesAccessEntry(t *testing.T) {
	log, logs := observedLogger()

	handler := middleware.Logger(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/avatars", nil))

	require.Equal(t, 1, logs.Len())

	entry := logs.All()[0]
	require.Equal(t, "request", entry.Message)
	require.Equal(t, zapcore.InfoLevel, entry.Level)

	fields := entry.ContextMap()
	require.Equal(t, http.MethodGet, fields["method"])
	require.Equal(t, "/api/v1/avatars", fields["path"])
	require.EqualValues(t, http.StatusOK, fields["status"])
	require.EqualValues(t, len("hello"), fields["bytes"])
}

func TestLoggerLevelDependsOnStatus(t *testing.T) {
	tests := map[string]struct {
		status int
		level  zapcore.Level
	}{
		"успех":          {status: http.StatusOK, level: zapcore.InfoLevel},
		"ошибка клиента": {status: http.StatusNotFound, level: zapcore.WarnLevel},
		"ошибка сервера": {status: http.StatusInternalServerError, level: zapcore.ErrorLevel},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			log, logs := observedLogger()

			handler := middleware.Logger(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

			require.Equal(t, 1, logs.Len())
			require.Equal(t, tc.level, logs.All()[0].Level)
			require.EqualValues(t, tc.status, logs.All()[0].ContextMap()["status"])
		})
	}
}

func TestLoggerAddsRequestID(t *testing.T) {
	log, logs := observedLogger()

	handler := middleware.RequestID(middleware.Logger(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(middleware.HeaderRequestID, "trace-1")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, "trace-1", logs.All()[0].ContextMap()["request_id"])
}

func TestLoggerWithoutLoggerPassesThrough(t *testing.T) {
	called := false

	handler := middleware.Logger(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	require.True(t, called)
}
