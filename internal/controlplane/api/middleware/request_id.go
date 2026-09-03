package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

type contextKey string

const RequestIDKey contextKey = "request_id"

const HeaderXRequestID = "X-Request-ID"

// RequestID extracts or generates a unique request identifier and propagates it in context and response.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(HeaderXRequestID)
		if reqID == "" {
			reqID = uuid.New().String()
		}

		w.Header().Set(HeaderXRequestID, reqID)

		ctx := context.WithValue(r.Context(), RequestIDKey, reqID)
		ctx = context.WithValue(ctx, logger.TraceIDKey, reqID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID returns the request ID from the context if available.
func GetRequestID(ctx context.Context) string {
	if val, ok := ctx.Value(RequestIDKey).(string); ok {
		return val
	}
	return ""
}
