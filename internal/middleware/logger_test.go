package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/middleware"
	"github.com/fireflg/gophprofile/pkg/logger"
)

func TestLoggerPassesRequestThrough(t *testing.T) {
	var (
		called bool
		method string
	)

	handler := middleware.Logger(logger.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		method = r.Method

		_, _ = w.Write([]byte("hello"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/avatars", nil))

	require.True(t, called)
	require.Equal(t, http.MethodGet, method)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "hello", rec.Body.String())
}

func TestLoggerKeepsResponseStatus(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError} {
		handler := middleware.Logger(logger.Nop())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Equal(t, status, rec.Code)
	}
}

func TestLoggerWithoutLoggerPassesThrough(t *testing.T) {
	called := false

	handler := middleware.Logger(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	require.True(t, called)
}
