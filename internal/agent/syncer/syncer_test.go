package syncer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

func TestSyncer_HandleConfigUpdate(t *testing.T) {
	cfg := &config.Config{
		Agent: config.AgentConfig{
			SyncInterval: 10 * time.Millisecond,
			WireGuard: config.WireGuardConfig{
				InterfacePrefix: "wg",
			},
		},
	}

	client := grpc.NewClient(cfg)
	syncer := New(client, nil, nil, cfg)
	require.NotNil(t, syncer)

	ctx := context.Background()

	// 1. Nil update
	err := syncer.HandleConfigUpdate(ctx, nil)
	assert.Error(t, err)

	// 2. Initial config apply (mock wgManager is nil, should succeed without error)
	credBytes, err := json.Marshal(map[string]interface{}{
		"public_key":  "x0YF0+k4kH07f0w3e1e4k1e1k0e1e1e1e1e1e1e1e1U=",
		"allowed_ips": "10.0.0.2/32",
		"keepalive":   25,
	})
	require.NoError(t, err)

	update := &agentv1.ConfigUpdate{
		ConfigVersion: 1,
		IsFull:        true,
		Users: []*agentv1.NodeUserConfig{
			{
				UserId: "u-1",
				Credentials: []*agentv1.CredentialConfig{
					{
						CredentialId: "cred-1",
						Protocol:     "wireguard",
						ConfigBytes:  credBytes,
					},
				},
			},
		},
	}

	err = syncer.HandleConfigUpdate(ctx, update)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), syncer.CurrentVersion())

	// 3. Stale update ignored
	err = syncer.HandleConfigUpdate(ctx, update)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), syncer.CurrentVersion())

	// 4. Stop syncer
	err = syncer.Stop(ctx)
	assert.NoError(t, err)
}
