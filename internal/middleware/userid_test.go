package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestUserIDFromEmptyContext(t *testing.T) {
	require.Empty(t, ctxmeta.UserIDFrom(t.Context()))
}

func TestUserIDDropsUnsafeHeader(t *testing.T) {
	tests := map[string]string{
		"перевод строки":  "user-1\nlevel=fatal",
		"пробел":          "user 1",
		"кавычка":         `user"1`,
		"слишком длинный": strings.Repeat("u", 129),
	}

	for name, header := range tests {
		t.Run(name, func(t *testing.T) {
			var fromContext string

			handler := middleware.UserID(headerUserID)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				fromContext = ctxmeta.UserIDFrom(r.Context())
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(headerUserID, header)

			handler.ServeHTTP(httptest.NewRecorder(), req)

			require.Empty(t, fromContext)
		})
	}
}

func TestWithUserIDIgnoresEmptyValue(t *testing.T) {
	ctx := ctxmeta.WithUserID(t.Context(), "")

	require.Empty(t, ctxmeta.UserIDFrom(ctx))
}
