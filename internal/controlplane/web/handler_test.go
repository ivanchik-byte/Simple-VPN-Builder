package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWeb_TemplateEngine(t *testing.T) {
	engine, err := NewTemplateEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)

	rec := httptest.NewRecorder()
	err = engine.Render(rec, "login.html", map[string]any{
		"IsLoginPage": true,
		"Error":       "",
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Control Plane")
	assert.Contains(t, rec.Body.String(), "Admin Access")
}

func TestWeb_TemplateEngine_HelpersAndPortal(t *testing.T) {
	engine, err := NewTemplateEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)

	rec := httptest.NewRecorder()
	err = engine.RenderStandalone(rec, "portal.html", map[string]any{
		"User": store.User{
			Username:     "alice",
			Status:       pgtype.Text{String: "active", Valid: true},
			TrafficUsed:  pgtype.Int8{Int64: 524288000, Valid: true},
			TrafficLimit: pgtype.Int8{Int64: 1073741824, Valid: true},
		},
		"Plan": map[string]any{
			"Name":        "Pro VPN",
			"Bandwidth":   1000000000,
			"DeviceLimit": 5,
		},
		"SubToken":        "test-token-uuid-12345",
		"SubURL":          "http://localhost:8110/sub/test-token-uuid-12345",
		"ActiveClients":   2,
		"TrafficUsed":     pgtype.Int8{Int64: 524288000, Valid: true},
		"TrafficLimit":    pgtype.Int8{Int64: 1073741824, Valid: true},
		"QuotaPercent":    48.8,
		"TrafficLeft":     "524.0 MB",
		"DaysRemaining":   29,
		"ExpiresAt":       time.Now().Add(29 * 24 * time.Hour),
		"SingboxImport":   "sing-box://import-remote-profile?url=http%3A%2F%2Flocalhost%3A8110%2Fsub%2Ftest-token-uuid-12345%3Fformat%3Dsingbox",
		"ClashImport":     "clash://install-config?url=http%3A%2F%2Flocalhost%3A8110%2Fsub%2Ftest-token-uuid-12345%3Fformat%3Dclash",
		"HiddifyImport":   "hiddify://install-sub?url=http%3A%2F%2Flocalhost%3A8110%2Fsub%2Ftest-token-uuid-12345",
		"StreisandImport": "streisand://import/http%3A%2F%2Flocalhost%3A8110%2Fsub%2Ftest-token-uuid-12345",
		"QRCodeDataURL":   "data:image/svg+xml;utf8,<svg></svg>",
		"PrimaryOS":       "unknown",
		"Nodes":           []any{},
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "alice")
	assert.Contains(t, body, "Bandwidth Consumed")
	assert.Contains(t, body, "test-token-uuid-12345")
}

func TestWeb_ReadHostTelemetry(t *testing.T) {
	cpuPercent, cpuModel, ramUsed, ramTotal, diskUsed, diskTotal := ReadHostTelemetry()
	assert.GreaterOrEqual(t, cpuPercent, 0.0)
	assert.NotEmpty(t, cpuModel)
	assert.GreaterOrEqual(t, ramTotal, int64(0))
	assert.GreaterOrEqual(t, ramUsed, int64(0))
	assert.GreaterOrEqual(t, diskTotal, int64(0))
	assert.GreaterOrEqual(t, diskUsed, int64(0))

	rxRate, txRate := ReadHostNetworkRates()
	assert.GreaterOrEqual(t, rxRate, int64(0))
	assert.GreaterOrEqual(t, txRate, int64(0))
}

func TestWeb_RequireWebAuth_Redirect(t *testing.T) {
	handler := RequireWebAuth(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/admin/login", rec.Header().Get("Location"))
}
