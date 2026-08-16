package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/fireflg/gophprofile/internal/handlers"
)

func okProbe(context.Context) error { return nil }

func failingProbe(message string) func(context.Context) error {
	return func(context.Context) error { return errors.New(message) }
}

func TestHealthAllComponentsUp(t *testing.T) {
	handler := handlers.NewHealthHandler(zap.NewNop(),
		handlers.Check{Name: "postgres", Probe: okProbe},
		handlers.Check{Name: "s3", Probe: okProbe},
		handlers.Check{Name: "broker", Probe: okProbe},
	)

	rec := httptest.NewRecorder()
	handler.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusOK, rec.Code)

	body := decode[handlers.HealthResponse](t, rec)
	require.Equal(t, "ok", body.Status)
	require.Len(t, body.Components, 3)
	require.False(t, body.CheckedAt.IsZero())
}

func TestHealthReturns503WhenComponentDown(t *testing.T) {
	handler := handlers.NewHealthHandler(zap.NewNop(),
		handlers.Check{Name: "postgres", Probe: okProbe},
		handlers.Check{Name: "s3", Probe: failingProbe("bucket недоступен")},
	)

	rec := httptest.NewRecorder()
	handler.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	body := decode[handlers.HealthResponse](t, rec)
	require.Equal(t, "fail", body.Status)
	require.Equal(t, "ok", body.Components["postgres"].Status)
	require.Equal(t, "fail", body.Components["s3"].Status)
	require.Equal(t, "bucket недоступен", body.Components["s3"].Error)
}

func TestHealthWithoutChecks(t *testing.T) {
	handler := handlers.NewHealthHandler(zap.NewNop())

	rec := httptest.NewRecorder()
	handler.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", decode[handlers.HealthResponse](t, rec).Status)
}

// Проверки идут параллельно, поэтому медленный компонент ограничен общим таймаутом контекста.
func TestHealthProbeRespectsContext(t *testing.T) {
	handler := handlers.NewHealthHandler(zap.NewNop(),
		handlers.Check{Name: "slow", Probe: func(ctx context.Context) error {
			_, hasDeadline := ctx.Deadline()
			require.True(t, hasDeadline)

			return nil
		}},
	)

	rec := httptest.NewRecorder()
	handler.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusOK, rec.Code)
}
