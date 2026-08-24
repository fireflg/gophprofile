package otelx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/fireflg/gophprofile/internal/config"
)

// metricsReadHeaderTimeout - защита от медленных заголовков на служебном порту.
const metricsReadHeaderTimeout = 5 * time.Second

// maxPort - верхняя граница диапазона TCP-портов.
const maxPort = 65535

// MetricsServer отдаёт метрики в формате Prometheus на отдельном порту.
type MetricsServer struct {
	server   *http.Server
	router   chi.Router
	listener net.Listener
	addr     string
	path     string
}

// NewMetricsServer готовит сервер метрик; nil - если метрики выключены.
func NewMetricsServer(ctx context.Context, cfg config.Metrics, registry *prometheus.Registry) (*MetricsServer, error) {
	if !cfg.Enabled || registry == nil {
		return nil, nil
	}

	if cfg.Port <= 0 || cfg.Port > maxPort {
		return nil, fmt.Errorf("metrics: port must be in range 1-%d, got %d", maxPort, cfg.Port)
	}

	if !strings.HasPrefix(cfg.Path, "/") {
		return nil, fmt.Errorf("metrics: path must start with /, got %q", cfg.Path)
	}

	router := chi.NewRouter()
	router.Handle(cfg.Path, promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry}))

	var lc net.ListenConfig

	listener, err := lc.Listen(ctx, "tcp", cfg.Addr())
	if err != nil {
		return nil, fmt.Errorf("otel: listen metrics: %w", err)
	}

	return &MetricsServer{
		server:   &http.Server{Handler: router, ReadHeaderTimeout: metricsReadHeaderTimeout},
		router:   router,
		listener: listener,
		addr:     cfg.Addr(),
		path:     cfg.Path,
	}, nil
}

// Handle публикует дополнительный маршрут на служебном порту; вызывается до Serve.
func (s *MetricsServer) Handle(pattern string, handler http.HandlerFunc) {
	if s == nil {
		return
	}

	s.router.Get(pattern, handler)
}

// Serve обслуживает скрейп до вызова Shutdown; вызывается в отдельной горутине.
func (s *MetricsServer) Serve(log *slog.Logger) {
	if s == nil {
		return
	}

	log.Info("metrics server started", slog.String("addr", s.addr), slog.String("path", s.path))

	if err := s.server.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("metrics server stopped", slog.Any("error", err))
	}
}

// Shutdown закрывает сервер метрик.
func (s *MetricsServer) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("otel: shutdown metrics server: %w", err)
	}

	return nil
}
