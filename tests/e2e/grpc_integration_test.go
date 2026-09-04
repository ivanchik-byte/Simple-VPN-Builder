package e2e

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	cpgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

type e2eGRPCNodeRepo struct {
	store.NodeRepository
	mu    sync.RWMutex
	nodes map[uuid.UUID]store.Node
}

func newE2EGRPCNodeRepo() *e2eGRPCNodeRepo {
	return &e2eGRPCNodeRepo{nodes: make(map[uuid.UUID]store.Node)}
}

func (r *e2eGRPCNodeRepo) GetByName(_ context.Context, name string) (store.Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, n := range r.nodes {
		if n.Name == name {
			return n, nil
		}
	}
	return store.Node{}, assert.AnError
}

func (r *e2eGRPCNodeRepo) GetByID(_ context.Context, id uuid.UUID) (store.Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n, ok := r.nodes[id]; ok {
		return n, nil
	}
	return store.Node{}, assert.AnError
}

func (r *e2eGRPCNodeRepo) Create(_ context.Context, p store.CreateNodeParams) (store.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := uuid.New()
	node := store.Node{
		ID:        id,
		Name:      p.Name,
		Status:    p.Status,
		PublicKey: p.PublicKey,
	}
	r.nodes[id] = node
	return node, nil
}

func (r *e2eGRPCNodeRepo) UpdateHeartbeat(_ context.Context, id uuid.UUID, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.nodes[id]; ok {
		n.Status = pgtype.Text{String: status, Valid: true}
		n.LastHeartbeat = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		r.nodes[id] = n
		return nil
	}
	return assert.AnError
}

func (r *e2eGRPCNodeRepo) GetStatus(id uuid.UUID) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.nodes[id].Status.String
}

type e2eGRPCUserRepo struct {
	store.UserRepository
	mu           sync.RWMutex
	users        map[uuid.UUID]store.User
	trafficAdded map[uuid.UUID]int64
}

func newE2EGRPCUserRepo() *e2eGRPCUserRepo {
	return &e2eGRPCUserRepo{
		users:        make(map[uuid.UUID]store.User),
		trafficAdded: make(map[uuid.UUID]int64),
	}
}

func (r *e2eGRPCUserRepo) GetByID(_ context.Context, id uuid.UUID) (store.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return store.User{}, assert.AnError
}

func (r *e2eGRPCUserRepo) UpdateTraffic(_ context.Context, id uuid.UUID, bytes int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trafficAdded[id] += bytes
	return nil
}

func (r *e2eGRPCUserRepo) GetTraffic(id uuid.UUID) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.trafficAdded[id]
}

type e2eGRPCCredRepo struct {
	store.CredentialRepository
	creds []store.Credential
}

func (r *e2eGRPCCredRepo) ListActiveByNode(_ context.Context, nodeID uuid.UUID) ([]store.Credential, error) {
	var res []store.Credential
	for _, c := range r.creds {
		if c.NodeID == nodeID {
			res = append(res, c)
		}
	}
	return res, nil
}

func (r *e2eGRPCCredRepo) GetByID(_ context.Context, id uuid.UUID) (store.Credential, error) {
	for _, c := range r.creds {
		if c.ID == id {
			return c, nil
		}
	}
	return store.Credential{}, assert.AnError
}

type e2eGRPCTrafficRepo struct {
	store.TrafficRepository
	mu      sync.RWMutex
	records []store.UpsertTrafficStatsParams
}

func (r *e2eGRPCTrafficRepo) Upsert(_ context.Context, p store.UpsertTrafficStatsParams) (store.TrafficStat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, p)
	return store.TrafficStat{}, nil
}

func (r *e2eGRPCTrafficRepo) GetRecords() []store.UpsertTrafficStatsParams {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copied := make([]store.UpsertTrafficStatsParams, len(r.records))
	copy(copied, r.records)
	return copied
}

