package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/middleware"
	"github.com/fireflg/gophprofile/pkg/logger"
)

func TestRecoverReturns500OnPanic(t *testing.T) {
	handler := middleware.Recover(logger.Nop())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/avatars", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"error":"Internal server error"}`, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "boom")
}

func TestRecoverRepanicsAbortHandler(t *testing.T) {
	handler := middleware.Recover(logger.Nop())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	require.PanicsWithError(t, http.ErrAbortHandler.Error(), func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})
}

func TestRecoverKeepsStatusWhenResponseStarted(t *testing.T) {
	handler := middleware.Logger(logger.Nop())(middleware.Recover(logger.Nop())(http.HandlerFunc(
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
