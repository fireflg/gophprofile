// Package api собирает HTTP-маршруты сервиса.
package api

import (
	"compress/gzip"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/handlers"
	"github.com/fireflg/gophprofile/internal/middleware"
)

// healthPath - маршрут проверки для хелсчека.
const healthPath = "/health"

// Deps - зависимости роутера.
type Deps struct {
	Config  *config.Config
	Logger  *zap.Logger
	Avatars *handlers.AvatarHandler
	Health  *handlers.HealthHandler
	// Static - статика веб-интерфейса; nil отключает раздачу.
	Static fs.FS
}

// multipartOverhead - запас на служебные части multipart-запроса сверх размера самого файла.
const multipartOverhead = 64 << 10

// NewRouter собирает маршруты сервиса.
func NewRouter(deps Deps) http.Handler {
	router := chi.NewRouter()

	router.Use(
		middleware.RequestID,
		middleware.UserID(handlers.HeaderUserID),
		middleware.Trace,
		middleware.Metrics(healthPath),
		middleware.Logger(deps.Logger),
		middleware.Recover(deps.Logger),
		middleware.Gzip(gzip.DefaultCompression),
	)

	router.NotFound(handlers.NotFound)
	router.MethodNotAllowed(handlers.MethodNotAllowed)

	router.Get(healthPath, deps.Health.Health)

	uploadLimit := deps.Config.Image.MaxFileSize + multipartOverhead

	router.Route("/api/v1", func(r chi.Router) {
		r.With(chimw.RequestSize(uploadLimit)).Post("/avatars", deps.Avatars.Upload)
		r.Get("/avatars/{id}", deps.Avatars.Get)
		r.Get("/avatars/{id}/metadata", deps.Avatars.Metadata)
		r.Delete("/avatars/{id}", deps.Avatars.Delete)
		r.Get("/users/{user_id}/avatar", deps.Avatars.GetUserAvatar)
		r.Delete("/users/{user_id}/avatar", deps.Avatars.DeleteUserAvatar)
		r.Get("/users/{user_id}/avatars", deps.Avatars.ListUserAvatars)
	})

	if deps.Static != nil {
		router.Handle("/*", http.FileServerFS(deps.Static))
	}

	return otelhttp.NewHandler(router, "http",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != healthPath
		}),
	)
}
