package otelx_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/pkg/otelx"
)

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
