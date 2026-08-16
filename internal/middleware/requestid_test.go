package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/middleware"
)

func TestRequestIDGeneratesWhenHeaderMissing(t *testing.T) {
	var fromContext string

	handler := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		fromContext = middleware.RequestIDFrom(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.NotEmpty(t, fromContext)
	require.Equal(t, fromContext, rec.Header().Get(middleware.HeaderRequestID))
}

func TestRequestIDKeepsIncomingHeader(t *testing.T) {
	const incoming = "trace-42"

	var fromContext string

	handler := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		fromContext = middleware.RequestIDFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(middleware.HeaderRequestID, incoming)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, incoming, fromContext)
	require.Equal(t, incoming, rec.Header().Get(middleware.HeaderRequestID))
}

func TestRequestIDIsUniquePerRequest(t *testing.T) {
	seen := make(map[string]struct{}, 10)

	handler := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen[middleware.RequestIDFrom(r.Context())] = struct{}{}
	}))

	for range 10 {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}

	require.Len(t, seen, 10)
}

func TestRequestIDFromEmptyContext(t *testing.T) {
	require.Empty(t, middleware.RequestIDFrom(t.Context()))
}
