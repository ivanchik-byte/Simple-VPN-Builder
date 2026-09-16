package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
)

type contextKey string

const (
	AdminContextKey contextKey = "admin_context"
	CookieAuthName  string     = "vpn_admin_token"
	CSRFHeaderName  string     = "X-CSRF-Token"
	CSRFFormField   string     = "csrf_token"
)

type AdminContext struct {
	AdminID  uuid.UUID
	Username string
	Role     string
}

// GenerateCSRFToken creates an HMAC-SHA256 authenticated token tied to adminID, secret, and expiry.
func GenerateCSRFToken(adminID string, secret []byte, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	payload := fmt.Sprintf("%s:%d", adminID, exp)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s:%s", payload, sig)
}

// ValidateCSRFToken verifies that a CSRF token matches the adminID, secret, and has not expired.
func ValidateCSRFToken(tokenStr string, adminID string, secret []byte) bool {
	parts := strings.Split(tokenStr, ":")
	if len(parts) != 3 {
		return false
	}
	tokAdminID, expStr, sig := parts[0], parts[1], parts[2]
	if tokAdminID != adminID {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	payload := fmt.Sprintf("%s:%s", tokAdminID, expStr)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expectedSig))
}

// RequireCSRF protects state-changing requests (POST, PUT, PATCH, DELETE) against cross-site request forgery.
func RequireCSRF(jwtManager *auth.JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only validate state-changing requests
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				// Obtain current admin context
				adminCtx := GetAdminContext(r.Context())
				if adminCtx == nil {
					http.Error(w, "Forbidden: unauthenticated request", http.StatusForbidden)
					return
				}

				// Check CSRF token from header or form value
				token := r.Header.Get(CSRFHeaderName)
				if token == "" {
					if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
						_ = r.ParseMultipartForm(32 << 20)
					} else {
						_ = r.ParseForm()
					}
					token = r.FormValue(CSRFFormField)
				}

				var secret []byte
				if jwtManager != nil {
					secret = jwtManager.SecretBytes()
				}
				if token == "" || !ValidateCSRFToken(token, adminCtx.AdminID.String(), secret) {
					http.Error(w, "Forbidden: invalid or missing CSRF token", http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// IsHTTPS returns true if the incoming request was made over TLS or forwarded via a secure HTTPS reverse proxy.
func IsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// RequireWebAuth ensures that incoming HTTP requests to the web interface carry a valid JWT cookie.
// If unauthenticated or token is revoked/invalid, redirects cleanly to /admin/login.
func RequireWebAuth(jwtManager *auth.JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			redirectToLogin := func() {
				if r.Header.Get("HX-Request") == "true" {
					w.Header().Set("HX-Redirect", "/admin/login")
					w.WriteHeader(http.StatusOK)
					return
				}
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			}

			cookie, err := r.Cookie(CookieAuthName)
			if err != nil || strings.TrimSpace(cookie.Value) == "" {
				redirectToLogin()
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
					Secure:   IsHTTPS(r),
					SameSite: http.SameSiteLaxMode,
				})
				redirectToLogin()
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
