package service

import (
	"context"
	"encoding/json"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

type mockNodeRepo struct {
	store.NodeRepository
	node store.Node
	err  error
}

func (m *mockNodeRepo) GetByID(_ context.Context, _ uuid.UUID) (store.Node, error) {
	if m.err != nil {
		return store.Node{}, m.err
	}
	return m.node, nil
}

type mockCredRepo struct {
	store.CredentialRepository
	creds []store.Credential
	err   error
}

func (m *mockCredRepo) ListActiveByNode(_ context.Context, _ uuid.UUID) ([]store.Credential, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.creds, nil
}

type mockUserRepo struct {
	store.UserRepository
	users map[uuid.UUID]store.User
	err   error
}

func (m *mockUserRepo) GetByID(_ context.Context, id uuid.UUID) (store.User, error) {
	if m.err != nil {
		return store.User{}, m.err
	}
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return store.User{}, assert.AnError
}

func (m *mockUserRepo) GetByIDs(_ context.Context, ids []uuid.UUID) ([]store.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	result := make([]store.User, 0, len(ids))
	for _, id := range ids {
		if u, ok := m.users[id]; ok {
			result = append(result, u)
		}
	}
	return result, nil
}

func TestConfigBuilder_BuildConfig_Success(t *testing.T) {
	nodeID := uuid.New()
	userID1 := uuid.New()
	userID2 := uuid.New()

	nodeRepo := &mockNodeRepo{
		node: store.Node{
			ID:           nodeID,
			Name:         "nl-ams-01",
			Status:       pgtype.Text{String: "online", Valid: true},
			CapacityGbps: pgtype.Int4{Int32: 10, Valid: true},
		},
	}

	ip1 := netip.MustParseAddr("10.8.0.2")
	ip2 := netip.MustParseAddr("10.8.0.3")

	credRepo := &mockCredRepo{
		creds: []store.Credential{
			{
				ID:        uuid.New(),
				UserID:    userID1,
				NodeID:    nodeID,
				Protocol:  "wireguard",
				PublicKey: pgtype.Text{String: "wg-pub-key-1", Valid: true},
				Ipv4:      &ip1,
				Status:    pgtype.Text{String: "active", Valid: true},
			},
			{
				ID:        uuid.New(),
				UserID:    userID2,
				NodeID:    nodeID,
				Protocol:  "amneziawg",
				PublicKey: pgtype.Text{String: "awg-pub-key-2", Valid: true},
				Ipv4:      &ip2,
				Status:    pgtype.Text{String: "active", Valid: true},
				AwgJc:     pgtype.Int4{Int32: 5, Valid: true},
				AwgJmin:   pgtype.Int4{Int32: 50, Valid: true},
				AwgJmax:   pgtype.Int4{Int32: 80, Valid: true},
				AwgS1:     pgtype.Int4{Int32: 64, Valid: true},
				AwgS2:     pgtype.Int4{Int32: 64, Valid: true},
				AwgH1:     pgtype.Int8{Int64: 16843009, Valid: true},
			},
		},
	}

	userRepo := &mockUserRepo{
		users: map[uuid.UUID]store.User{
			userID1: {
				ID:        userID1,
				Username:  "user1",
				Status:    pgtype.Text{String: "active", Valid: true},
				CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			},
			userID2: {
				ID:        userID2,
				Username:  "user2",
				Status:    pgtype.Text{String: "active", Valid: true},
				CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			},
		},
	}

	builder := NewConfigBuilder(nodeRepo, credRepo, userRepo)
	cfgUpdate, err := builder.BuildConfig(context.Background(), nodeID, 42, true)
	require.NoError(t, err)
	require.NotNil(t, cfgUpdate)

	assert.Equal(t, int64(42), cfgUpdate.ConfigVersion)
	assert.True(t, cfgUpdate.IsFull)
	assert.Equal(t, "wg0", cfgUpdate.NodeConfig.WireguardInterface)
	assert.Len(t, cfgUpdate.Users, 2)

	// Verify AmneziaWG deserialization
	var foundAwg bool
	for _, u := range cfgUpdate.Users {
		for _, c := range u.Credentials {
			if c.Protocol == "amneziawg" {
				foundAwg = true
				var p WireGuardCredentialPayload
				err = json.Unmarshal(c.ConfigBytes, &p)
				require.NoError(t, err)
				assert.Equal(t, "awg-pub-key-2", p.PublicKey)
				assert.Equal(t, int32(5), p.AwgJc)
				assert.Equal(t, int32(50), p.AwgJmin)
				assert.Equal(t, int32(80), p.AwgJmax)
				assert.Equal(t, "10.8.0.3/32", p.AllowedIPs)
			}
		}
	}
	assert.True(t, foundAwg)
}

func TestConfigBuilder_BuildConfig_FiltersInactiveUsers(t *testing.T) {
	nodeID := uuid.New()
	activeUserID := uuid.New()
	suspendedUserID := uuid.New()

	nodeRepo := &mockNodeRepo{
		node: store.Node{ID: nodeID, Name: "node-1"},
	}

	credRepo := &mockCredRepo{
		creds: []store.Credential{
			{
				ID:        uuid.New(),
				UserID:    activeUserID,
				NodeID:    nodeID,
				Protocol:  "wireguard",
				PublicKey: pgtype.Text{String: "active-key", Valid: true},
			},
			{
				ID:        uuid.New(),
				UserID:    suspendedUserID,
				NodeID:    nodeID,
				Protocol:  "wireguard",
				PublicKey: pgtype.Text{String: "suspended-key", Valid: true},
			},
		},
	}

	userRepo := &mockUserRepo{
		users: map[uuid.UUID]store.User{
			activeUserID: {
				ID:     activeUserID,
				Status: pgtype.Text{String: "active", Valid: true},
			},
			suspendedUserID: {
				ID:     suspendedUserID,
				Status: pgtype.Text{String: "suspended", Valid: true},
			},
		},
	}

	builder := NewConfigBuilder(nodeRepo, credRepo, userRepo)
	cfgUpdate, err := builder.BuildConfig(context.Background(), nodeID, 1, false)
	require.NoError(t, err)
	require.NotNil(t, cfgUpdate)

	assert.Len(t, cfgUpdate.Users, 1)
	assert.Equal(t, activeUserID.String(), cfgUpdate.Users[0].UserId)
}
