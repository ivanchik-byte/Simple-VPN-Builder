package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/web"
	"github.com/stretchr/testify/assert"
)

func TestAIHandler_AccessControl(t *testing.T) {
	h := NewAIHandler(nil, nil)

	// 1. Unauthenticated request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/settings", nil)
	rr := httptest.NewRecorder()
	h.GetSettings(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), "restricted to owner or authorized administrators")

	// 2. Standard admin web session without permission
	reqAdmin := httptest.NewRequest(http.MethodGet, "/api/v1/ai/settings", nil)
	adminCtx := &web.AdminContext{
		AdminID:  uuid.New(),
		Username: "operator@test.local",
		Role:     "admin",
	}
	reqAdmin = reqAdmin.WithContext(context.WithValue(reqAdmin.Context(), web.AdminContextKey, adminCtx))
	rrAdmin := httptest.NewRecorder()
	h.GetSettings(rrAdmin, reqAdmin)
	assert.Equal(t, http.StatusForbidden, rrAdmin.Code)

	// 3. Superadmin without permission (default is false)
	reqSuper := httptest.NewRequest(http.MethodGet, "/api/v1/ai/settings", nil)
	superCtx := &web.AdminContext{
		AdminID:  uuid.New(),
		Username: "super@test.local",
		Role:     "superadmin",
	}
	reqSuper = reqSuper.WithContext(context.WithValue(reqSuper.Context(), web.AdminContextKey, superCtx))
	rrSuper := httptest.NewRecorder()
	h.GetSettings(rrSuper, reqSuper)
	assert.Equal(t, http.StatusForbidden, rrSuper.Code)

	// 4. Owner web session - permission granted
	reqOwner := httptest.NewRequest(http.MethodGet, "/api/v1/ai/settings", nil)
	ownerCtx := &web.AdminContext{
		AdminID:  uuid.New(),
		Username: "owner@test.local",
		Role:     "owner",
	}
	reqOwner = reqOwner.WithContext(context.WithValue(reqOwner.Context(), web.AdminContextKey, ownerCtx))
	rrOwner := httptest.NewRecorder()
	h.GetSettings(rrOwner, reqOwner)
	// Since copilot service is nil in this unit test, it returns 503 (service unavailable) instead of 403 (forbidden)
	assert.Equal(t, http.StatusServiceUnavailable, rrOwner.Code)

	// 5. Owner API Auth JWT
	reqAuthOwner := httptest.NewRequest(http.MethodGet, "/api/v1/ai/settings", nil)
	authOwner := &middleware.AuthContext{
		UserID:   uuid.New(),
		Email:    "owner@test.local",
		Role:     "owner",
		AuthType: "jwt",
	}
	reqAuthOwner = reqAuthOwner.WithContext(context.WithValue(reqAuthOwner.Context(), middleware.AuthCtxKey, authOwner))
	rrAuthOwner := httptest.NewRecorder()
	h.GetSettings(rrAuthOwner, reqAuthOwner)
	assert.Equal(t, http.StatusServiceUnavailable, rrAuthOwner.Code)
}

// customWriterWithoutFlusher implements http.ResponseWriter without http.Flusher.
type customWriterWithoutFlusher struct {
	header http.Header
	status int
	body   []byte
}

func (w *customWriterWithoutFlusher) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *customWriterWithoutFlusher) Write(b []byte) (int, error) {
	w.body = append(w.body, b...)
	return len(b), nil
}

func (w *customWriterWithoutFlusher) WriteHeader(statusCode int) {
	w.status = statusCode
}

func TestAIHandler_Chat_NoFlusher_DoesNotReturn500(t *testing.T) {
	// AIHandler with nil copilot returns 503, but not 500 "Streaming not supported"
	h := NewAIHandler(nil, nil)
	writer := &customWriterWithoutFlusher{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat", nil)
	ownerCtx := &web.AdminContext{
		AdminID:  uuid.New(),
		Username: "owner@test.local",
		Role:     "owner",
	}
	req = req.WithContext(context.WithValue(req.Context(), web.AdminContextKey, ownerCtx))

	h.Chat(writer, req)

	// Since copilot is nil, it returns StatusServiceUnavailable (503), not StatusInternalServerError (500)
	assert.Equal(t, http.StatusServiceUnavailable, writer.status)
	assert.NotContains(t, string(writer.body), "Streaming not supported")
}

func TestAIHandler_TestConnection_AccessControl(t *testing.T) {
	h := NewAIHandler(nil, nil)

	// 1. Unauthenticated request
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/test", nil)
	rr := httptest.NewRecorder()
	h.TestConnection(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)

	// 2. Owner request with nil copilot -> 503
	ownerCtx := &web.AdminContext{
		AdminID:  uuid.New(),
		Username: "owner@test.local",
		Role:     "owner",
	}
	reqOwner := httptest.NewRequest(http.MethodPost, "/api/v1/ai/test", nil)
	reqOwner = reqOwner.WithContext(context.WithValue(reqOwner.Context(), web.AdminContextKey, ownerCtx))
	rrOwner := httptest.NewRecorder()
	h.TestConnection(rrOwner, reqOwner)
	assert.Equal(t, http.StatusServiceUnavailable, rrOwner.Code)
}

