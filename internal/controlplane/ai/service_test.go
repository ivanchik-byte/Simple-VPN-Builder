package ai_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/ai/knowledge"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/ai/tools"
)

func TestSystemPromptBuilder(t *testing.T) {
	builder := knowledge.NewSystemPromptBuilder()
	facts := knowledge.DynamicClusterContext{
		OnlineNodes:   3,
		DrainingNodes: 1,
		OfflineNodes:  0,
		ActiveUsers:   42,
		TotalUsers:    100,
		ActiveAlerts:  []string{"High bandwidth spike on node nl-ams-01"},
		Timestamp:     time.Now().UTC(),
	}

	prompt := builder.Build(facts)
	if len(prompt) == 0 {
		t.Fatalf("expected non-empty prompt")
	}

	// Verify key knowledge components
	for _, substr := range []string{
		"Simple-VPN-Builder Infrastructure Agent Playbook",
		"AmneziaWG",
		"VLESS Reality",
		"High bandwidth spike on node nl-ams-01",
		"Active Exit Nodes: 3 online, 1 draining, 0 offline",
	} {
		if !contains(prompt, substr) {
			t.Errorf("prompt missing required substring: %s", substr)
		}
	}
}

func TestSafetyManager(t *testing.T) {
	safety := tools.NewSafetyManager([]byte("test-secret-salt"))
	actionName := "node_drain"
	payloadHash := tools.ComputePayloadHash("node_drain:node-123")

	token, exp := safety.GenerateConfirmationToken(actionName, payloadHash)
	if token == "" || exp.Before(time.Now()) {
		t.Fatalf("invalid generated token")
	}

	// Validate valid token
	if err := safety.ValidateConfirmationToken(token, actionName, payloadHash); err != nil {
		t.Fatalf("expected token validation to pass: %v", err)
	}

	// Validate mismatched action
	if err := safety.ValidateConfirmationToken(token, "node_add", payloadHash); err == nil {
		t.Fatalf("expected validation failure for mismatched action")
	}

	// Validate tampered hash
	if err := safety.ValidateConfirmationToken(token, actionName, "tampered-hash"); err == nil {
		t.Fatalf("expected validation failure for tampered hash")
	}
}

func TestToolRegistrySpecs(t *testing.T) {
	safety := tools.NewSafetyManager(nil)
	reg := tools.NewToolRegistry(safety)

	reg.Register(tools.ToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
		Safety:      tools.SafetyReadOnly,
		Parameters:  json.RawMessage(`{"type": "object"}`),
		Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
			return map[string]string{"status": "ok"}, nil
		},
	})

	specs := reg.GetSpecs()
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Function.Name != "test_tool" {
		t.Fatalf("unexpected spec name: %s", specs[0].Function.Name)
	}

	res, err := reg.Execute(context.Background(), "test_tool", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	m, ok := res.(map[string]string)
	if !ok || m["status"] != "ok" {
		t.Fatalf("unexpected result: %v", res)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) && searchString(s, substr)))
}

func searchString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
