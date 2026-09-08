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

// HasScope checks if the authenticated entity has the specified scope.
func (ac *AuthContext) HasScope(scope string) bool {
	if ac.Role == "owner" || ac.Role == "superadmin" || ac.Role == "admin" {
		return true
	}
	for _, s := range ac.Scopes {
		if s == scope || s == "*" {
			return true
		}
	}
	return false
}

// Authenticator handles dual-scheme authentication (Bearer JWT & X-API-Key).
type Authenticator struct {
	jwtManager    *auth.JWTManager
	apiKeyManager *auth.APIKeyManager
}

func NewAuthenticator(jwtManager *auth.JWTManager, apiKeyManager *auth.APIKeyManager) *Authenticator {
	return &Authenticator{
		jwtManager:    jwtManager,
		apiKeyManager: apiKeyManager,
	}
}

func (a *Authenticator) JWTManager() *auth.JWTManager {
	return a.jwtManager
}

// Authenticate inspects headers and validates credentials if present.
func (a *Authenticator) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// 1. Check Bearer JWT
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := a.jwtManager.ValidateAccessToken(tokenStr)
			if err == nil {
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
		if apiKey != "" && a.apiKeyManager != nil {
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

		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
