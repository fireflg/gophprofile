package repository_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/repository"
)

func TestNewPoolRequiresDSN(t *testing.T) {
	_, err := repository.NewPool(t.Context(), config.Postgres{})
	require.ErrorContains(t, err, "dsn is required")
}
