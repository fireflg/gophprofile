package logger_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/fireflg/gophprofile/pkg/logger"
)

func TestComponentAddsField(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)

	log := logger.Component(zap.New(core), "avatar_service")
	log.Info("ready")

	require.Equal(t, 1, logs.Len())
	require.Equal(t, "avatar_service", logs.All()[0].ContextMap()["component"])
}

func TestComponentKeepsParentFields(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)

	log := logger.Component(zap.New(core).With(zap.String("env", "test")), "consumer")
	log.Info("ready")

	fields := logs.All()[0].ContextMap()
	require.Equal(t, "test", fields["env"])
	require.Equal(t, "consumer", fields["component"])
}

func TestComponentAcceptsNil(t *testing.T) {
	log := logger.Component(nil, "consumer")

	require.NotNil(t, log)
	log.Info("ready")
}
