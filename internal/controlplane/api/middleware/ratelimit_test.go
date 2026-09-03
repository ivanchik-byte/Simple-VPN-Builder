package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimiterMiddleware(t *testing.T) {
	rl := NewRateLimiter(nil, 3, 100*time.Millisecond)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/resource", nil)
		req.RemoteAddr = "192.0.2.1:12345"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "Request %d should succeed", i)
		assert.Equal(t, "3", rec.Header().Get("RateLimit-Limit"))
	}

	// 4th request must be throttled
	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "0", rec.Header().Get("RateLimit-Remaining"))

	// Another IP should still be allowed
	otherReq := httptest.NewRequest(http.MethodGet, "/resource", nil)
	otherReq.RemoteAddr = "192.0.2.2:12345"
	otherRec := httptest.NewRecorder()

	handler.ServeHTTP(otherRec, otherReq)
	assert.Equal(t, http.StatusOK, otherRec.Code)
}
