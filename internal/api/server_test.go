package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/api"
	"github.com/fireflg/gophprofile/internal/config"
)

func TestNewServerRejectsInvalidPort(t *testing.T) {
	for name, port := range map[string]int{"ноль": 0, "отрицательный": -1, "вне диапазона": 70000} {
		t.Run(name, func(t *testing.T) {
			_, err := api.NewServer(config.HTTP{Host: "127.0.0.1", Port: port}, http.NotFoundHandler())
			require.ErrorContains(t, err, "port must be in range")
		})
	}
}

func TestNewServerAppliesTimeouts(t *testing.T) {
	server, err := api.NewServer(config.HTTP{
		Host:         "127.0.0.1",
		Port:         8080,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 7 * time.Second,
	}, http.NotFoundHandler())
	require.NoError(t, err)

	require.Equal(t, "127.0.0.1:8080", server.Addr)
	require.Equal(t, 5*time.Second, server.ReadTimeout)
	require.Equal(t, 5*time.Second, server.ReadHeaderTimeout)
	require.Equal(t, 7*time.Second, server.WriteTimeout)
}
