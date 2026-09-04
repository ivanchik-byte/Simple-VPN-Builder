package manager

import (
	"context"
	"fmt"
	"net/netip"
	"testing"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestIPAllocator_AllocateAndRelease(t *testing.T) {
	v4Prefix := netip.MustParsePrefix("10.8.0.0/16")
	v6Prefix := netip.MustParsePrefix("fd00::/64")
	alloc := NewIPAllocator(v4Prefix, v6Prefix)

	// Allocate for peer 1
	v4, v6, err := alloc.Allocate("peer-1")
	require.NoError(t, err)
	assert.True(t, v4.IsValid())
	assert.True(t, v6.IsValid())
	assert.True(t, v4Prefix.Contains(v4))
	assert.True(t, v6Prefix.Contains(v6))

	// Calling Allocate again for same peer returns same IPs
	v4Same, v6Same, err := alloc.Allocate("peer-1")
	require.NoError(t, err)
	assert.Equal(t, v4, v4Same)
	assert.Equal(t, v6, v6Same)

	// Allocate for peer 2 must be different
	v4Other, v6Other, err := alloc.Allocate("peer-2")
	require.NoError(t, err)
	assert.NotEqual(t, v4, v4Other)
	assert.NotEqual(t, v6, v6Other)

	// Release peer 1
	alloc.Release("peer-1")

	// Peer 1 addresses are freed and can be reused
	assert.False(t, alloc.usedV4[v4])
	assert.False(t, alloc.usedV6[v6])
}

func TestIPAllocator_Exceeds256Addresses(t *testing.T) {
	v4Prefix := netip.MustParsePrefix("10.8.0.0/16")
	v6Prefix := netip.MustParsePrefix("fd00::/64")
	alloc := NewIPAllocator(v4Prefix, v6Prefix)

	allocated := make(map[string]bool)

	// Allocate 300 addresses without truncation collisions
	for i := 1; i <= 300; i++ {
		peerID := fmt.Sprintf("peer-%d", i)
		v4, _, err := alloc.Allocate(peerID)
		require.NoError(t, err, "Failed at index %d", i)

		ipStr := v4.String()
		assert.False(t, allocated[ipStr], "Duplicate IP detected at index %d: %s", i, ipStr)
		allocated[ipStr] = true
	}

	assert.Len(t, allocated, 300)
}

type mockDeviceClient struct {
	device   *wgtypes.Device
	lastCfg  wgtypes.Config
	devErr   error
	cfgErr   error
}

func (m *mockDeviceClient) Device(_ string) (*wgtypes.Device, error) {
	if m.devErr != nil {
		return nil, m.devErr
	}
	return m.device, nil
}

func (m *mockDeviceClient) ConfigureDevice(_ string, cfg wgtypes.Config) error {
	if m.cfgErr != nil {
		return m.cfgErr
	}
	m.lastCfg = cfg
	// Simulate applying config to device
	for _, p := range cfg.Peers {
		if p.Remove {
			var remaining []wgtypes.Peer
			for _, ep := range m.device.Peers {
				if ep.PublicKey != p.PublicKey {
					remaining = append(remaining, ep)
				}
			}
			m.device.Peers = remaining
		} else {
			m.device.Peers = append(m.device.Peers, wgtypes.Peer{
				PublicKey: p.PublicKey,
			})
		}
	}
	return nil
}

func (m *mockDeviceClient) Close() error {
	return nil
}

func TestWireGuardManager_SyncPeers(t *testing.T) {
	k1, _ := wgtypes.GenerateKey()
	k2, _ := wgtypes.GenerateKey()
	k3, _ := wgtypes.GenerateKey()

	initialDevice := &wgtypes.Device{
		Name: "wg0",
		Peers: []wgtypes.Peer{
			{PublicKey: k1},
			{PublicKey: k2},
		},
	}

	client := &mockDeviceClient{device: initialDevice}
	cfg := &config.WireGuardConfig{
		SubnetV4: "10.8.0.0/24",
		SubnetV6: "fd00::/64",
	}

	mgr, err := NewWireGuardManagerWithClient(cfg, client)
	require.NoError(t, err)

	mgr.interfaces["wg0"] = initialDevice

	// Desired: keep k1, remove k2, add k3
	desired := []DesiredPeer{
		{
			PeerID:     "user-1",
			PublicKey:  k1.String(),
			AllowedIPs: "10.8.0.2/32",
			Keepalive:  25,
		},
		{
			PeerID:     "user-3",
			PublicKey:  k3.String(),
			AllowedIPs: "10.8.0.4/32",
			Keepalive:  25,
		},
	}

	err = mgr.SyncPeers(context.Background(), "wg0", desired, true)
	require.NoError(t, err)

	// Check configured peers in client
	assert.Len(t, client.lastCfg.Peers, 3) // update k1, remove k2, add k3
	var removedK2 bool
	for _, p := range client.lastCfg.Peers {
		if p.PublicKey == k2 && p.Remove {
			removedK2 = true
		}
	}
	assert.True(t, removedK2)
}

func TestWireGuardManager_SyncPeers_Delta(t *testing.T) {
	k1, _ := wgtypes.GenerateKey()
	k2, _ := wgtypes.GenerateKey()
	k3, _ := wgtypes.GenerateKey()

	initialDevice := &wgtypes.Device{
		Name: "wg0",
		Peers: []wgtypes.Peer{
			{PublicKey: k1},
			{PublicKey: k2},
		},
	}

	client := &mockDeviceClient{device: initialDevice}
	cfg := &config.WireGuardConfig{
		SubnetV4: "10.8.0.0/24",
		SubnetV6: "fd00::/64",
	}

	mgr, err := NewWireGuardManagerWithClient(cfg, client)
	require.NoError(t, err)

	mgr.interfaces["wg0"] = initialDevice

	// Delta update with only k3 (isFull = false). k1 and k2 must NOT be removed!
	deltaDesired := []DesiredPeer{
		{
			PeerID:     "user-3",
			PublicKey:  k3.String(),
			AllowedIPs: "10.8.0.4/32",
			Keepalive:  25,
		},
	}

	err = mgr.SyncPeers(context.Background(), "wg0", deltaDesired, false)
	require.NoError(t, err)

	// In delta mode, only k3 should be configured, NO removals
	assert.Len(t, client.lastCfg.Peers, 1)
	assert.Equal(t, k3, client.lastCfg.Peers[0].PublicKey)
	assert.False(t, client.lastCfg.Peers[0].Remove)
}
