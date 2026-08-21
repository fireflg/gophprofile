// Package logger настраивает структурированный логгер приложения на log/slog.
package logger

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/log"

	"github.com/fireflg/gophprofile/pkg/ctxmeta"
	"github.com/fireflg/gophprofile/pkg/otelx"
)

// scope - имя instrumentation scope для записей, уходящих в OTLP.
const scope = "github.com/fireflg/gophprofile/pkg/logger"

// New создаёт логгер: JSON для production-окружений, текстовый вывод для локальной разработки.
func New(env, level string, provider log.LoggerProvider) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:       parseLevel(level),
		AddSource:   true,
		ReplaceAttr: replaceAttr,
	}

	var handler slog.Handler = slog.NewJSONHandler(os.Stdout, opts)
	if isDevelopment(env) {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	if provider != nil {
		bridge := otelslog.NewHandler(scope, otelslog.WithLoggerProvider(provider))
		handler = multiHandler{handler, bridge}
	}

	return slog.New(ContextHandler(handler))
}

// ContextHandler добавляет к каждой записи идентификаторы трейса и пользователя из контекста.
func ContextHandler(next slog.Handler) slog.Handler {
	return contextHandler{Handler: next}
}

// Component возвращает дочерний логгер компонента.
func Component(log *slog.Logger, name string) *slog.Logger {
	if log == nil {
		return Nop()
	}

	return log.With(slog.String("component", name))
}

// Nop возвращает логгер, который ничего не пишет.
func Nop() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// FatalStartup сообщает об отказе старта тем же JSON, что и обычные записи:
// на этом этапе логгера ещё нет, а plain text до сборщика логов не доезжает.
func FatalStartup(message string, err error) {
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{ReplaceAttr: replaceAttr}))

	log.Error(message, slog.Any("error", err))
}

type contextHandler struct {
	slog.Handler
}

// Handle дописывает корреляцию и передаёт запись дальше.
func (h contextHandler) Handle(ctx context.Context, record slog.Record) error {
	record = record.Clone()

	if traceID := otelx.TraceIDFrom(ctx); traceID != "" {
		record.AddAttrs(
			slog.String("trace_id", traceID),
			slog.String("span_id", otelx.SpanIDFrom(ctx)))
	}

	if userID := ctxmeta.UserIDFrom(ctx); userID != "" {
		record.AddAttrs(slog.String("user_id", userID))
	}

	return h.Handler.Handle(ctx, record)
}

// WithAttrs сохраняет обёртку у дочернего логгера.
func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup сохраняет обёртку у дочернего логгера.
func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name)}
}

// multiHandler раздаёт запись нескольким хендлерам: stdout и мосту OTLP.
type multiHandler []slog.Handler

// Enabled разрешает запись, если её ждёт хотя бы один хендлер.
func (h multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h {
		if handler.Enabled(ctx, level) {
			return true
		}
	}

	return false
}

// Handle отдаёт запись всем хендлерам и собирает их ошибки.
func (h multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error

	for _, handler := range h {
		if !handler.Enabled(ctx, record.Level) {
			continue
		}

		if err := handler.Handle(ctx, record.Clone()); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// WithAttrs добавляет поля каждому хендлеру.
func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make(multiHandler, len(h))
	for i, handler := range h {
		next[i] = handler.WithAttrs(attrs)
	}

	return next
}

// WithGroup открывает группу у каждого хендлера.
func (h multiHandler) WithGroup(name string) slog.Handler {
	next := make(multiHandler, len(h))
	for i, handler := range h {
		next[i] = handler.WithGroup(name)
	}

	return next
}

// replaceAttr приводит служебные поля к виду, привычному сборщику логов.
func replaceAttr(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return attr
	}

	switch attr.Key {
	case slog.TimeKey:
		attr.Key = "ts"
	case slog.SourceKey:
		source, ok := attr.Value.Any().(*slog.Source)
		if !ok {
			return attr
		}

		return slog.String("caller", caller(source))
	}

	return attr
}

func caller(source *slog.Source) string {
	dir, file := filepath.Split(source.File)

	return filepath.Join(filepath.Base(dir), file) + ":" + strconv.Itoa(source.Line)
}

func isDevelopment(env string) bool {
	return strings.EqualFold(env, "development") || strings.EqualFold(env, "local")
}

func parseLevel(level string) slog.Level {
	var parsed slog.Level

	if err := parsed.UnmarshalText([]byte(strings.TrimSpace(level))); err != nil {
		return slog.LevelInfo
	}

	return parsed
}
