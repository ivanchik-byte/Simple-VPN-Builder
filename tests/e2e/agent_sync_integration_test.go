package e2e

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/manager"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/metrics"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/syncer"
	cpgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

func TestE2E_AgentSyncAndCommandExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Setup Control Plane gRPC Server
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serverAddr := lis.Addr().String()

	nodeRepo := newE2EGRPCNodeRepo()
	userRepo := newE2EGRPCUserRepo()
	credRepo := &e2eGRPCCredRepo{}
	trafficRepo := &e2eGRPCTrafficRepo{}

	nodeID := uuid.New()
	nodeRepo.nodes[nodeID] = store.Node{
		ID:        nodeID,
		Name:      "test-e2e-node",
		Status:    pgtype.Text{String: "offline", Valid: true},
		PublicKey: "mock-pubkey",
	}

	userID := uuid.New()
	userRepo.users[userID] = store.User{
		ID:     userID,
		Status: pgtype.Text{String: "active", Valid: true},
	}

	peerIP := netip.MustParseAddr("10.100.0.2")
	credRepo.creds = append(credRepo.creds, store.Credential{
		ID:        uuid.New(),
		UserID:    userID,
		NodeID:    nodeID,
		Protocol:  "wireguard",
		PublicKey: pgtype.Text{String: "k7F0+k4kH07f0w3e1e4k1e1k0e1e1e1e1e1e1e1e1U=", Valid: true},
		Ipv4:      &peerIP,
		Status:    pgtype.Text{String: "active", Valid: true},
	})

	sessionMgr := cpgrpc.NewSessionManager()
	configBuilder := service.NewConfigBuilder(nodeRepo, credRepo, userRepo)
	agentService := cpgrpc.NewAgentServiceServer(nodeRepo, userRepo, credRepo, trafficRepo, configBuilder, sessionMgr)

	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCAddr: serverAddr,
		},
	}

	server := cpgrpc.NewServer(cfg, agentService)
	go func() {
		_ = server.GRPCServer().Serve(lis)
	}()
	defer server.GRPCServer().Stop()

	// 2. Setup Node Agent
	agentCfg := &config.Config{
		Agent: config.AgentConfig{
			NodeName:        "test-e2e-node",
			ControlPlane:    serverAddr,
			SyncInterval:    50 * time.Millisecond,
			MetricsInterval: 50 * time.Millisecond,
			WireGuard: config.WireGuardConfig{
				InterfacePrefix: "wg",
			},
		},
	}

	agentClient := agentgrpc.NewClient(agentCfg)
	agentClient.SetPublicKey("mock-pubkey")

	configSyncer := syncer.New(agentClient, nil, nil, agentCfg)
	executor := manager.NewCommandExecutor(nil, func(c context.Context) error {
		return nil
	})
	agentClient.SetHandlers(configSyncer, executor)

	err = agentClient.Connect(ctx)
	require.NoError(t, err)
	defer agentClient.Close()

	// Wait for registration and session establishment
	var sess *cpgrpc.AgentSession
	assert.Eventually(t, func() bool {
		s, ok := sessionMgr.Get(nodeID)
		if ok && s != nil {
			sess = s
			return true
		}
		return false
	}, 3*time.Second, 50*time.Millisecond)
	require.NotNil(t, sess)

	// 3. Test Config Push to Agent
	configUpdate := &agentv1.ConfigUpdate{
		ConfigVersion: 10,
		IsFull:        true,
	}

	err = sess.Send(&agentv1.ControlMessage{
		Payload: &agentv1.ControlMessage_Config{
			Config: configUpdate,
		},
	})
	require.NoError(t, err)

	// Wait for syncer to process config update
	assert.Eventually(t, func() bool {
		return configSyncer.CurrentVersion() == 10
	}, 2*time.Second, 50*time.Millisecond)

	// 4. Test Remote Command Execution: ping
	pingCmd := &agentv1.Command{
		CommandId: "ping-test-1",
		Type:      "ping",
	}

	resp, err := sessionMgr.SendCommand(ctx, nodeID, pingCmd)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, int32(0), resp.ExitCode)
	assert.Contains(t, resp.Output, "pong")

	// 5. Test Metrics Collector
	collector := metrics.NewCollector(nil, nil, agentClient, agentCfg)
	require.NotNil(t, collector)
}
