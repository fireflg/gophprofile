package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/internal/handlers"
	"github.com/fireflg/gophprofile/internal/handlers/mocks"
	"github.com/fireflg/gophprofile/pkg/logger"
)

func newGetRouter(t *testing.T) (http.Handler, *mocks.MockAvatarUseCase) {
	t.Helper()

	service := mocks.NewMockAvatarUseCase(gomock.NewController(t))
	service.EXPECT().MaxFileSize().Return(int64(maxFileSize)).AnyTimes()
	service.EXPECT().AllowedMimeTypes().Return([]string{"image/png"}).AnyTimes()

	handler := handlers.NewAvatarHandler(service, logger.Nop())

	router := chi.NewRouter()
	router.Get("/api/v1/avatars/{id}", handler.Get)

	return router, service
}

func TestServerErrorIsNotExposed(t *testing.T) {
	router, service := newGetRouter(t)

	id := uuid.New()
	service.EXPECT().GetFile(gomock.Any(), id, "", "").Return(nil, errors.New("база недоступна"))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+id.String(), nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "база недоступна")
	require.JSONEq(t, `{"error":"Internal server error"}`, rec.Body.String())
}

func TestClientErrorKeepsStatus(t *testing.T) {
	router, service := newGetRouter(t)

	id := uuid.New()
	service.EXPECT().GetFile(gomock.Any(), id, "", "").Return(nil, domain.ErrAvatarNotFound)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+id.String(), nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
}
