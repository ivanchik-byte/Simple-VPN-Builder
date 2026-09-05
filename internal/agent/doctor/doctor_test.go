package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
)

func TestDoctor_RunBasic(t *testing.T) {
	doc := NewDoctor(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	report := doc.Run(ctx)

	if report.Timestamp == "" {
		t.Errorf("expected non-empty timestamp")
	}
	if report.OS == "" {
		t.Errorf("expected non-empty OS")
	}
	if report.Arch == "" {
		t.Errorf("expected non-empty Arch")
	}
	if len(report.Checks) == 0 {
		t.Errorf("expected at least 1 diagnostic check")
	}

	total := report.PassedCount + report.WarningCount + report.FailedCount
	if total != len(report.Checks) {
		t.Errorf("check counts sum %d does not match checks slice len %d", total, len(report.Checks))
	}
}

func TestDoctor_PrintHuman(t *testing.T) {
	doc := NewDoctor(nil)
	report := doc.Run(context.Background())

	var buf bytes.Buffer
	report.PrintHuman(&buf)

	output := buf.String()
	if !strings.Contains(output, "Simple-VPN-Builder Agent Diagnostics") {
		t.Errorf("missing header in human-readable output: %s", output)
	}
	if !strings.Contains(output, "Summary:") {
		t.Errorf("missing summary in human-readable output: %s", output)
	}
}

func TestDoctor_PrintJSON(t *testing.T) {
	doc := NewDoctor(nil)
	report := doc.Run(context.Background())

	var buf bytes.Buffer
	err := report.PrintJSON(&buf)
	if err != nil {
		t.Fatalf("PrintJSON failed: %v", err)
	}

	var parsed DiagnosticReport
	err = json.Unmarshal(buf.Bytes(), &parsed)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v", err)
	}

	if parsed.Timestamp != report.Timestamp {
		t.Errorf("expected timestamp %s, got %s", report.Timestamp, parsed.Timestamp)
	}
	if len(parsed.Checks) != len(report.Checks) {
		t.Errorf("expected %d checks, got %d", len(report.Checks), len(parsed.Checks))
	}
}

func TestDoctor_WithControlPlaneConfig(t *testing.T) {
	cfg := &config.AgentConfig{
		ControlPlane: "127.0.0.1:65534", // Non-existent port to test connection failure path gracefully
	}
	doc := NewDoctor(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	report := doc.Run(ctx)

	foundCP := false
	for _, c := range report.Checks {
		if c.Name == "Control Plane Connectivity" {
			foundCP = true
			if c.Status != StatusFail {
				t.Errorf("expected StatusFail for non-existent CP port, got %s", c.Status)
			}
			break
		}
	}
	if !foundCP {
		t.Errorf("expected Control Plane Connectivity check in report")
	}
}
