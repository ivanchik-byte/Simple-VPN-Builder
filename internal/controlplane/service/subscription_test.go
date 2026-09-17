package service

import (
	"context"
	"encoding/base64"
	"net/netip"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

type subMockUserRepo struct {
	store.UserRepository
	user store.User
}

func (m *subMockUserRepo) GetBySubscriptionToken(_ context.Context, _ uuid.UUID) (store.User, error) {
	return m.user, nil
}

type subMockCredRepo struct {
	store.CredentialRepository
	creds []store.Credential
}

func (m *subMockCredRepo) ListByUser(_ context.Context, _ uuid.UUID) ([]store.Credential, error) {
	return m.creds, nil
}

type subMockNodeRepo struct {
	store.NodeRepository
	node store.Node
}

func (m *subMockNodeRepo) GetByID(_ context.Context, _ uuid.UUID) (store.Node, error) {
	return m.node, nil
}

func TestSubscriptionService_Formats(t *testing.T) {
	userID := uuid.New()
	token := uuid.New()
	nodeID := uuid.New()
	vlessUUID := uuid.New()

	uRepo := &subMockUserRepo{
		user: store.User{
			ID:                userID,
			SubscriptionToken: token,
			Status:            pgtype.Text{String: "active", Valid: true},
			TrafficLimit:      pgtype.Int8{Int64: 100 * 1024 * 1024 * 1024, Valid: true},
			TrafficUsed:       pgtype.Int8{Int64: 25 * 1024 * 1024 * 1024, Valid: true},
		},
	}

	nRepo := &subMockNodeRepo{
		node: store.Node{
			ID:        nodeID,
			Name:      "Frankfurt-Node-1",
			Endpoint:  "198.51.100.1",
			PublicKey: "node-pubkey",
		},
	}

	peerIP := netip.MustParseAddr("10.8.0.2")
	cRepo := &subMockCredRepo{
		creds: []store.Credential{
			{
				ID:        uuid.New(),
				UserID:    userID,
				NodeID:    nodeID,
				Protocol:  "vless",
				Uuid:      pgtype.UUID{Bytes: vlessUUID, Valid: true},
				Flow:      pgtype.Text{String: "xtls-rprx-vision", Valid: true},
				PublicKey: pgtype.Text{String: "reality-public-key", Valid: true},
				Status:    pgtype.Text{String: "active", Valid: true},
			},
			{
				ID:         uuid.New(),
				UserID:     userID,
				NodeID:     nodeID,
				Protocol:   "wireguard",
				PrivateKey: pgtype.Text{String: "client-privkey", Valid: true},
				Ipv4:       &peerIP,
				Status:     pgtype.Text{String: "active", Valid: true},
			},
		},
	}

	svc := NewSubscriptionService(uRepo, cRepo, nRepo)
	ctx := context.Background()

	// 1. Base64
	content, contentType, err := svc.GenerateSubscriptionContent(ctx, token, "base64")
	require.NoError(t, err)
	assert.Equal(t, "text/plain; charset=utf-8", contentType)

	decoded, err := base64.StdEncoding.DecodeString(string(content))
	require.NoError(t, err)
	decodedStr := string(decoded)
	assert.Contains(t, decodedStr, "vless://"+vlessUUID.String()+"@198.51.100.1:443")
	assert.Contains(t, decodedStr, "security=reality")
	assert.Contains(t, decodedStr, "flow=xtls-rprx-vision")
	assert.Contains(t, decodedStr, "wireguard://client-privkey@198.51.100.1:51820")

	// 2. Sing-box JSON
	content, contentType, err = svc.GenerateSubscriptionContent(ctx, token, "singbox")
	require.NoError(t, err)
	assert.Equal(t, "application/json", contentType)
	assert.True(t, strings.Contains(string(content), `"type": "vless"`))
	assert.True(t, strings.Contains(string(content), vlessUUID.String()))

	// 3. Clash Meta YAML
	content, contentType, err = svc.GenerateSubscriptionContent(ctx, token, "clash")
	require.NoError(t, err)
	assert.Equal(t, "application/x-yaml", contentType)
	assert.True(t, strings.Contains(string(content), "type: vless"))
	assert.True(t, strings.Contains(string(content), "reality-opts:"))

	// 4. WireGuard .conf
	content, contentType, err = svc.GenerateSubscriptionContent(ctx, token, "wireguard")
	require.NoError(t, err)
	assert.Equal(t, "text/plain; charset=utf-8", contentType)
	assert.Contains(t, string(content), "[Interface]")
	assert.Contains(t, string(content), "PrivateKey = client-privkey")
	assert.Contains(t, string(content), "Address = 10.8.0.2/32")
	assert.Contains(t, string(content), "[Peer]")
	assert.Contains(t, string(content), "PublicKey = node-pubkey")
	assert.Contains(t, string(content), "Endpoint = 198.51.100.1:51820")
	assert.False(t, strings.Contains(string(content), "Jc ="))

	// 5. Subscription Userinfo
	info, err := svc.GetUserSubscriptionInfo(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, int64(100*1024*1024*1024), info.TotalLimit)
	assert.Equal(t, int64(25*1024*1024*1024), info.DownloadBytes)
}

func TestSubscriptionService_AmneziaWG_And_IPv6(t *testing.T) {
	userID := uuid.New()
	token := uuid.New()
	nodeID := uuid.New()

	uRepo := &subMockUserRepo{
		user: store.User{
			ID:                userID,
			SubscriptionToken: token,
			Status:            pgtype.Text{String: "active", Valid: true},
			TrafficLimit:      pgtype.Int8{Int64: 50 * 1024 * 1024 * 1024, Valid: true},
			TrafficUsed:       pgtype.Int8{Int64: 5 * 1024 * 1024 * 1024, Valid: true},
		},
	}

	// Test bare IPv6 node endpoint
	nRepo := &subMockNodeRepo{
		node: store.Node{
			ID:        nodeID,
			Name:      "IPv6-Node",
			Endpoint:  "2001:db8::1",
			PublicKey: "ipv6-node-pubkey",
		},
	}

	peerIP := netip.MustParseAddr("10.8.0.5")
	cRepo := &subMockCredRepo{
		creds: []store.Credential{
			{
				ID:         uuid.New(),
				UserID:     userID,
				NodeID:     nodeID,
				Protocol:   "amneziawg",
				PrivateKey: pgtype.Text{String: "awg-privkey", Valid: true},
				Ipv4:       &peerIP,
				Status:     pgtype.Text{String: "active", Valid: true},
				AwgJc:      pgtype.Int4{Int32: 0, Valid: true}, // Explicit zero check
				AwgJmin:    pgtype.Int4{Int32: 40, Valid: true},
				AwgJmax:    pgtype.Int4{Int32: 70, Valid: true},
				AwgS1:      pgtype.Int4{Int32: 64, Valid: true},
				AwgS2:      pgtype.Int4{Int32: 64, Valid: true},
				AwgH1:      pgtype.Int8{Int64: 16843009, Valid: true},
				AwgH2:      pgtype.Int8{Int64: 33686018, Valid: true},
				AwgH3:      pgtype.Int8{Int64: 50529027, Valid: true},
				AwgH4:      pgtype.Int8{Int64: 67372036, Valid: true},
			},
		},
	}

	svc := NewSubscriptionService(uRepo, cRepo, nRepo)
	ctx := context.Background()

	content, contentType, err := svc.GenerateSubscriptionContent(ctx, token, "amneziawg")
	require.NoError(t, err)
	assert.Equal(t, "text/plain; charset=utf-8", contentType)

	conf := string(content)
	assert.Contains(t, conf, "PrivateKey = awg-privkey")
	assert.Contains(t, conf, "Address = 10.8.0.5/32")
	// Verify IPv6 bracket formatting
	assert.Contains(t, conf, "Endpoint = [2001:db8::1]:51820")
	// Verify zero-value AWG parameter preservation
	assert.Contains(t, conf, "Jc = 0")
	assert.Contains(t, conf, "Jmin = 40")
	assert.Contains(t, conf, "H1 = 16843009")
}

func TestSubscriptionService_WireGuard_MissingIPv4_Fails(t *testing.T) {
	userID := uuid.New()
	token := uuid.New()
	nodeID := uuid.New()

	uRepo := &subMockUserRepo{
		user: store.User{
			ID:                userID,
			SubscriptionToken: token,
			Status:            pgtype.Text{String: "active", Valid: true},
		},
	}

	nRepo := &subMockNodeRepo{
		node: store.Node{
			ID:       nodeID,
			Name:     "Node-Without-IP",
			Endpoint: "198.51.100.1",
		},
	}

	cRepo := &subMockCredRepo{
		creds: []store.Credential{
			{
				ID:         uuid.New(),
				UserID:     userID,
				NodeID:     nodeID,
				Protocol:   "wireguard",
				PrivateKey: pgtype.Text{String: "client-privkey", Valid: true},
				Ipv4:       nil, // Missing IPv4!
				Status:     pgtype.Text{String: "active", Valid: true},
			},
		},
	}

	svc := NewSubscriptionService(uRepo, cRepo, nRepo)
	ctx := context.Background()

	_, _, err := svc.GenerateSubscriptionContent(ctx, token, "wireguard")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no allocated IPv4 address")
}

func TestSubscriptionService_MultiNodeWireGuardAndReality(t *testing.T) {
	userID := uuid.New()
	token := uuid.New()
	nodeAID := uuid.New()
	nodeBID := uuid.New()

	uRepo := &subMockUserRepo{
		user: store.User{
			ID:                userID,
			SubscriptionToken: token,
			Status:            pgtype.Text{String: "active", Valid: true},
		},
	}

	nodes := map[uuid.UUID]store.Node{
		nodeAID: {
			ID: nodeAID, Name: "Node-A", Endpoint: "198.51.100.1",
			PublicKey:  "node-a-pubkey",
			RealitySni: pgtype.Text{String: "sni-a.example.com", Valid: true},
			RealitySid: pgtype.Text{String: "aabbccdd", Valid: true},
			RealityPbk: pgtype.Text{String: "node-a-pbk", Valid: true},
		},
		nodeBID: {
			ID: nodeBID, Name: "Node-B", Endpoint: "198.51.100.2",
			PublicKey: "node-b-pubkey",
		},
	}
	nRepo := &subMockNodeMapRepo{nodes: nodes}

	ipA := netip.MustParseAddr("10.8.0.2")
	ipB := netip.MustParseAddr("10.8.0.3")
	vlessUUID := uuid.New()
	cRepo := &subMockCredRepo{
		creds: []store.Credential{
			{
				ID: uuid.New(), UserID: userID, NodeID: nodeAID,
				Protocol:   "wireguard",
				PrivateKey: pgtype.Text{String: "priv-a", Valid: true},
				Ipv4:       &ipA,
				Status:     pgtype.Text{String: "active", Valid: true},
			},
			{
				ID: uuid.New(), UserID: userID, NodeID: nodeBID,
				Protocol:   "wireguard",
				PrivateKey: pgtype.Text{String: "priv-a", Valid: true},
				Ipv4:       &ipB,
				Status:     pgtype.Text{String: "active", Valid: true},
			},
			{
				ID: uuid.New(), UserID: userID, NodeID: nodeAID,
				Protocol: "vless",
				Uuid:     pgtype.UUID{Bytes: vlessUUID, Valid: true},
				Status:   pgtype.Text{String: "active", Valid: true},
			},
		},
	}

	svc := NewSubscriptionService(uRepo, cRepo, nRepo)
	ctx := context.Background()

	content, _, err := svc.GenerateSubscriptionContent(ctx, token, "wireguard")
	require.NoError(t, err)
	conf := string(content)
	assert.Equal(t, 2, strings.Count(conf, "[Peer]"))
	assert.Contains(t, conf, "Endpoint = 198.51.100.1:51820")
	assert.Contains(t, conf, "Endpoint = 198.51.100.2:51820")

	content, _, err = svc.GenerateSubscriptionContent(ctx, token, "base64")
	require.NoError(t, err)
	decoded, err := base64.StdEncoding.DecodeString(string(content))
	require.NoError(t, err)
	bundle := string(decoded)
	assert.Contains(t, bundle, "sni=sni-a.example.com")
	assert.Contains(t, bundle, "sid=aabbccdd")
	assert.Contains(t, bundle, "pbk=node-a-pbk")
}

type subMockNodeMapRepo struct {
	store.NodeRepository
	nodes map[uuid.UUID]store.Node
}

func (m *subMockNodeMapRepo) GetByID(_ context.Context, id uuid.UUID) (store.Node, error) {
	if n, ok := m.nodes[id]; ok {
		return n, nil
	}
	return store.Node{}, assert.AnError
}
