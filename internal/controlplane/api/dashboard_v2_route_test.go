package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/web"
)

func TestDashboardV2Routes(t *testing.T) {
	jwtManager := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!", 15*time.Minute, 24*time.Hour)
	authenticator := middleware.NewAuthenticator(jwtManager, nil)
	r := NewRouter(nil, Handlers{Web: &web.Handler{}}, authenticator, nil, nil, nil)

	// Dashboard preview page requires auth (redirects to login, not 404).
	for _, path := range []string{"/admin/dashboard-v2", "/admin/nodes-v2", "/admin/users-v2", "/admin/plans-v2", "/admin/credentials-v2", "/admin/csrf-token", "/admin/users-data", "/admin/plans-data", "/admin/analytics-v2", "/admin/audit-v2", "/admin/audit-data", "/admin/broadcast-v2", "/admin/settings-v2", "/admin/node-v2"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%s: expected redirect to login, got %d", path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/admin/login" {
			t.Fatalf("%s: expected /admin/login, got %q", path, loc)
		}
	}

	// Static assets are public (login page loads pre-auth); unknown files 404.
	areq := httptest.NewRequest(http.MethodGet, "/ui/assets/dashboard-test.js", nil)
	arec := httptest.NewRecorder()
	r.ServeHTTP(arec, areq)
	if arec.Code == http.StatusSeeOther {
		t.Fatalf("assets route must not redirect, got %d", arec.Code)
	}

	// Removed legacy SPA path falls through to the error page.
	lreq := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	lrec := httptest.NewRecorder()
	r.ServeHTTP(lrec, lreq)
	if lrec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for removed legacy path, got %d", lrec.Code)
	}

	// Classic page URLs redirect to the new console.
	// Public login goes straight to login-v2.
	loginReq := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	loginRec := httptest.NewRecorder()
	r.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusSeeOther || loginRec.Header().Get("Location") != "/admin/login-v2" {
		t.Fatalf("expected /admin/login -> /admin/login-v2, got %d %q", loginRec.Code, loginRec.Header().Get("Location"))
	}
	// Protected classics sit behind auth (303 to login proves the route matched).
	for _, from := range []string{"/admin", "/admin/dashboard", "/admin/nodes", "/admin/users", "/admin/plans", "/admin/settings", "/admin/broadcast"} {
		req := httptest.NewRequest(http.MethodGet, from, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
			t.Fatalf("%s: expected auth redirect, got %d %q", from, rec.Code, rec.Header().Get("Location"))
		}
	}
}
