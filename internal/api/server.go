package api

import (
	"fmt"
	"net/http"

	"github.com/fireflg/gophprofile/internal/config"
)

// maxPort - верхняя граница диапазона TCP-портов.
const maxPort = 65535

// NewServer собирает HTTP-сервер по настройкам транспорта.
func NewServer(cfg config.HTTP, handler http.Handler) (*http.Server, error) {
	if cfg.Port <= 0 || cfg.Port > maxPort {
		return nil, fmt.Errorf("http: port must be in range 1-%d, got %d", maxPort, cfg.Port)
	}

	return &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
	}, nil
}
