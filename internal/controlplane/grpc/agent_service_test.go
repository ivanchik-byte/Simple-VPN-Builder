package grpc

import (
	"context"
	"io"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

type mockAgentNodeRepo struct {
	store.NodeRepository
	mu    sync.RWMutex
	nodes map[uuid.UUID]store.Node
}

func newMockAgentNodeRepo() *mockAgentNodeRepo {
	return &mockAgentNodeRepo{nodes: make(map[uuid.UUID]store.Node)}
}

func (m *mockAgentNodeRepo) GetByName(_ context.Context, name string) (store.Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.nodes {
		if n.Name == name {
			return n, nil
		}
	}
	return store.Node{}, assert.AnError
}

func (m *mockAgentNodeRepo) GetByID(_ context.Context, id uuid.UUID) (store.Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if n, ok := m.nodes[id]; ok {
		return n, nil
	}
	return store.Node{}, assert.AnError
}

func (m *mockAgentNodeRepo) Create(_ context.Context, p store.CreateNodeParams) (store.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := uuid.New()
	n := store.Node{
		ID:        id,
		Name:      p.Name,
		Status:    p.Status,
		PublicKey: p.PublicKey,
	}
	m.nodes[id] = n
	return n, nil
}

func (m *mockAgentNodeRepo) UpdateHeartbeat(_ context.Context, id uuid.UUID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.nodes[id]; ok {
		n.Status = pgtype.Text{String: status, Valid: true}
		n.LastHeartbeat = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		m.nodes[id] = n
		return nil
	}
	return assert.AnError
}

func (m *mockAgentNodeRepo) GetStatus(id uuid.UUID) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nodes[id].Status.String
}

type mockAgentUserRepo struct {
	store.UserRepository
	mu           sync.RWMutex
	users        map[uuid.UUID]store.User
	trafficAdded map[uuid.UUID]int64
}

func newMockAgentUserRepo() *mockAgentUserRepo {
	return &mockAgentUserRepo{
		users:        make(map[uuid.UUID]store.User),
		trafficAdded: make(map[uuid.UUID]int64),
	}
}

func (m *mockAgentUserRepo) GetByID(_ context.Context, id uuid.UUID) (store.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return store.User{}, assert.AnError
}

func (m *mockAgentUserRepo) UpdateTraffic(_ context.Context, id uuid.UUID, bytes int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trafficAdded[id] += bytes
	return nil
}

func (m *mockAgentUserRepo) GetTraffic(id uuid.UUID) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.trafficAdded[id]
}

type mockAgentCredRepo struct {
	store.CredentialRepository
	creds []store.Credential
}

func (m *mockAgentCredRepo) GetByID(_ context.Context, id uuid.UUID) (store.Credential, error) {
	for _, c := range m.creds {
		if c.ID == id {
			return c, nil
		}
	}
	return store.Credential{}, assert.AnError
}

func (m *mockAgentCredRepo) ListActiveByNode(_ context.Context, nodeID uuid.UUID) ([]store.Credential, error) {
	var list []store.Credential
	for _, c := range m.creds {
		if c.NodeID == nodeID {
			list = append(list, c)
		}
	}
	return list, nil
}

type mockAgentTrafficRepo struct {
	store.TrafficRepository
	mu      sync.RWMutex
	records []store.UpsertTrafficStatsParams
}

func (m *mockAgentTrafficRepo) Upsert(_ context.Context, p store.UpsertTrafficStatsParams) (store.TrafficStat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, p)
	return store.TrafficStat{}, nil
}

func (m *mockAgentTrafficRepo) GetRecords() []store.UpsertTrafficStatsParams {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copied := make([]store.UpsertTrafficStatsParams, len(m.records))
	copy(copied, m.records)
	return copied
}

// mockStream simulates a bidirectional gRPC stream in-memory.
type mockStream struct {
	ctx        context.Context
	cancel     context.CancelFunc
	inboundCh  chan *agentv1.AgentMessage
	outboundCh chan *agentv1.ControlMessage
}

func newMockStream() *mockStream {
	ctx, cancel := context.WithCancel(context.Background())
	return &mockStream{
		ctx:        ctx,
		cancel:     cancel,
		inboundCh:  make(chan *agentv1.AgentMessage, 10),
		outboundCh: make(chan *agentv1.ControlMessage, 10),
	}
}

func (s *mockStream) Send(msg *agentv1.ControlMessage) error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.outboundCh <- msg:
		return nil
	}
}

func (s *mockStream) Recv() (*agentv1.AgentMessage, error) {
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case msg, ok := <-s.inboundCh:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	}
}

func (s *mockStream) SetHeader(_ metadata.MD) error  { return nil }
func (s *mockStream) SendHeader(_ metadata.MD) error { return nil }
func (s *mockStream) SetTrailer(_ metadata.MD)       {}
func (s *mockStream) Context() context.Context       { return s.ctx }
func (s *mockStream) SendMsg(_ interface{}) error    { return nil }
func (s *mockStream) RecvMsg(_ interface{}) error    { return nil }

