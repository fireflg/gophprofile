package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// HeaderRequestID - заголовок сквозного идентификатора запроса.
const HeaderRequestID = "X-Request-ID"

// maxRequestIDLen - предел длины идентификатора, принимаемого от клиента.
const maxRequestIDLen = 64

type contextKey struct{ name string }

var requestIDKey = &contextKey{name: "request-id"}

// RequestID проставляет идентификатор запроса: берёт из заголовка.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeID(r.Header.Get(HeaderRequestID), maxRequestIDLen)
		if id == "" {
			id = uuid.NewString()
		}

		w.Header().Set(HeaderRequestID, id)

		ctx := context.WithValue(r.Context(), requestIDKey, id)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom достаёт идентификатор запроса из контекста; "" - если прослойка не подключена.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)

	return id
}
