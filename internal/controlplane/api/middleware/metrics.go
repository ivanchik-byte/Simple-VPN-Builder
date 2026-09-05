package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	sharedmetrics "github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/metrics"
)

// PrometheusMetrics records RED metrics for incoming HTTP requests.
func PrometheusMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Seconds()
		routePattern := chi.RouteContext(r.Context()).RoutePattern()
		if routePattern == "" {
			routePattern = r.URL.Path
		}

		statusStr := fmt.Sprintf("%d", wrapped.statusCode)
		sharedmetrics.CPHTTPRequestsTotal.WithLabelValues(r.Method, routePattern, statusStr).Inc()
		sharedmetrics.CPHTTPRequestDuration.WithLabelValues(r.Method, routePattern).Observe(duration)
	})
}
