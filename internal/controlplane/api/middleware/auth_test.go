package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAPIReader struct {
	key store.ApiKey
	err error
}

func (m *mockAPIReader) GetAPIKeyByPrefix(_ context.Context, _ string) (store.ApiKey, error) {
	return m.key, m.err
}

func TestAuthMiddleware(t *testing.T) {
	jwtManager := auth.NewJWTManager("test-secret-key-32-bytes-long-now!", 15*time.Minute, 24*time.Hour)

	rawKey := "vpn_abcdef1234567890abcdef12"
	hash := sha256.Sum256([]byte(rawKey))
	apiKeyRecord := store.ApiKey{
		ID:      uuid.New(),
		Name:    "ci-service",
		Prefix:  "vpn_abcd",
		KeyHash: hex.EncodeToString(hash[:]),
		Scopes:  []string{"read", "write"},
	}
	apiManager := auth.NewAPIKeyManager(&mockAPIReader{key: apiKeyRecord})

	authenticator := NewAuthenticator(jwtManager, apiManager)

	protectedHandler := authenticator.Authenticate(RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCtx := GetAuth(r.Context())
		require.NotNil(t, authCtx)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok: " + authCtx.AuthType))
	})))

	t.Run("NoCredentials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		rec := httptest.NewRecorder()

		protectedHandler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("ValidJWT", func(t *testing.T) {
		adminID := uuid.New()
		token, err := jwtManager.GenerateAccessToken(adminID, "admin@example.com", "admin")
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		protectedHandler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "ok: jwt", rec.Body.String())
	})

	t.Run("ValidAPIKey", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("X-API-Key", rawKey)
		rec := httptest.NewRecorder()

		protectedHandler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "ok: apikey", rec.Body.String())
	})

	t.Run("RequireRole", func(t *testing.T) {
		roleHandler := authenticator.Authenticate(RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})))

		// 1. Viewer blocked
		viewerToken, err := jwtManager.GenerateAccessToken(uuid.New(), "viewer@example.com", "viewer")
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		rec := httptest.NewRecorder()
		roleHandler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)

		// 2. Admin allowed
		adminToken, err := jwtManager.GenerateAccessToken(uuid.New(), "admin@example.com", "admin")
		require.NoError(t, err)

		req = httptest.NewRequest(http.MethodGet, "/admin-only", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec = httptest.NewRecorder()
		roleHandler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("RequireScope", func(t *testing.T) {
		scopeHandler := authenticator.Authenticate(RequireScope("admin_write")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})))

		req := httptest.NewRequest(http.MethodGet, "/scoped", nil)
		req.Header.Set("X-API-Key", rawKey)
		rec := httptest.NewRecorder()

		scopeHandler.ServeHTTP(rec, req)
		// rawKey only has read, write - missing admin_write
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestScopeMatches(t *testing.T) {
	assert.True(t, ScopeMatches("*", "user:read"))
	assert.True(t, ScopeMatches("admin", "billing:write"))
	assert.True(t, ScopeMatches("billing:write", "billing:write"))
	assert.True(t, ScopeMatches("billing:*", "billing:read"))
	assert.True(t, ScopeMatches("billing:*", "billing:write"))
	assert.False(t, ScopeMatches("billing:read", "billing:write"))
	assert.False(t, ScopeMatches("billing:write", "billing:read"))
	assert.False(t, ScopeMatches("ai", "user:read"))
	assert.True(t, ScopeMatches("ai", "ai"))
	assert.True(t, ScopeMatches("user:read", "user:read"))
	assert.False(t, ScopeMatches("user:read", "user:write"))
}

func TestRequireScope_APIKeyIsolation(t *testing.T) {
	jwtManager := auth.NewJWTManager("test-secret-key-32-bytes-long-now!", 15*time.Minute, 24*time.Hour)

	withScopes := func(scopes []string) *Authenticator {
		raw := "vpn_abcdef1234567890abcdef12"
		hash := sha256.Sum256([]byte(raw))
		mgr := auth.NewAPIKeyManager(&mockAPIReader{key: store.ApiKey{
			ID:      uuid.New(),
			Name:    "scoped",
			Prefix:  raw[:12],
			KeyHash: hex.EncodeToString(hash[:]),
			Scopes:  scopes,
		}})
		return NewAuthenticator(jwtManager, mgr)
	}

	call := func(a *Authenticator, scope, key string) int {
		h := a.Authenticate(RequireScope(scope)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})))
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	raw := "vpn_abcdef1234567890abcdef12"
	// AI-only key reaches /ai/* and nothing else.
	assert.Equal(t, http.StatusOK, call(withScopes([]string{"ai"}), "ai", raw))
	assert.Equal(t, http.StatusForbidden, call(withScopes([]string{"ai"}), "user:read", raw))
	assert.Equal(t, http.StatusForbidden, call(withScopes([]string{"ai"}), "billing:read", raw))
	// Prefix wildcards grant their subtree only.
	assert.Equal(t, http.StatusOK, call(withScopes([]string{"billing:*"}), "billing:read", raw))
	assert.Equal(t, http.StatusForbidden, call(withScopes([]string{"billing:*"}), "user:read", raw))
	// Pre-scoping keys without scopes keep full access.
	assert.Equal(t, http.StatusOK, call(withScopes(nil), "user:write", raw))
}