func TestAgentServiceServer_FullStreamLifecycle(t *testing.T) {
	nodeRepo := newMockAgentNodeRepo()
	userRepo := newMockAgentUserRepo()
	credRepo := &mockAgentCredRepo{}
	trafficRepo := &mockAgentTrafficRepo{}

	sessionMgr := NewSessionManager()
	configBuilder := service.NewConfigBuilder(nodeRepo, credRepo, userRepo)
	agentServer := NewAgentServiceServer(nodeRepo, userRepo, credRepo, trafficRepo, configBuilder, sessionMgr)

	// Pre-seed a user and credential
	nodeID := uuid.New()
	nodeRepo.nodes[nodeID] = store.Node{
		ID:        nodeID,
		Name:      "test-node",
		Status:    pgtype.Text{String: "offline", Valid: true},
		PublicKey: "wg-pub-key-node",
	}

	userID := uuid.New()
	userRepo.users[userID] = store.User{
		ID:     userID,
		Status: pgtype.Text{String: "active", Valid: true},
	}

	ip := netip.MustParseAddr("10.8.0.5")
	credID := uuid.New()
	credRepo.creds = append(credRepo.creds, store.Credential{
		ID:        credID,
		UserID:    userID,
		NodeID:    nodeID,
		Protocol:  "wireguard",
		PublicKey: pgtype.Text{String: "peer-pub-key", Valid: true},
		Ipv4:      &ip,
		Status:    pgtype.Text{String: "active", Valid: true},
	})

	stream := newMockStream()

	// Run Connect handler in background
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- agentServer.Connect(stream)
	}()

	// 1. Agent registers
	stream.inboundCh <- &agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Register{
			Register: &agentv1.RegisterRequest{
				NodeName:           "test-node",
				WireguardPublicKey: "wg-pub-key-node",
				Version:            "v1.0.0",
			},
		},
	}

	// Expect full ConfigUpdate back
	select {
	case msg := <-stream.outboundCh:
		cfg := msg.GetConfig()
		require.NotNil(t, cfg)
		assert.Equal(t, int64(1), cfg.ConfigVersion)
		assert.True(t, cfg.IsFull)
		assert.Len(t, cfg.Users, 1)
		assert.Equal(t, userID.String(), cfg.Users[0].UserId)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for ConfigUpdate")
	}

	// Verify session registered and status online
	assert.Equal(t, 1, sessionMgr.Count())
	session, found := sessionMgr.Get(nodeID)
	require.True(t, found)
	assert.Equal(t, "online", nodeRepo.GetStatus(nodeID))

	// 2. Agent sends ConfigAck
	stream.inboundCh <- &agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_ConfigAck{
			ConfigAck: &agentv1.ConfigAck{
				ConfigVersion: 1,
				Success:       true,
			},
		},
	}
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, int64(1), session.GetConfigVersion())

	// 3. Agent sends Heartbeat
	stream.inboundCh <- &agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Heartbeat{
			Heartbeat: &agentv1.Heartbeat{
				Timestamp: time.Now().Unix(),
				Status:    agentv1.NodeStatus_NODE_STATUS_ONLINE,
			},
		},
	}
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, "online", nodeRepo.GetStatus(nodeID))

	// 4. Agent sends MetricsReport
	stream.inboundCh <- &agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Metrics{
			Metrics: &agentv1.MetricsReport{
				Timestamp: time.Now().Unix(),
				Protocols: []*agentv1.ProtocolMetrics{
					{
						Protocol: "wireguard",
						Peers: []*agentv1.PeerMetric{
							{
								PeerId:  credID.String(),
								RxBytes: 1000,
								TxBytes: 4000,
							},
						},
					},
				},
			},
		},
	}
	time.Sleep(20 * time.Millisecond)

	// Verify traffic accounting
	records := trafficRepo.GetRecords()
	require.Len(t, records, 1)
	assert.Equal(t, userID, records[0].UserID)
	assert.Equal(t, int64(1000), records[0].RxBytes.Int64)
	assert.Equal(t, int64(4000), records[0].TxBytes.Int64)
	assert.Equal(t, int64(5000), userRepo.GetTraffic(userID))

	// 5. Push delta config update from server
	err := agentServer.PushConfigUpdate(context.Background(), nodeID, 2, false)
	require.NoError(t, err)

	select {
	case msg := <-stream.outboundCh:
		cfg := msg.GetConfig()
		require.NotNil(t, cfg)
		assert.Equal(t, int64(2), cfg.ConfigVersion)
		assert.False(t, cfg.IsFull)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for PushConfigUpdate")
	}

	// 6. Close stream (simulate agent disconnect)
	close(stream.inboundCh)
	select {
	case err := <-serverErrCh:
		assert.NoError(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("server Connect did not terminate on stream close")
	}

	// Verify cleanup: session unregistered and node marked offline
	assert.Equal(t, 0, sessionMgr.Count())
	assert.Equal(t, "offline", nodeRepo.GetStatus(nodeID))
}
