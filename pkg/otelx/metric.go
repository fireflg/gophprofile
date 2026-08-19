package otelx

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/otlptranslator"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// durationBuckets - границы гистограмм длительности в секундах.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// NewMeterProvider поднимает провайдер метрик с Prometheus-ридером.
func NewMeterProvider(res *resource.Resource) (*sdkmetric.MeterProvider, *prometheus.Registry, error) {
	registry := prometheus.NewRegistry()

	exporter, err := otelprom.New(
		otelprom.WithRegisterer(registry),
		otelprom.WithTranslationStrategy(otlptranslator.UnderscoreEscapingWithSuffixes),
		otelprom.WithoutScopeInfo(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("otel: create prometheus exporter: %w", err)
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(exporter),
		sdkmetric.WithView(sdkmetric.NewView(
			sdkmetric.Instrument{Name: "*", Kind: sdkmetric.InstrumentKindHistogram, Unit: "s"},
			sdkmetric.Stream{
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: durationBuckets},
			},
		)),
	)

	otel.SetMeterProvider(provider)

	if err := runtime.Start(runtime.WithMeterProvider(provider)); err != nil {
		return nil, nil, fmt.Errorf("otel: start runtime metrics: %w", err)
	}

	return provider, registry, nil
}