func TestE2E_GRPCAgentSyncLifecycle(t *testing.T) {
	// Setup Repositories
	nodeRepo := newE2EGRPCNodeRepo()
	userRepo := newE2EGRPCUserRepo()
	credRepo := &e2eGRPCCredRepo{}
	trafficRepo := &e2eGRPCTrafficRepo{}

	// Seed pre-existing node, user, and credential
	nodeID := uuid.New()
	nodeRepo.nodes[nodeID] = store.Node{
		ID:        nodeID,
		Name:      "e2e-node-frankfurt",
		Status:    pgtype.Text{String: "offline", Valid: true},
		PublicKey: "wg-node-public-key",
	}

	userID := uuid.New()
	userRepo.users[userID] = store.User{
		ID:     userID,
		Status: pgtype.Text{String: "active", Valid: true},
	}

	peerIP := netip.MustParseAddr("10.8.0.100")
	credID := uuid.New()
	credRepo.creds = append(credRepo.creds, store.Credential{
		ID:        credID,
		UserID:    userID,
		NodeID:    nodeID,
		Protocol:  "wireguard",
		PublicKey: pgtype.Text{String: "peer-public-key", Valid: true},
		Ipv4:      &peerIP,
		Status:    pgtype.Text{String: "active", Valid: true},
	})

	sessionMgr := cpgrpc.NewSessionManager()
	configBuilder := service.NewConfigBuilder(nodeRepo, credRepo, userRepo)
	agentService := cpgrpc.NewAgentServiceServer(nodeRepo, userRepo, credRepo, trafficRepo, configBuilder, sessionMgr)

	// Bind gRPC server on a local loopback port
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serverAddr := lis.Addr().String()

	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCAddr: serverAddr,
		},
	}

	server := cpgrpc.NewServer(cfg, agentService)
	go func() {
		_ = server.GRPCServer().Serve(lis)
	}()
	defer server.GRPCServer().GracefulStop()

	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := agentv1.NewAgentServiceClient(conn)
	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()

	stream, err := client.Connect(streamCtx)
	require.NoError(t, err)

	// Stage 1: Registration
	err = stream.Send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Register{
			Register: &agentv1.RegisterRequest{
				NodeName:           "e2e-node-frankfurt",
				WireguardPublicKey: "wg-node-public-key",
				Version:            "v1.5.0",
				Architecture:       "amd64",
			},
		},
	})
	require.NoError(t, err)

	// Expect Initial ConfigUpdate
	resp, err := stream.Recv()
	require.NoError(t, err)
	cfgUpdate := resp.GetConfig()
	require.NotNil(t, cfgUpdate)
	assert.Equal(t, int64(1), cfgUpdate.ConfigVersion)
	assert.True(t, cfgUpdate.IsFull)
	assert.Equal(t, "wg0", cfgUpdate.NodeConfig.WireguardInterface)
	assert.Len(t, cfgUpdate.Users, 1)
	assert.Equal(t, userID.String(), cfgUpdate.Users[0].UserId)

	// Verify session registered and status online
	assert.Equal(t, 1, sessionMgr.Count())
	assert.Equal(t, "online", nodeRepo.GetStatus(nodeID))

	// Stage 2: Config ACK
	err = stream.Send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_ConfigAck{
			ConfigAck: &agentv1.ConfigAck{
				ConfigVersion: 1,
				Success:       true,
			},
		},
	})
	require.NoError(t, err)

	time.Sleep(20 * time.Millisecond)
	session, found := sessionMgr.Get(nodeID)
	require.True(t, found)
	assert.Equal(t, int64(1), session.GetConfigVersion())

	// Stage 3: Heartbeat
	now := time.Now().Unix()
	err = stream.Send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Heartbeat{
			Heartbeat: &agentv1.Heartbeat{
				Timestamp: now,
				Status:    agentv1.NodeStatus_NODE_STATUS_ONLINE,
				System: &agentv1.SystemInfo{
					UptimeSeconds:    3600,
					CpuUsagePercent:  12.5,
					MemoryTotal:      16000000000,
					MemoryUsed:       4000000000,
				},
			},
		},
	})
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, "online", nodeRepo.GetStatus(nodeID))

	// Stage 4: Metrics Ingestion
	err = stream.Send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Metrics{
			Metrics: &agentv1.MetricsReport{
				Timestamp: now,
				Protocols: []*agentv1.ProtocolMetrics{
					{
						Protocol: "wireguard",
						Peers: []*agentv1.PeerMetric{
							{
								PeerId:   credID.String(),
								RxBytes:  52428800,  // 50 MB
								TxBytes: 157286400, // 150 MB
								IsOnline: true,
							},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)

	records := trafficRepo.GetRecords()
	require.Len(t, records, 1)
	assert.Equal(t, userID, records[0].UserID)
	assert.Equal(t, int64(52428800), records[0].RxBytes.Int64)
	assert.Equal(t, int64(157286400), records[0].TxBytes.Int64)
	assert.Equal(t, int64(52428800+157286400), userRepo.GetTraffic(userID))

	// Stage 5: Command Dispatch
	cmdDoneCh := make(chan struct{})
	go func() {
		defer close(cmdDoneCh)
		cmdMsg, recvErr := stream.Recv()
		if recvErr != nil {
			return
		}
		cmd := cmdMsg.GetCommand()
		if cmd != nil {
			_ = stream.Send(&agentv1.AgentMessage{
				Payload: &agentv1.AgentMessage_CommandResult{
					CommandResult: &agentv1.CommandResult{
						CommandId: cmd.CommandId,
						Success:   true,
						Output:    "reloaded successfully",
						ExitCode:  0,
					},
				},
			})
		}
	}()

	cmdResult, err := sessionMgr.SendCommand(context.Background(), nodeID, &agentv1.Command{
		CommandId:      "e2e-cmd-001",
		Type:           "reload_config",
		TimeoutSeconds: 3,
	})
	require.NoError(t, err)
	require.NotNil(t, cmdResult)
	assert.True(t, cmdResult.Success)
	assert.Equal(t, "reloaded successfully", cmdResult.Output)
	<-cmdDoneCh

	// Stage 6: Disconnect and Teardown
	err = stream.CloseSend()
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	// Server unregisters session and marks node offline
	assert.Equal(t, 0, sessionMgr.Count())
	assert.Equal(t, "offline", nodeRepo.GetStatus(nodeID))
}

func TestE2E_GRPCAgentSyncAutoRegisterNewNode(t *testing.T) {
	nodeRepo := newE2EGRPCNodeRepo()
	userRepo := newE2EGRPCUserRepo()
	credRepo := &e2eGRPCCredRepo{}
	trafficRepo := &e2eGRPCTrafficRepo{}

	sessionMgr := cpgrpc.NewSessionManager()
	configBuilder := service.NewConfigBuilder(nodeRepo, credRepo, userRepo)
	agentService := cpgrpc.NewAgentServiceServer(nodeRepo, userRepo, credRepo, trafficRepo, configBuilder, sessionMgr)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serverAddr := lis.Addr().String()

	cfg := &config.Config{Server: config.ServerConfig{GRPCAddr: serverAddr}}
	server := cpgrpc.NewServer(cfg, agentService)
	go func() {
		_ = server.GRPCServer().Serve(lis)
	}()
	defer server.GRPCServer().GracefulStop()

	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := agentv1.NewAgentServiceClient(conn)
	stream, err := client.Connect(context.Background())
	require.NoError(t, err)

	// Register an unknown node name -> should auto-create
	err = stream.Send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Register{
			Register: &agentv1.RegisterRequest{
				NodeName:           "auto-created-ams-node",
				WireguardPublicKey: "auto-pub-key",
			},
		},
	})
	require.NoError(t, err)

	resp, err := stream.Recv()
	require.NoError(t, err)
	assert.NotNil(t, resp.GetConfig())

	createdNode, err := nodeRepo.GetByName(context.Background(), "auto-created-ams-node")
	require.NoError(t, err)
	assert.Equal(t, "auto-created-ams-node", createdNode.Name)
	assert.Equal(t, "online", createdNode.Status.String)

	_ = stream.CloseSend()
}

func TestE2E_GRPCAgentSyncInvalidRegistration(t *testing.T) {
	nodeRepo := newE2EGRPCNodeRepo()
	userRepo := newE2EGRPCUserRepo()
	credRepo := &e2eGRPCCredRepo{}
	trafficRepo := &e2eGRPCTrafficRepo{}

	sessionMgr := cpgrpc.NewSessionManager()
	configBuilder := service.NewConfigBuilder(nodeRepo, credRepo, userRepo)
	agentService := cpgrpc.NewAgentServiceServer(nodeRepo, userRepo, credRepo, trafficRepo, configBuilder, sessionMgr)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serverAddr := lis.Addr().String()

	cfg := &config.Config{Server: config.ServerConfig{GRPCAddr: serverAddr}}
	server := cpgrpc.NewServer(cfg, agentService)
	go func() {
		_ = server.GRPCServer().Serve(lis)
	}()
	defer server.GRPCServer().GracefulStop()

	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := agentv1.NewAgentServiceClient(conn)
	stream, err := client.Connect(context.Background())
	require.NoError(t, err)

	// Register with empty node name -> server returns error
	err = stream.Send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Register{
			Register: &agentv1.RegisterRequest{
				NodeName: "",
			},
		},
	})
	require.NoError(t, err)

	_, err = stream.Recv()
	assert.Error(t, err)
}
