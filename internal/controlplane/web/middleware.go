package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
)

type contextKey string

const (
	AdminContextKey contextKey = "admin_context"
	CookieAuthName  string     = "vpn_admin_token"
)

type AdminContext struct {
	AdminID  uuid.UUID
	Username string
	Role     string
}

// RequireWebAuth ensures that incoming HTTP requests to the web interface carry a valid JWT cookie.
// If unauthenticated or token is revoked/invalid, redirects cleanly to /admin/login.
func RequireWebAuth(jwtManager *auth.JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(CookieAuthName)
			if err != nil || strings.TrimSpace(cookie.Value) == "" {
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			claims, err := jwtManager.ValidateAccessToken(cookie.Value)
			if err != nil {
				// Clear invalid cookie and redirect
				http.SetCookie(w, &http.Cookie{
					Name:     CookieAuthName,
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			adminCtx := &AdminContext{
				AdminID:  claims.AdminID,
				Username: claims.Email,
				Role:     claims.Role,
			}
			ctx := context.WithValue(r.Context(), AdminContextKey, adminCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetAdminContext(ctx context.Context) *AdminContext {
	if val, ok := ctx.Value(AdminContextKey).(*AdminContext); ok {
		return val
	}
	return nil
}
