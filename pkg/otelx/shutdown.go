package otelx

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// shutdownTimeout ограничивает досылку накопленной телеметрии при остановке.
const shutdownTimeout = 5 * time.Second

// Shutdown гасит все трубы, давая процессорам дослать накопленное.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	var errs []error

	if err := t.Metrics.Shutdown(ctx); err != nil {
		errs = append(errs, err)
	}

	if t.Meters != nil {
		if err := t.Meters.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("otel: shutdown meters: %w", err))
		}
	}

	if t.Traces != nil {
		if err := t.Traces.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("otel: shutdown traces: %w", err))
		}
	}

	if t.Logs != nil {
		if err := t.Logs.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("otel: shutdown logs: %w", err))
		}
	}

	return errors.Join(errs...)
}
