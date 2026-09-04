package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/manager"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/syncer"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/handler"
	cpgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

type mockXrayApiClient struct {
	mu      sync.Mutex
	clients map[string]string // email -> uuid
	stats   map[string][2]int64
}

func newMockXrayApiClient() *mockXrayApiClient {
	return &mockXrayApiClient{
		clients: make(map[string]string),
		stats:   make(map[string][2]int64),
	}
}

func (m *mockXrayApiClient) AddClient(_ context.Context, _ string, email, uuid, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[email] = uuid
	return nil
}

func (m *mockXrayApiClient) RemoveClient(_ context.Context, _ string, email string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clients, email)
	return nil
}

func (m *mockXrayApiClient) QueryUserStats(_ context.Context, email string, _ bool) (int64, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.stats[email]
	if !ok {
		return 0, 0, nil
	}
	return st[0], st[1], nil
}

func (m *mockXrayApiClient) QueryAllUserStats(_ context.Context, _ bool) (map[string][2]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make(map[string][2]int64)
	for k, v := range m.stats {
		res[k] = v
	}
	return res, nil
}

func (m *mockXrayApiClient) Close() error {
	return nil
}

type e2eSubUserRepo struct {
	store.UserRepository
	users map[uuid.UUID]store.User
}

func (r *e2eSubUserRepo) GetBySubscriptionToken(_ context.Context, token uuid.UUID) (store.User, error) {
	for _, u := range r.users {
		if u.SubscriptionToken == token {
			return u, nil
		}
	}
	return store.User{}, assert.AnError
}

func (r *e2eSubUserRepo) GetByID(_ context.Context, id uuid.UUID) (store.User, error) {
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return store.User{}, assert.AnError
}

func (r *e2eSubUserRepo) UpdateTraffic(_ context.Context, _ uuid.UUID, _ int64) error {
	return nil
}

type e2eSubCredRepo struct {
	store.CredentialRepository
	creds []store.Credential
}

func (r *e2eSubCredRepo) ListByUser(_ context.Context, userID uuid.UUID) ([]store.Credential, error) {
	var res []store.Credential
	for _, c := range r.creds {
		if c.UserID == userID {
			res = append(res, c)
		}
	}
	return res, nil
}

func (r *e2eSubCredRepo) ListActiveByUser(_ context.Context, userID uuid.UUID) ([]store.Credential, error) {
	return r.ListByUser(context.Background(), userID)
}

func (r *e2eSubCredRepo) ListActiveByNode(_ context.Context, nodeID uuid.UUID) ([]store.Credential, error) {
	var res []store.Credential
	for _, c := range r.creds {
		if c.NodeID == nodeID {
			res = append(res, c)
		}
	}
	return res, nil
}

type e2eSubNodeRepo struct {
	store.NodeRepository
	nodes map[uuid.UUID]store.Node
}

func (r *e2eSubNodeRepo) GetByID(_ context.Context, id uuid.UUID) (store.Node, error) {
	if n, ok := r.nodes[id]; ok {
		return n, nil
	}
	return store.Node{}, assert.AnError
}

func (r *e2eSubNodeRepo) GetByName(_ context.Context, name string) (store.Node, error) {
	for _, n := range r.nodes {
		if n.Name == name {
			return n, nil
		}
	}
	return store.Node{}, assert.AnError
}

