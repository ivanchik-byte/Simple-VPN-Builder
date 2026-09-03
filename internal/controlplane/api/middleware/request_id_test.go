package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequestIDMiddleware(t *testing.T) {
	t.Run("GeneratesNewIDIfMissing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		var ctxID string
		handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctxID = GetRequestID(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		handler.ServeHTTP(rec, req)

		assert.NotEmpty(t, ctxID)
		assert.Equal(t, ctxID, rec.Header().Get(HeaderXRequestID))
	})

	t.Run("PreservesExistingID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(HeaderXRequestID, "custom-trace-id-1234")
		rec := httptest.NewRecorder()

		var ctxID string
		handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctxID = GetRequestID(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		handler.ServeHTTP(rec, req)

		assert.Equal(t, "custom-trace-id-1234", ctxID)
		assert.Equal(t, "custom-trace-id-1234", rec.Header().Get(HeaderXRequestID))
	})
}
