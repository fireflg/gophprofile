package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/internal/handlers"
	"github.com/fireflg/gophprofile/internal/handlers/mocks"
	"github.com/fireflg/gophprofile/pkg/ctxmeta"
)

func newObservedRouter(t *testing.T) (http.Handler, *mocks.MockAvatarUseCase, *observer.ObservedLogs) {
	t.Helper()

	core, logs := observer.New(zapcore.ErrorLevel)

	service := mocks.NewMockAvatarUseCase(gomock.NewController(t))
	service.EXPECT().MaxFileSize().Return(int64(maxFileSize)).AnyTimes()
	service.EXPECT().AllowedMimeTypes().Return([]string{"image/png"}).AnyTimes()

	handler := handlers.NewAvatarHandler(service, zap.New(core))

	router := chi.NewRouter()
	router.Get("/api/v1/avatars/{id}", handler.Get)

	return router, service, logs
}

func TestServerErrorIsLoggedWithCorrelation(t *testing.T) {
	router, service, logs := newObservedRouter(t)

	id := uuid.New()
	service.EXPECT().GetFile(gomock.Any(), id, "", "").Return(nil, errors.New("база недоступна"))

	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)

	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+id.String(), nil)
	ctx := trace.ContextWithSpanContext(req.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctxmeta.WithUserID(ctx, "user-1")))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "база недоступна")
	require.Equal(t, 1, logs.Len())

	fields := logs.All()[0].ContextMap()
	require.Equal(t, "request failed", logs.All()[0].Message)
	require.Equal(t, traceID.String(), fields["trace_id"])
	require.Equal(t, "00f067aa0ba902b7", fields["span_id"])
	require.Equal(t, "user-1", fields["user_id"])
	require.Contains(t, fields["error"], "база недоступна")
}

func TestClientErrorIsNotLogged(t *testing.T) {
	router, service, logs := newObservedRouter(t)

	id := uuid.New()
	service.EXPECT().GetFile(gomock.Any(), id, "", "").Return(nil, domain.ErrAvatarNotFound)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+id.String(), nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Zero(t, logs.Len())
}
