package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBootstrapNodeScript(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/bootstrap/node.sh", nil)
	req.Host = "panel.example:8110"
	rec := httptest.NewRecorder()

	BootstrapNodeScript(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/x-shellscript") {
		t.Fatalf("unexpected content type %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	script := string(body)
	for _, want := range []string{
		"--grpc",
		"--node-name",
		"install.sh",
		"--agent",
		"--cp-url",
		"panel.example:9090", // grpc default derived from Host
		"pre-create",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("bootstrap script missing %q", want)
		}
	}
	if strings.Contains(script, "__PANEL_HOST__") || strings.Contains(script, "__GRPC_HOST__") {
		t.Error("unsubstituted placeholders in bootstrap script")
	}
}
