// Package ctxmeta переносит сквозные атрибуты запроса через context.Context.
package ctxmeta

import "context"

type contextKey struct{ name string }

var userIDKey = &contextKey{name: "user-id"}

// WithUserID кладёт идентификатор пользователя в контекст.
func WithUserID(ctx context.Context, userID string) context.Context {
	if userID == "" {
		return ctx
	}

	return context.WithValue(ctx, userIDKey, userID)
}

// UserIDFrom достаёт идентификатор пользователя из контекста; "" - если его туда не клали.
func UserIDFrom(ctx context.Context) string {
	userID, _ := ctx.Value(userIDKey).(string)

	return userID
}
