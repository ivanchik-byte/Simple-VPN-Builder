package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/handler"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type routerMockAdminRepo struct {
	admin store.Admin
}

func (m *routerMockAdminRepo) Create(_ context.Context, _ store.CreateAdminParams) (store.Admin, error) {
	return m.admin, nil
}
func (m *routerMockAdminRepo) GetByID(_ context.Context, _ uuid.UUID) (store.Admin, error) {
	return m.admin, nil
}
func (m *routerMockAdminRepo) GetByEmail(_ context.Context, email string) (store.Admin, error) {
	if email == m.admin.Email {
		return m.admin, nil
	}
	return store.Admin{}, assert.AnError
}
func (m *routerMockAdminRepo) List(_ context.Context) ([]store.Admin, error) {
	return []store.Admin{m.admin}, nil
}
func (m *routerMockAdminRepo) Update(_ context.Context, _ store.UpdateAdminParams) (store.Admin, error) {
	return m.admin, nil
}
func (m *routerMockAdminRepo) UpdateLastLogin(_ context.Context, _ uuid.UUID) error {
	return nil
}
func (m *routerMockAdminRepo) UpdatePermissions(_ context.Context, _ uuid.UUID, _ []byte) error {
	return nil
}
func (m *routerMockAdminRepo) Delete(_ context.Context, _ uuid.UUID) error {
	return nil
}

func TestRouter_Integration(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{HTTPAddr: ":0"},
	}

	pwdMgr := auth.NewPasswordManager(10)
	hash, err := pwdMgr.Hash("admin-pass-123")
	require.NoError(t, err)

	admin := store.Admin{
		Email:        "admin@test.com",
		PasswordHash: hash,
		Role:         pgtype.Text{String: "admin", Valid: true},
	}
	adminRepo := &routerMockAdminRepo{admin: admin}

	jwtMgr := auth.NewJWTManager("test-secret-key-32-bytes-long-now!", 15*time.Minute, 24*time.Hour)
	totpMgr := auth.NewTOTPManager("VPN-Test")
	blacklist := auth.NewMemoryBlacklist()
	authHandler := handler.NewAuthHandler(adminRepo, jwtMgr, pwdMgr, totpMgr, blacklist)
	authenticator := middleware.NewAuthenticator(jwtMgr, nil)
	rateLimiter := middleware.NewRateLimiter(nil, 100, time.Minute)

	router := NewRouter(cfg, Handlers{Auth: authHandler}, authenticator, rateLimiter, nil, nil)

	t.Run("HealthzAndHeaders", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
		assert.NotEmpty(t, rec.Header().Get(middleware.HeaderXRequestID))
	})

	t.Run("Readyz", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp HealthResponse
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "ok", resp.Status)
	})

	t.Run("AuthLoginEndpoint", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"email":    "admin@test.com",
			"password": "admin-pass-123",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var tokenResp handler.TokenResponse
		err := json.Unmarshal(rec.Body.Bytes(), &tokenResp)
		require.NoError(t, err)
		assert.NotEmpty(t, tokenResp.AccessToken)
		assert.NotEmpty(t, tokenResp.RefreshToken)
	})
}
