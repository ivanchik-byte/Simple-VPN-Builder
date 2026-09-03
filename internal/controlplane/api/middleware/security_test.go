package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
	assert.Equal(t, "1; mode=block", rec.Header().Get("X-XSS-Protection"))
}

func TestBodyLimitMiddleware(t *testing.T) {
	t.Run("UnderLimit", func(t *testing.T) {
		body := bytes.NewBufferString("small body")
		req := httptest.NewRequest(http.MethodPost, "/upload", body)
		rec := httptest.NewRecorder()

		handler := BodyLimit(100)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, "small body", string(data))
			w.WriteHeader(http.StatusOK)
		}))

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("ExceedsLimit", func(t *testing.T) {
		largeData := bytes.Repeat([]byte("a"), 200)
		req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(largeData))
		rec := httptest.NewRecorder()

		handler := BodyLimit(50)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	})
}