func (r *e2eSubNodeRepo) UpdateHeartbeat(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

func TestE2E_XrayVLESSRealitySyncAndSubscription(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Setup Data
	nodeID := uuid.New()
	userID := uuid.New()
	clientUUID := uuid.New()
	subToken := uuid.New()

	node := store.Node{
		ID:        nodeID,
		Name:      "vless-exit-node",
		Status:    pgtype.Text{String: "online", Valid: true},
		Endpoint:  "vpn.example.com:443",
		PublicKey: "mock-wg-key",
	}

	user := store.User{
		ID:                userID,
		Username:          "alice",
		Status:            pgtype.Text{String: "active", Valid: true},
		SubscriptionToken: subToken,
		TrafficLimit:      pgtype.Int8{Int64: 100 * 1024 * 1024 * 1024, Valid: true}, // 100GB
		TrafficUsed:       pgtype.Int8{Int64: 5 * 1024 * 1024 * 1024, Valid: true},   // 5GB
		ExpiresAt:         pgtype.Timestamptz{Time: time.Now().Add(30 * 24 * time.Hour), Valid: true},
	}

	peerIP := netip.MustParseAddr("10.200.0.5")
	vlessCred := store.Credential{
		ID:       uuid.New(),
		UserID:   userID,
		NodeID:   nodeID,
		Protocol: "vless",
		Uuid:     pgtype.UUID{Bytes: clientUUID, Valid: true},
		Ipv4:     &peerIP,
		Status:   pgtype.Text{String: "active", Valid: true},
	}

	userRepo := &e2eSubUserRepo{users: map[uuid.UUID]store.User{userID: user}}
	nodeRepo := &e2eSubNodeRepo{nodes: map[uuid.UUID]store.Node{nodeID: node}}
	credRepo := &e2eSubCredRepo{creds: []store.Credential{vlessCred}}
	trafficRepo := &e2eGRPCTrafficRepo{}

	// 2. Setup Subscription Service & HTTP Endpoint
	subService := service.NewSubscriptionService(userRepo, credRepo, nodeRepo)
	subHandler := handler.NewSubscriptionHandler(subService)

	r := chi.NewRouter()
	r.Get("/sub/{token}", subHandler.GetSubscription)
	ts := httptest.NewServer(r)
	defer ts.Close()

	// 3. Test Subscription Delivery
	req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/sub/"+subToken.String(), nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "v2rayNG/1.8.5")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("Subscription-Userinfo"))
	assert.Contains(t, resp.Header.Get("Subscription-Userinfo"), "total=107374182400")

	// Read and decode body (MIN-06)
	rawBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	bodyStr := strings.TrimSpace(string(rawBody))

	decoded, err := base64.StdEncoding.DecodeString(bodyStr)
	require.NoError(t, err)
	vlessURI := string(decoded)
	assert.Contains(t, vlessURI, "vless://"+clientUUID.String()+"@vpn.example.com:443")
	assert.Contains(t, vlessURI, "security=reality")
	assert.Contains(t, vlessURI, "flow=xtls-rprx-vision")

	// 4. Setup Control Plane gRPC Server
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serverAddr := lis.Addr().String()

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

	// 5. Setup Agent with Mock Xray API Client
	mockAPI := newMockXrayApiClient()
	xrayMgr := manager.NewXrayManager(&config.XrayConfig{APIPort: 10085})
	xrayMgr.SetAPIClient(mockAPI)

	agentCfg := &config.Config{
		Agent: config.AgentConfig{
			NodeName:     "vless-exit-node",
			ControlPlane: serverAddr,
			SyncInterval: 50 * time.Millisecond,
			WireGuard: config.WireGuardConfig{
				InterfacePrefix: "wg",
			},
		},
	}

	agentClient := agentgrpc.NewClient(agentCfg)
	agentClient.SetPublicKey("mock-wg-key")

	configSyncer := syncer.New(agentClient, nil, xrayMgr, agentCfg)
	agentClient.SetHandlers(configSyncer, nil)
	go configSyncer.Run(ctx)

	err = agentClient.Connect(ctx)
	require.NoError(t, err)
	defer agentClient.Close()

	// Wait for session
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

	// 6. Push VLESS Credential Update via gRPC
	vlessPayload, err := json.Marshal(service.VLESSCredentialPayload{
		UUID:  clientUUID.String(),
		Flow:  "xtls-rprx-vision",
		Email: "alice@vpn.example.com",
	})
	require.NoError(t, err)

	vlessCredConfig := &agentv1.CredentialConfig{
		CredentialId: vlessCred.ID.String(),
		Protocol:     "vless",
		ConfigBytes:  vlessPayload,
	}

	userConfig := &agentv1.NodeUserConfig{
		UserId:      userID.String(),
		Credentials: []*agentv1.CredentialConfig{vlessCredConfig},
	}

	update := &agentv1.ConfigUpdate{
		ConfigVersion: 100,
		IsFull:        true,
		Users:         []*agentv1.NodeUserConfig{userConfig},
	}

	err = sess.Send(&agentv1.ControlMessage{
		Payload: &agentv1.ControlMessage_Config{
			Config: update,
		},
	})
	require.NoError(t, err)

	// Verify XrayManager received dynamic client addition without crash
	assert.Eventually(t, func() bool {
		mockAPI.mu.Lock()
		defer mockAPI.mu.Unlock()
		_, exists := mockAPI.clients["alice@vpn.example.com"]
		return exists
	}, 3*time.Second, 50*time.Millisecond)

	mockAPI.mu.Lock()
	assert.Equal(t, clientUUID.String(), mockAPI.clients["alice@vpn.example.com"])
	mockAPI.mu.Unlock()

	// 7. Verify Xray Metrics Collection
	mockAPI.mu.Lock()
	mockAPI.stats["alice@vpn.example.com"] = [2]int64{1048576, 5242880} // 1MB RX, 5MB TX
	mockAPI.mu.Unlock()

	metricsList, err := xrayMgr.GetMetrics(ctx)
	require.NoError(t, err)
	require.Len(t, metricsList, 1)
	assert.Equal(t, int64(1048576), metricsList[0].RXBytes)
	assert.Equal(t, int64(5242880), metricsList[0].TXBytes)
	assert.True(t, metricsList[0].IsOnline)
}
