// Package middleware содержит HTTP-прослойки сервиса: по одной на файл.
package middleware

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// responseWriter запоминает код ответа и объём тела - это нужно логгеру,
// recover-прослойке и gzip.
type responseWriter struct {
	http.ResponseWriter

	status      int
	bytes       int
	wroteHeader bool
}

func wrapWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (w *responseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}

	w.status = code
	w.wroteHeader = true

	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}

	n, err := w.ResponseWriter.Write(b)
	w.bytes += n

	return n, err
}

// Status - код ответа (200, если обработчик его не выставил явно).
func (w *responseWriter) Status() int { return w.status }

// Written сообщает, начал ли обработчик писать ответ.
func (w *responseWriter) Written() bool { return w.wroteHeader }

// Unwrap нужен http.ResponseController, чтобы добраться до исходного writer'а.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("middleware: ResponseWriter does not implement http.Hijacker")
	}

	return hijacker.Hijack()
}
