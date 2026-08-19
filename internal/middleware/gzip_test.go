package middleware_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/middleware"
)

// jsonPayload — тело заведомо больше порога MinSize.
var jsonPayload = []byte(`{"items":["` + strings.Repeat("a", 2048) + `"]}`)

func gzipHandler(contentType string, body []byte) http.Handler {
	return middleware.Gzip(gzip.DefaultCompression)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}

		_, _ = w.Write(body)
	}))
}

func requestWithEncoding(encoding string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if encoding != "" {
		req.Header.Set("Accept-Encoding", encoding)
	}

	return req
}

func TestGzipCompressesJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	gzipHandler("application/json", jsonPayload).ServeHTTP(rec, requestWithEncoding("gzip"))

	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	require.Contains(t, rec.Header().Values("Vary"), "Accept-Encoding")
	require.Empty(t, rec.Header().Get("Content-Length"))
	require.Less(t, rec.Body.Len(), len(jsonPayload))

	reader, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)

	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, jsonPayload, decoded)
}

func TestGzipSkipsWithoutAcceptEncoding(t *testing.T) {
	rec := httptest.NewRecorder()
	gzipHandler("application/json", jsonPayload).ServeHTTP(rec, requestWithEncoding(""))

	require.Empty(t, rec.Header().Get("Content-Encoding"))
	require.Equal(t, jsonPayload, rec.Body.Bytes())
	// Vary выставляется всегда: иначе кеш отдаст сжатый ответ клиенту без поддержки gzip.
	require.Contains(t, rec.Header().Values("Vary"), "Accept-Encoding")
}

func TestGzipSkipsWhenQualityIsZero(t *testing.T) {
	rec := httptest.NewRecorder()
	gzipHandler("application/json", jsonPayload).ServeHTTP(rec, requestWithEncoding("gzip;q=0"))

	require.Empty(t, rec.Header().Get("Content-Encoding"))
	require.Equal(t, jsonPayload, rec.Body.Bytes())
}

func TestGzipSkipsImages(t *testing.T) {
	rec := httptest.NewRecorder()
	gzipHandler("image/jpeg", jsonPayload).ServeHTTP(rec, requestWithEncoding("gzip"))

	require.Empty(t, rec.Header().Get("Content-Encoding"))
	require.Equal(t, jsonPayload, rec.Body.Bytes())
}

func TestGzipSkipsSmallResponsesWithKnownLength(t *testing.T) {
	handler := middleware.Gzip(gzip.DefaultCompression)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte(`{"a"}`))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithEncoding("gzip"))

	require.Empty(t, rec.Header().Get("Content-Encoding"))
	require.Equal(t, "5", rec.Header().Get("Content-Length"))
}

func TestGzipSkipsNotModified(t *testing.T) {
	handler := middleware.Gzip(gzip.DefaultCompression)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotModified)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithEncoding("gzip"))

	require.Equal(t, http.StatusNotModified, rec.Code)
	require.Empty(t, rec.Header().Get("Content-Encoding"))
}

func TestGzipMarksETagWeak(t *testing.T) {
	handler := middleware.Gzip(gzip.DefaultCompression)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", `"abc"`)
		_, _ = w.Write(jsonPayload)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithEncoding("gzip"))

	require.Equal(t, `W/"abc"`, rec.Header().Get("ETag"))
}

func TestGzipDetectsContentTypeFromBody(t *testing.T) {
	rec := httptest.NewRecorder()
	gzipHandler("", []byte("<html>"+strings.Repeat("текст ", 200)+"</html>")).
		ServeHTTP(rec, requestWithEncoding("gzip"))

	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
}

func TestGzipReusesWritersAcrossRequests(t *testing.T) {
	handler := gzipHandler("application/json", jsonPayload)

	for range 3 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, requestWithEncoding("gzip"))

		reader, err := gzip.NewReader(rec.Body)
		require.NoError(t, err)

		decoded, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, jsonPayload, decoded)
	}
}
