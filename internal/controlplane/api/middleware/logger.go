package middleware

import (
	"net/http"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusResponseWriter) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Logger logs each incoming HTTP request with structured slog fields.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		logger.InfoContext(r.Context(), "HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration_ms", duration.Milliseconds(),
			"bytes", wrapped.bytes,
			"remote_ip", r.RemoteAddr,
		)
	})
}
