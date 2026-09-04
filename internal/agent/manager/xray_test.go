package manager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
)

type mockXrayApiClient struct {
	addedClients   []string
	removedClients []string
	queryStatsMock func(email string) (int64, int64)
}

func (m *mockXrayApiClient) AddClient(_ context.Context, _, email, _, _ string) error {
	m.addedClients = append(m.addedClients, email)
	return nil
}

func (m *mockXrayApiClient) RemoveClient(_ context.Context, _, email string) error {
	m.removedClients = append(m.removedClients, email)
	return nil
}

func (m *mockXrayApiClient) QueryUserStats(_ context.Context, email string, _ bool) (int64, int64, error) {
	if m.queryStatsMock != nil {
		rx, tx := m.queryStatsMock(email)
		return rx, tx, nil
	}
	return 1000, 2000, nil
}

func (m *mockXrayApiClient) QueryAllUserStats(_ context.Context, _ bool) (map[string][2]int64, error) {
	res := make(map[string][2]int64)
	for _, email := range m.addedClients {
		if m.queryStatsMock != nil {
			rx, tx := m.queryStatsMock(email)
			res[email] = [2]int64{rx, tx}
		} else {
			res[email] = [2]int64{1000, 2000}
		}
	}
	return res, nil
}

func (m *mockXrayApiClient) Close() error {
	return nil
}

func TestXrayManager_BuildDaemonConfig(t *testing.T) {
	cfg := &config.XrayConfig{
		APIPort:  10085,
		LogLevel: "warning",
	}

	mgr := NewXrayManager(cfg)
	privKey, pubKey, err := GenerateRealityKeypair()
	require.NoError(t, err)
	assert.NotEmpty(t, privKey)
	assert.NotEmpty(t, pubKey)

	shortID, err := GenerateShortID()
	require.NoError(t, err)
	assert.Len(t, shortID, 16)

	mgr.SetRealitySettings(RealityServerSettings{
		Dest:        "swdist.apple.com:443",
		ServerNames: []string{"swdist.apple.com"},
		PrivateKey:  privKey,
		PublicKey:   pubKey,
		ShortIDs:    []string{shortID},
	})

	data, err := mgr.BuildDaemonConfig(443)
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Contains(t, string(data), "vless-reality-in")
	assert.Contains(t, string(data), "swdist.apple.com")
	assert.Contains(t, string(data), "geoip:private")
	assert.Contains(t, string(data), "169.254.169.254/32")
	assert.Contains(t, string(data), "bittorrent")
}

func TestXrayManager_SyncClientsAndMetrics(t *testing.T) {
	cfg := &config.XrayConfig{APIPort: 10085}
	mgr := NewXrayManager(cfg)

	mockClient := &mockXrayApiClient{}
	mgr.SetAPIClient(mockClient)

	ctx := context.Background()

	// 1. Initial Sync (2 users)
	users := []VLESSClient{
		{CredentialID: "c1", UUID: "uuid-1", Email: "user1@vpn", Flow: "xtls-rprx-vision"},
		{CredentialID: "c2", UUID: "uuid-2", Email: "user2@vpn", Flow: "xtls-rprx-vision"},
	}
	err := mgr.SyncClients(ctx, users, true)
	require.NoError(t, err)
	assert.Len(t, mockClient.addedClients, 2)
	assert.Contains(t, mockClient.addedClients, "user1@vpn")
	assert.Contains(t, mockClient.addedClients, "user2@vpn")

	// 2. Metrics Collection
	metrics, err := mgr.GetMetrics(ctx)
	require.NoError(t, err)
	assert.Len(t, metrics, 2)
	assert.Equal(t, int64(1000), metrics[0].RXBytes)
	assert.Equal(t, int64(2000), metrics[0].TXBytes)

	// 3. Delta Sync (user 3 added, isFull = false)
	deltaUsers := []VLESSClient{
		{CredentialID: "c3", UUID: "uuid-3", Email: "user3@vpn", Flow: "xtls-rprx-vision"},
	}
	err = mgr.SyncClients(ctx, deltaUsers, false)
	require.NoError(t, err)
	assert.Len(t, mockClient.removedClients, 0) // No removals in delta mode
	assert.Contains(t, mockClient.addedClients, "user3@vpn")

	// 4. Full Sync with Prune (keep only user 1)
	fullUsers := []VLESSClient{
		{CredentialID: "c1", UUID: "uuid-1", Email: "user1@vpn", Flow: "xtls-rprx-vision"},
	}
	err = mgr.SyncClients(ctx, fullUsers, true)
	require.NoError(t, err)
	assert.Contains(t, mockClient.removedClients, "user2@vpn")
	assert.Contains(t, mockClient.removedClients, "user3@vpn")

	// 5. Stop lifecycle
	err = mgr.Stop(ctx)
	assert.NoError(t, err)
}
