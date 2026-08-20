package metrics

import (
	"context"
	"sync"
	"time"
)

// Cached нужен для кэширования значений метрик, чтобы не грузить бд
func Cached(load func(context.Context) (int64, error), ttl time.Duration) func(context.Context) (int64, error) {
	cache := &valueCache{load: load, ttl: ttl}

	return cache.get
}

type valueCache struct {
	load func(context.Context) (int64, error)
	ttl  time.Duration

	mu        sync.Mutex
	value     int64
	err       error
	checkedAt time.Time
}

func (c *valueCache) get(ctx context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.checkedAt.IsZero() && time.Since(c.checkedAt) < c.ttl {
		return c.value, c.err
	}

	c.value, c.err = c.load(ctx)
	c.checkedAt = time.Now()

	return c.value, c.err
}
