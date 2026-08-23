package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/fireflg/gophprofile/internal/metrics"
	"github.com/fireflg/gophprofile/pkg/logger"
)

// HealthPath - маршрут для хелсчека
const HealthPath = "/health"

// checkTimeout - предел ожидания одной проверки компонента.
const checkTimeout = 3 * time.Second

// Check - проверка доступности компонента (БД, S3, брокер).
type Check struct {
	Name  string
	Probe func(ctx context.Context) error
}

// ComponentStatus - результат проверки одного компонента.
type ComponentStatus struct {
	Status string `json:"status"`
}

// HealthResponse - ответ GET /health.
type HealthResponse struct {
	Status     string                     `json:"status"`
	Components map[string]ComponentStatus `json:"components"`
	CheckedAt  time.Time                  `json:"checked_at"`
}

// Статусы компонентов и сервиса в целом.
const (
	statusOK   = "ok"
	statusFail = "fail"
)

// HealthHandler — обработчик проверки работоспособности.
type HealthHandler struct {
	log    *slog.Logger
	checks []Check

	mu    sync.Mutex
	alive map[string]bool
}

// NewHealthHandler создаёт обработчик с набором проверок.
func NewHealthHandler(log *slog.Logger, checks ...Check) *HealthHandler {
	return &HealthHandler{
		log:    logger.Component(log, "health_handler"),
		checks: checks,
		alive:  make(map[string]bool, len(checks)),
	}
}

// Health обрабатывает GET /health: 200, если живы все компоненты, иначе 503.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()

	var (
		mu         sync.Mutex
		wg         sync.WaitGroup
		components = make(map[string]ComponentStatus, len(h.checks))
	)

	for _, check := range h.checks {
		wg.Add(1)

		go func(check Check) {
			defer wg.Done()

			err := check.Probe(ctx)

			h.report(ctx, check.Name, err)

			status := ComponentStatus{Status: statusOK}
			if err != nil {
				status = ComponentStatus{Status: statusFail}
			}

			mu.Lock()
			components[check.Name] = status
			mu.Unlock()
		}(check)
	}

	wg.Wait()

	response := HealthResponse{
		Status:     statusOK,
		Components: components,
		CheckedAt:  time.Now().UTC(),
	}

	code := http.StatusOK

	for _, component := range components {
		if component.Status != statusOK {
			response.Status = statusFail
			code = http.StatusServiceUnavailable

			break
		}
	}

	if err := WriteJSON(w, code, response); err != nil {
		h.log.ErrorContext(ctx, "write health response", slog.Any("error", err))
	}
}

func (h *HealthHandler) report(ctx context.Context, name string, err error) {
	up := err == nil

	metrics.ObserveDependency(ctx, name, up)

	h.mu.Lock()
	previous, known := h.alive[name]
	h.alive[name] = up
	h.mu.Unlock()

	if known && previous == up {
		return
	}

	switch {
	case !up:
		h.log.ErrorContext(ctx, "dependency is down", slog.String("check", name), slog.Any("error", err))
	case known:
		h.log.InfoContext(ctx, "dependency is back", slog.String("check", name))
	}
}
