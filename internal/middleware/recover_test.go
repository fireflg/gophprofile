package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/fireflg/gophprofile/internal/middleware"
)

func TestRecoverReturns500OnPanic(t *testing.T) {
	log, logs := observedLogger()

	handler := middleware.Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/avatars", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"error":"Internal server error"}`, rec.Body.String())

	require.Equal(t, 1, logs.Len())

	entry := logs.All()[0]
	require.Equal(t, "panic recovered", entry.Message)
	require.Equal(t, zapcore.ErrorLevel, entry.Level)

	fields := entry.ContextMap()
	require.Equal(t, "recover", fields["component"])
	require.Equal(t, http.MethodGet, fields["method"])
	require.Equal(t, "/api/v1/avatars", fields["path"])
	require.Contains(t, fields["error"], "boom")
	require.NotEmpty(t, fields["stack"])
}

func TestRecoverKeepsAccessLogEntry(t *testing.T) {
	log, logs := observedLogger()

	handler := middleware.Logger(log)(middleware.Recover(log)(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) {
			panic("boom")
		})))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/avatars", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Len(t, logs.FilterMessage("panic recovered").All(), 1)

	access := logs.FilterMessage("request").All()
	require.Len(t, access, 1)
	require.EqualValues(t, http.StatusInternalServerError, access[0].ContextMap()["status"])
}

func TestRecoverRepanicsAbortHandler(t *testing.T) {
	log, logs := observedLogger()

	handler := middleware.Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	require.PanicsWithError(t, http.ErrAbortHandler.Error(), func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})

	require.Zero(t, logs.Len())
}

func TestRecoverKeepsStatusWhenResponseStarted(t *testing.T) {
	log, _ := observedLogger()

	handler := middleware.Logger(log)(middleware.Recover(log)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("partial"))

			panic("boom")
		})))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "partial", rec.Body.String())
}

func TestRecoverWithoutLoggerStillAnswers(t *testing.T) {
	handler := middleware.Recover(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}
