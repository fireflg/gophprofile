package middleware

import (
	"net/http"
	"strings"

	"github.com/fireflg/gophprofile/pkg/ctxmeta"
)

// UserID переносит идентификатор пользователя из заголовка в контекст.
//
// Дальше его подхватывает logger.WithContext, и user_id оказывается в каждой
// строке лога запроса, а не только в строке доступа: по нему ищутся все следы
// конкретного пользователя. В лейблы осознанно не добавляю, потому что высокой становится координальность.
func UserID(header string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := strings.TrimSpace(r.Header.Get(header))
			if id == "" {
				next.ServeHTTP(w, r)

				return
			}

			next.ServeHTTP(w, r.WithContext(ctxmeta.WithUserID(r.Context(), id)))
		})
	}
}
