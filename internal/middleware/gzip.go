package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// defaultMinSize - ответы мельче не сжимаем: накладные расходы больше выигрыша.
const defaultMinSize int64 = 512

// defaultCompressibleTypes - префиксы Content-Type, которые имеет смысл сжимать.
// Готовые JPEG/PNG/WebP сюда намеренно не входят.
var defaultCompressibleTypes = []string{
	"text/",
	"application/json",
	"application/javascript",
	"application/xml",
	"image/svg+xml",
}

// GzipConfig - настройки прослойки сжатия.
type GzipConfig struct {
	Level        int
	MinSize      int64
	ContentTypes []string
}

// Gzip сжимает ответ, если клиент прислал Accept-Encoding: gzip.
func Gzip(level int) func(http.Handler) http.Handler {
	return GzipWithConfig(GzipConfig{Level: level})
}

// GzipWithConfig - настраиваемый вариант Gzip.
func GzipWithConfig(cfg GzipConfig) func(http.Handler) http.Handler {
	if cfg.Level < gzip.HuffmanOnly || cfg.Level > gzip.BestCompression {
		cfg.Level = gzip.DefaultCompression
	}
	if cfg.MinSize <= 0 {
		cfg.MinSize = defaultMinSize
	}
	if len(cfg.ContentTypes) == 0 {
		cfg.ContentTypes = defaultCompressibleTypes
	}

	pool := &sync.Pool{New: func() any {
		writer, _ := gzip.NewWriterLevel(io.Discard, cfg.Level)

		return writer
	}}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Ответ зависит от Accept-Encoding даже когда мы не сжали, иначе кеши перепутают варианты.
			w.Header().Add("Vary", "Accept-Encoding")

			if !acceptsGzip(r) {
				next.ServeHTTP(w, r)

				return
			}

			gw := &gzipResponseWriter{ResponseWriter: w, cfg: cfg, pool: pool}
			defer gw.close()

			next.ServeHTTP(gw, r)
		})
	}
}

func acceptsGzip(r *http.Request) bool {
	for _, encoding := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(encoding), ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}

		if quality, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if value, err := strconv.ParseFloat(quality, 64); err == nil && value == 0 {
				return false
			}
		}

		return true
	}

	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter

	cfg  GzipConfig
	pool *sync.Pool

	gz          *gzip.Writer
	wroteHeader bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true

	if w.shouldCompress(code) {
		header := w.Header()
		header.Set("Content-Encoding", "gzip")
		header.Del("Content-Length")
		header.Set("ETag", weakETag(header.Get("ETag")))

		writer, _ := w.pool.Get().(*gzip.Writer)
		writer.Reset(w.ResponseWriter)
		w.gz = writer
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		if w.Header().Get("Content-Type") == "" && len(b) > 0 {
			w.Header().Set("Content-Type", http.DetectContentType(b))
		}

		w.WriteHeader(http.StatusOK)
	}

	if w.gz != nil {
		return w.gz.Write(b)
	}

	return w.ResponseWriter.Write(b)
}

func (w *gzipResponseWriter) shouldCompress(code int) bool {
	if code < http.StatusOK || code == http.StatusNoContent || code == http.StatusNotModified {
		return false
	}

	header := w.Header()
	if header.Get("Content-Encoding") != "" {
		return false
	}

	if declared := header.Get("Content-Length"); declared != "" {
		if size, err := strconv.ParseInt(declared, 10, 64); err == nil && size < w.cfg.MinSize {
			return false
		}
	}

	contentType, _, _ := strings.Cut(header.Get("Content-Type"), ";")
	contentType = strings.ToLower(strings.TrimSpace(contentType))

	for _, prefix := range w.cfg.ContentTypes {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}

	return false
}

func (w *gzipResponseWriter) close() {
	if w.gz == nil {
		return
	}

	_ = w.gz.Close()
	w.gz.Reset(io.Discard)
	w.pool.Put(w.gz)
	w.gz = nil
}

func (w *gzipResponseWriter) Flush() {
	if w.gz != nil {
		_ = w.gz.Flush()
	}

	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap нужен http.ResponseController и внешним обёрткам.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// weakETag помечает ETag слабым: тело изменилось из-за сжатия, побайтовым совпадением он уже не является.
func weakETag(etag string) string {
	if etag == "" || strings.HasPrefix(etag, "W/") {
		return etag
	}

	return "W/" + etag
}
