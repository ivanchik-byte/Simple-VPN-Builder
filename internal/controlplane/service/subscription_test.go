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
			Endpoint: "198.51.100.1",
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
