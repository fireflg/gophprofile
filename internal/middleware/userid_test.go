package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/middleware"
	"github.com/fireflg/gophprofile/pkg/ctxmeta"
)

const headerUserID = "X-User-ID"

func TestUserIDPutsHeaderIntoContext(t *testing.T) {
	var fromContext string

	handler := middleware.UserID(headerUserID)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		fromContext = ctxmeta.UserIDFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", nil)
	req.Header.Set(headerUserID, "  user-1  ")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, "user-1", fromContext)
}

func TestUserIDWithoutHeaderLeavesContextEmpty(t *testing.T) {
	var fromContext string

	called := false

	handler := middleware.UserID(headerUserID)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		fromContext = ctxmeta.UserIDFrom(r.Context())
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	require.True(t, called)
	require.Empty(t, fromContext)
}

func TestLoggerAddsUserIDFromHeader(t *testing.T) {
	log, logs := observedLogger()

	handler := middleware.UserID(headerUserID)(middleware.Logger(log)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", nil)
	req.Header.Set(headerUserID, "user-1")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, 1, logs.Len())
	require.Equal(t, "user-1", logs.All()[0].ContextMap()["user_id"])
}

func TestLoggerAddsUserIDFromRouteParam(t *testing.T) {
	log, logs := observedLogger()

	router := chi.NewRouter()
	router.Use(middleware.UserID(headerUserID), middleware.Logger(log))
	router.Get("/api/v1/users/{user_id}/avatar", func(http.ResponseWriter, *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-7/avatar", nil)

	router.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, 1, logs.Len())
	require.Equal(t, "user-7", logs.All()[0].ContextMap()["user_id"])
}

func TestLoggerPrefersHeaderOverRouteParam(t *testing.T) {
	log, logs := observedLogger()

	router := chi.NewRouter()
	router.Use(middleware.UserID(headerUserID), middleware.Logger(log))
	router.Get("/api/v1/users/{user_id}/avatar", func(http.ResponseWriter, *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-7/avatar", nil)
	req.Header.Set(headerUserID, "user-1")

	router.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, 1, logs.Len())
	require.Equal(t, "user-1", logs.All()[0].ContextMap()["user_id"])
}

func TestUserIDFromEmptyContext(t *testing.T) {
	require.Empty(t, ctxmeta.UserIDFrom(t.Context()))
}

func TestWithUserIDIgnoresEmptyValue(t *testing.T) {
	ctx := ctxmeta.WithUserID(t.Context(), "")

	require.Empty(t, ctxmeta.UserIDFrom(ctx))
}
