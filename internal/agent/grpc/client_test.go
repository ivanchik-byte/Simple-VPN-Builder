package grpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

type mockConfigHandler struct {
	mu             sync.Mutex
	handledVersion int64
}

func (m *mockConfigHandler) HandleConfigUpdate(ctx context.Context, update *agentv1.ConfigUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handledVersion = update.ConfigVersion
	return nil
}

func (m *mockConfigHandler) getVersion() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.handledVersion
}

type mockCommandHandler struct {
	mu           sync.Mutex
	executedType string
}

func (m *mockCommandHandler) Execute(ctx context.Context, cmd *agentv1.Command) *agentv1.CommandResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executedType = cmd.Type
	return &agentv1.CommandResult{
		CommandId: cmd.CommandId,
		Success:   true,
		ExitCode:  0,
	}
}

func (m *mockCommandHandler) getType() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.executedType
}

func TestClient_LifecycleAndHandlers(t *testing.T) {
	cfg := &config.Config{
		Agent: config.AgentConfig{
			NodeName:     "test-node",
			ControlPlane: "localhost:50051",
		},
	}

	client := NewClient(cfg)
	require.NotNil(t, client)

	client.SetPublicKey("test-pubkey")
	assert.Equal(t, "test-pubkey", client.publicKey)

	cfgHandler := &mockConfigHandler{}
	cmdHandler := &mockCommandHandler{}
	client.SetHandlers(cfgHandler, cmdHandler)

	// Test message routing
	ctx := context.Background()
	client.handleMessage(ctx, &agentv1.ControlMessage{
		Payload: &agentv1.ControlMessage_Config{
			Config: &agentv1.ConfigUpdate{ConfigVersion: 42},
		},
	})

	client.handleMessage(ctx, &agentv1.ControlMessage{
		Payload: &agentv1.ControlMessage_Command{
			Command: &agentv1.Command{CommandId: "c1", Type: "ping"},
		},
	})

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(42), cfgHandler.getVersion())
	assert.Equal(t, "ping", cmdHandler.getType())

	// Error path when sending disconnected
	err := client.SendHeartbeat(ctx, &agentv1.Heartbeat{})
	assert.Error(t, err)

	err = client.Close()
	assert.NoError(t, err)
}
