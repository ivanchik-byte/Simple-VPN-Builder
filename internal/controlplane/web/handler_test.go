package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
