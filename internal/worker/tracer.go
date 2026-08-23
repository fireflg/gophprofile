package worker

import (
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/pkg/otelx"
)

// tracer - трейсер модуля; его имя попадает в спаны как instrumentation scope.
var tracer = otelx.Tracer("internal/worker")

// recordError помечает спан ошибкой и возвращает её же для удобства вызова в return.
// Клиентские ошибки статус спана не меняют: по конвенциям OpenTelemetry 4xx остаётся Unset.
func recordError(span trace.Span, err error) error {
	if err == nil || domain.IsClientError(err) {
		return err
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	return err
}
