package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/fireflg/gophprofile/pkg/logger"
)

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
	log    *zap.Logger
	checks []Check
}

// NewHealthHandler создаёт обработчик с набором проверок.
func NewHealthHandler(log *zap.Logger, checks ...Check) *HealthHandler {
	return &HealthHandler{log: logger.Component(log, "health_handler"), checks: checks}
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

			status := ComponentStatus{Status: statusOK}
			if err := check.Probe(ctx); err != nil {
				status = ComponentStatus{Status: statusFail}

				if h.log != nil {
					h.log.Error("health check failed",
						zap.String("check", check.Name), zap.Error(err))
				}
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

	if err := WriteJSON(w, code, response); err != nil && h.log != nil {
		h.log.Error("write health response", zap.Error(err))
	}
}
