package metrics_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/metrics"
)

func TestCachedQueriesSourceOnceWithinTTL(t *testing.T) {
	var calls atomic.Int64

	value := metrics.Cached(func(context.Context) (int64, error) {
		return calls.Add(1) * 100, nil
	}, time.Hour)

	for range 5 {
		got, err := value(t.Context())
		require.NoError(t, err)
		require.Equal(t, int64(100), got)
	}

	require.Equal(t, int64(1), calls.Load())
}

func TestCachedRefreshesWhenValueExpired(t *testing.T) {
	var calls atomic.Int64

	value := metrics.Cached(func(context.Context) (int64, error) {
		return calls.Add(1) * 100, nil
	}, 0)

	first, err := value(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(100), first)

	second, err := value(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(200), second)
}

func TestCachedKeepsFailureUntilTTL(t *testing.T) {
	var calls atomic.Int64

	wantErr := errors.New("база недоступна")
	value := metrics.Cached(func(context.Context) (int64, error) {
		calls.Add(1)

		return 0, wantErr
	}, time.Hour)

	for range 5 {
		_, err := value(t.Context())
		require.ErrorIs(t, err, wantErr)
	}

	require.Equal(t, int64(1), calls.Load())
}

func TestCachedIsSafeForConcurrentUse(t *testing.T) {
	var calls atomic.Int64

	value := metrics.Cached(func(context.Context) (int64, error) {
		return calls.Add(1) * 100, nil
	}, time.Hour)

	var wg sync.WaitGroup

	results := make([]int64, 16)
	failures := make([]error, 16)

	for i := range results {
		wg.Add(1)

		go func() {
			defer wg.Done()

			results[i], failures[i] = value(t.Context())
		}()
	}

	wg.Wait()

	for i := range results {
		require.NoError(t, failures[i])
		require.Equal(t, int64(100), results[i])
	}

	require.Equal(t, int64(1), calls.Load())
}
