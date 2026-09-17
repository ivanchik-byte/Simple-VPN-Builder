package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
)

type authContextKey string

const AuthCtxKey authContextKey = "auth_context"

// AuthContext holds validated authentication metadata for the request.
type AuthContext struct {
	UserID   uuid.UUID
	Email    string
	Role     string
	Scopes   []string
	AuthType string // "jwt" or "apikey"
}

// ScopeMatches reports whether a granted scope satisfies a required scope.
// Supported forms for the granted scope:
//   - "*" or "admin" (or empty scope list, handled by HasScope) grant everything
//     for backward compatibility with keys issued before scoping.
//   - exact match ("billing:read" satisfies "billing:read").
//   - prefix wildcard ("billing:*" satisfies "billing:read", "node:*" satisfies "node:write").
func ScopeMatches(granted, required string) bool {
	granted = strings.TrimSpace(granted)
	required = strings.TrimSpace(required)
	if granted == "*" || granted == "admin" {
		return true
	}
	if granted == required {
		return true
	}
	if strings.HasSuffix(granted, "*") {
		prefix := strings.TrimSuffix(granted, "*")
		if prefix != "" && strings.HasPrefix(required, prefix) {
			return true
		}
	}
	return false
}

// HasScope checks if the authenticated entity has the specified scope.
func (ac *AuthContext) HasScope(scope string) bool {
	// Keys issued before scoping carry no scopes — treat them as admin
	// for backward compatibility.
	if ac.AuthType == "apikey" && len(ac.Scopes) == 0 {
		return true
	}
	for _, s := range ac.Scopes {
		if ScopeMatches(s, scope) {
			return true
		}
	}
	return false
}

// Authenticator handles dual-scheme authentication (Bearer JWT & X-API-Key).
type Authenticator struct {
	jwtManager     *auth.JWTManager
	apiKeyManager  *auth.APIKeyManager
	internalAPIKey string
}

func NewAuthenticator(jwtManager *auth.JWTManager, apiKeyManager *auth.APIKeyManager) *Authenticator {
	return &Authenticator{
		jwtManager:    jwtManager,
		apiKeyManager: apiKeyManager,
	}
}

func (a *Authenticator) SetInternalAPIKey(key string) {
	a.internalAPIKey = strings.TrimSpace(key)
}

func (a *Authenticator) JWTManager() *auth.JWTManager {
	return a.jwtManager
}

// Authenticate inspects headers and validates credentials if present.
func (a *Authenticator) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// 1. Check Bearer JWT or vpn_admin_token cookie (for web console AJAX calls)
		var tokenStr string
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
		} else if cookie, err := r.Cookie("vpn_admin_token"); err == nil && strings.TrimSpace(cookie.Value) != "" {
			tokenStr = strings.TrimSpace(cookie.Value)
		}

		if tokenStr != "" && a.jwtManager != nil {
			claims, err := a.jwtManager.ValidateAccessToken(tokenStr)
			if err == nil {
				// Forced rotation: tokens flagged must_change may only call
				// the rotation/logout endpoints; everything else is 403.
				// (Login/Refresh enforce this too; this closes stale tokens
				// issued before the flag was set.)
				if claims.MustChangePassword && !isPasswordRotationPath(r.URL.Path) {
					response.RespondForbidden(w, r, "Password change required: rotate the default credentials")
					return
				}
				authCtx := &AuthContext{
					UserID:   claims.AdminID,
					Email:    claims.Email,
					Role:     claims.Role,
					Scopes:   []string{"*"},
					AuthType: "jwt",
				}
				ctx = context.WithValue(ctx, AuthCtxKey, authCtx)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 2. Check X-API-Key
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != "" {
			if a.internalAPIKey != "" && apiKey == a.internalAPIKey {
				authCtx := &AuthContext{
					UserID:   uuid.Nil,
					Email:    "internal-service",
					Role:     "owner",
					Scopes:   []string{"*"},
					AuthType: "apikey",
				}
				ctx = context.WithValue(ctx, AuthCtxKey, authCtx)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if a.apiKeyManager != nil {
				keyRecord, err := a.apiKeyManager.ValidateKey(ctx, apiKey)
				if err == nil {
					authCtx := &AuthContext{
						UserID:   keyRecord.ID,
						Email:    keyRecord.Name,
						Role:     "api_client",
						Scopes:   keyRecord.Scopes,
						AuthType: "apikey",
					}
					ctx = context.WithValue(ctx, AuthCtxKey, authCtx)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isPasswordRotationPath allows must_change tokens only on rotation endpoints.
func isPasswordRotationPath(path string) bool {
	return strings.HasSuffix(path, "/auth/change-password") ||
		strings.HasSuffix(path, "/auth/logout") ||
		strings.HasSuffix(path, "/admin/change-password") ||
		strings.HasSuffix(path, "/admin/logout")
}

// GetAuth retrieves the AuthContext from the context if authenticated.
func GetAuth(ctx context.Context) *AuthContext {
	if val, ok := ctx.Value(AuthCtxKey).(*AuthContext); ok {
		return val
	}
	return nil
}

// RequireAuth blocks unauthenticated requests with an RFC 7807 401 response.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetAuth(r.Context()) == nil {
			response.RespondUnauthorized(w, r, "Authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole checks that the authenticated user matches one of the allowed roles.
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCtx := GetAuth(r.Context())
			if authCtx == nil {
				response.RespondUnauthorized(w, r, "Authentication required")
				return
			}

			for _, role := range allowedRoles {
				if authCtx.Role == role || authCtx.Role == "owner" || authCtx.Role == "superadmin" {
					next.ServeHTTP(w, r)
					return
				}
			}

			if authCtx.AuthType == "apikey" && (authCtx.HasScope("admin") || authCtx.HasScope("*")) {
				next.ServeHTTP(w, r)
				return
			}

			response.RespondForbidden(w, r, "Insufficient permissions for this operation")
		})
	}
}

// RequireScope verifies that the authenticated entity possesses the given scope.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCtx := GetAuth(r.Context())
			if authCtx == nil {
				response.RespondUnauthorized(w, r, "Authentication required")
				return
			}

			if !authCtx.HasScope(scope) {
				response.RespondForbidden(w, r, "Missing required scope: "+scope)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
