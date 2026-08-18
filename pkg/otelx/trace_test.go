package otelx_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/pkg/otelx"
)

func TestNewTracerProviderRejectsSampleRatioOutOfRange(t *testing.T) {
	for _, ratio := range []float64{-0.1, 1.5} {
		_, err := otelx.NewTracerProvider(t.Context(), config.OTel{SampleRatio: ratio}, nil)
		require.ErrorContains(t, err, "sample_ratio must be within [0, 1]")
	}
}
