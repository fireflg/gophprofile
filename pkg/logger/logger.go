// Package logger настраивает структурированный логгер приложения на zap.
package logger

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/fireflg/gophprofile/pkg/ctxmeta"
	"github.com/fireflg/gophprofile/pkg/otelx"
)

// scope - имя instrumentation scope для записей, уходящих в OTLP.
const scope = "github.com/fireflg/gophprofile/pkg/logger"

// contextField - имя поля, в котором контекст едет до моста.
const contextField = "context"

// New создаёт логгер: JSON для production-окружений, консольный вывод для локальной разработки.
func New(env, level string, provider log.LoggerProvider) (*zap.Logger, error) {
	var cfg zap.Config
	if isDevelopment(env) {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}

	cfg.Level = zap.NewAtomicLevelAt(parseLevel(level))
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	base, err := cfg.Build()
	if err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}

	if provider == nil {
		return base, nil
	}

	bridge := otelzap.NewCore(scope, otelzap.WithLoggerProvider(provider))

	return base.WithOptions(zap.WrapCore(func(stdout zapcore.Core) zapcore.Core {
		return zapcore.NewTee(ctxFilterCore{Core: stdout}, bridge)
	})), nil
}

// WithContext готовит логгер к записи в рамках текущего спана.
func WithContext(ctx context.Context, l *zap.Logger) *zap.Logger {
	if l == nil {
		return nil
	}

	return l.With(ContextFields(ctx)...)
}

// ContextFields возвращает поля корреляции: контекст, идентификаторы трассы и пользователя.
func ContextFields(ctx context.Context) []zap.Field {
	fields := []zap.Field{zap.Any(contextField, ctx)}

	if traceID := otelx.TraceIDFrom(ctx); traceID != "" {
		fields = append(fields,
			zap.String("trace_id", traceID),
			zap.String("span_id", otelx.SpanIDFrom(ctx)))
	}

	if userID := ctxmeta.UserIDFrom(ctx); userID != "" {
		fields = append(fields, zap.String("user_id", userID))
	}

	return fields
}

// ctxFilterCore выбрасывает поле с контекстом перед записью в stdout.
type ctxFilterCore struct {
	zapcore.Core
}

// With отбрасывает контекст из полей, накопленных логгером.
func (c ctxFilterCore) With(fields []zapcore.Field) zapcore.Core {
	return ctxFilterCore{Core: c.Core.With(dropContext(fields))}
}

// Check добавляет в запись само ядро-фильтр, а не вложенное: иначе Write
// пойдёт мимо фильтра и контекст всё-таки окажется в выводе.
func (c ctxFilterCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}

	return checked
}

// Write пишет запись без поля с контекстом.
func (c ctxFilterCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	return c.Core.Write(entry, dropContext(fields))
}

// dropContext возвращает поля без значений типа context.Context.
// Исходный срез не меняется: его же получает второе ядро в tee.
func dropContext(fields []zapcore.Field) []zapcore.Field {
	first := -1

	for i, field := range fields {
		if _, ok := field.Interface.(context.Context); ok {
			first = i

			break
		}
	}

	if first < 0 {
		return fields
	}

	kept := make([]zapcore.Field, 0, len(fields)-1)
	kept = append(kept, fields[:first]...)

	for _, field := range fields[first+1:] {
		if _, ok := field.Interface.(context.Context); ok {
			continue
		}

		kept = append(kept, field)
	}

	return kept
}

// Component возвращает дочерний логгер компонента.
func Component(log *zap.Logger, name string) *zap.Logger {
	if log == nil {
		return zap.NewNop()
	}

	return log.With(zap.String("component", name))
}

// FatalStartup сообщает об отказе старта тем же JSON, что и обычные записи:
// на этом этапе логгера ещё нет, а plain text до сборщика логов не доезжает.
func FatalStartup(message string, err error) {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.OutputPaths = []string{"stderr"}

	log, buildErr := cfg.Build()
	if buildErr != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", message, err)

		return
	}

	defer func() { _ = log.Sync() }()

	log.Error(message, zap.Error(err))
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
