// Package logger настраивает структурированный логгер приложения на zap.
package logger

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New создаёт логгер: JSON для production-окружений, консольный вывод для локальной разработки.
func New(env, level string) (*zap.Logger, error) {
	var cfg zap.Config
	if isDevelopment(env) {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}

	cfg.Level = zap.NewAtomicLevelAt(parseLevel(level))
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	log, err := cfg.Build()
	if err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}

	return log, nil
}

// Component возвращает дочерний логгер компонента.
func Component(log *zap.Logger, name string) *zap.Logger {
	if log == nil {
		return zap.NewNop()
	}

	return log.With(zap.String("component", name))
}

func isDevelopment(env string) bool {
	return strings.EqualFold(env, "development") || strings.EqualFold(env, "local")
}

func parseLevel(level string) zapcore.Level {
	parsed, err := zapcore.ParseLevel(strings.ToLower(strings.TrimSpace(level)))
	if err != nil {
		return zapcore.InfoLevel
	}

	return parsed
}
