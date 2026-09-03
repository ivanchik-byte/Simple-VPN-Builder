package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/stretchr/testify/assert"
)

func TestLoggerMiddleware(t *testing.T) {
	var logBuf bytes.Buffer
	l := logger.New("info", "json", &logBuf)
	logger.SetDefault(l)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	rec := httptest.NewRecorder()

	handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created response"))
	}))

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "/api/v1/test")
	assert.Contains(t, logOutput, "201")
}
