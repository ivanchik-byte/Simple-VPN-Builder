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

func TestRateLimiterMiddleware_Bypasses(t *testing.T) {
	rl := NewRateLimiter(nil, 1, time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Static assets should bypass even if limit is exhausted
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/admin/static/css/theme.css", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// Favicon should bypass
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// Telemetry partials should bypass
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/admin/partials/telemetry", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// Requests with a forged session cookie must still be throttled
	req0 := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req0.RemoteAddr = "10.0.0.9:1234"
	req0.AddCookie(&http.Cookie{Name: "vpn_admin_token", Value: "forged-token"})
	rec0 := httptest.NewRecorder()
	handler.ServeHTTP(rec0, req0)
	assert.Equal(t, http.StatusOK, rec0.Code)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
		req.RemoteAddr = "10.0.0.9:1234"
		req.AddCookie(&http.Cookie{Name: "vpn_admin_token", Value: "forged-token"})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	}

	// Web page request when throttled returns HTML
	req1 := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	req2.RemoteAddr = "10.0.0.1:1234"
	req2.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
	assert.Contains(t, rec2.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec2.Body.String(), "Rate Limit Exceeded")
	assert.Contains(t, rec2.Body.String(), "err-countdown")
}

func TestRateLimiterMiddleware_SessionBypass(t *testing.T) {
	rl := NewRateLimiter(nil, 1, time.Minute)
	rl.SetSessionValidator(func(token string) bool { return token == "genuine" })

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Genuine session bypasses even with exhausted limit.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
		req.RemoteAddr = "10.0.0.9:1234"
		req.AddCookie(&http.Cookie{Name: "vpn_admin_token", Value: "genuine"})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// Forged session from another IP is throttled after first request.
	first := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	first.RemoteAddr = "10.0.0.10:1234"
	first.AddCookie(&http.Cookie{Name: "vpn_admin_token", Value: "forged"})
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, first)
	assert.Equal(t, http.StatusOK, firstRec.Code)

	second := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	second.RemoteAddr = "10.0.0.10:1234"
	second.AddCookie(&http.Cookie{Name: "vpn_admin_token", Value: "forged"})
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, second)
	assert.Equal(t, http.StatusTooManyRequests, secondRec.Code)
}
