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

	// Static assets route must win over the legacy /ui* redirect.
	areq := httptest.NewRequest(http.MethodGet, "/ui/assets/dashboard-test.js", nil)
	arec := httptest.NewRecorder()
	r.ServeHTTP(arec, areq)
	if loc := arec.Header().Get("Location"); loc == "/admin" {
		t.Fatalf("assets route lost to legacy /ui redirect")
	}

	// Legacy SPA path still redirects to the classic UI.
	lreq := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	lrec := httptest.NewRecorder()
	r.ServeHTTP(lrec, lreq)
	if loc := lrec.Header().Get("Location"); loc != "/admin" {
		t.Fatalf("expected /admin redirect, got %q", loc)
	}
}
