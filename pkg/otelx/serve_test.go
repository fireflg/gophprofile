package otelx_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/pkg/logger"
	"github.com/fireflg/gophprofile/pkg/otelx"
)

func TestMetricsServerServesMetricsAndRegisteredRoutes(t *testing.T) {
	cfg := config.Metrics{Enabled: true, Host: "127.0.0.1", Port: freePort(t), Path: "/metrics"}

	server, err := otelx.NewMetricsServer(t.Context(), cfg, prometheus.NewRegistry())
	require.NoError(t, err)

	server.Handle("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	go server.Serve(logger.Nop())

	t.Cleanup(func() { require.NoError(t, server.Shutdown(context.Background())) })

	require.Equal(t, http.StatusOK, statusOf(t, cfg.Addr(), cfg.Path))
	require.Equal(t, http.StatusTeapot, statusOf(t, cfg.Addr(), "/health"))
}

func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
	require.NoError(t, listener.Close())

	return addr.Port
}

func statusOf(t *testing.T, addr, path string) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+path, nil)
	require.NoError(t, err)

	var resp *http.Response

	require.Eventually(t, func() bool {
		resp, err = http.DefaultClient.Do(req) //nolint:bodyclose // тело закрывается ниже

		return err == nil
	}, time.Second, 10*time.Millisecond)

	defer func() { require.NoError(t, resp.Body.Close()) }()

	return resp.StatusCode
}

func TestNewMetricsServerDisabled(t *testing.T) {
	server, err := otelx.NewMetricsServer(t.Context(), config.Metrics{}, prometheus.NewRegistry())
	require.NoError(t, err)
	require.Nil(t, server)
}

func TestNewMetricsServerRejectsInvalidConfig(t *testing.T) {
	tests := map[string]struct {
		cfg  config.Metrics
		want string
	}{
		"нулевой порт": {
			cfg:  config.Metrics{Enabled: true, Path: "/metrics"},
			want: "port must be in range",
		},
		"порт за границей диапазона": {
			cfg:  config.Metrics{Enabled: true, Port: 70000, Path: "/metrics"},
			want: "port must be in range",
		},
		"путь без слеша": {
			cfg:  config.Metrics{Enabled: true, Port: 9090, Path: "metrics"},
			want: "path must start with /",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := otelx.NewMetricsServer(t.Context(), tc.cfg, prometheus.NewRegistry())
			require.ErrorContains(t, err, tc.want)
		})
	}
}
