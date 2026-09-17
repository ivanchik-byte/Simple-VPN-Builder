package manager

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

// WireGuardDeviceClient abstracts low-level wgctrl device configuration.
type WireGuardDeviceClient interface {
	Device(name string) (*wgtypes.Device, error)
	ConfigureDevice(name string, cfg wgtypes.Config) error
	Close() error
}

// DesiredPeer holds target configuration for a WireGuard peer.
type DesiredPeer struct {
	PeerID       string
	PublicKey    string
	PresharedKey string
	AllowedIPs   string
	Keepalive    int
}

type WireGuardManager struct {
	config      *config.WireGuardConfig
	client      WireGuardDeviceClient
	interfaces  map[string]*wgtypes.Device
	peerIPs     map[string]netip.Addr
	peerCredIDs map[wgtypes.Key]string
	mu          sync.RWMutex
	ipAllocator *IPAllocator
}

type IPAllocator struct {
	networkV4 netip.Prefix
	networkV6 netip.Prefix
	peerToV4  map[string]netip.Addr
	peerToV6  map[string]netip.Addr
	usedV4    map[netip.Addr]bool
	usedV6    map[netip.Addr]bool
	nextV4    uint32
	nextV6    uint64
	mu        sync.Mutex
}

func NewIPAllocator(v4, v6 netip.Prefix) *IPAllocator {
	return &IPAllocator{
		networkV4: v4,
		networkV6: v6,
		peerToV4:  make(map[string]netip.Addr),
		peerToV6:  make(map[string]netip.Addr),
		usedV4:    make(map[netip.Addr]bool),
		usedV6:    make(map[netip.Addr]bool),
		nextV4:    2,
		nextV6:    2,
	}
}

func (a *IPAllocator) Allocate(peerID string) (netip.Addr, netip.Addr, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if v4, ok := a.peerToV4[peerID]; ok {
		return v4, a.peerToV6[peerID], nil
	}

	v4, err := a.allocateV4()
	if err != nil {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("allocate v4: %w", err)
	}

	v6, err := a.allocateV6()
	if err != nil {
		a.releaseV4(v4)
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("allocate v6: %w", err)
	}

	a.peerToV4[peerID] = v4
	a.peerToV6[peerID] = v6
	a.usedV4[v4] = true
	a.usedV6[v6] = true

	return v4, v6, nil
}

func (a *IPAllocator) allocateV4() (netip.Addr, error) {
	base := a.networkV4.Addr().As4()
	baseInt := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
	mask := uint32(0xFFFFFFFF) << (32 - a.networkV4.Bits())
	maxHosts := ^mask

	for i := uint32(0); i < maxHosts-2; i++ {
		candidate := baseInt + a.nextV4
		a.nextV4++
		if a.nextV4 >= maxHosts-1 {
			a.nextV4 = 2
		}

		ip := netip.AddrFrom4([4]byte{
			byte(candidate >> 24),
			byte(candidate >> 16),
			byte(candidate >> 8),
			byte(candidate),
		})

		if !a.usedV4[ip] {
			return ip, nil
		}
	}

	return netip.Addr{}, fmt.Errorf("IPv4 subnet exhausted")
}

func (a *IPAllocator) allocateV6() (netip.Addr, error) {
	base := a.networkV6.Addr().As16()
	candidate := base
	idx := a.nextV6
	a.nextV6++

	for i := 15; i >= 8; i-- {
		candidate[i] = byte(idx)
		idx >>= 8
	}

	ip := netip.AddrFrom16(candidate)
	if a.usedV6[ip] {
		return netip.Addr{}, fmt.Errorf("IPv6 address collision")
	}

	return ip, nil
}

func (a *IPAllocator) releaseV4(ip netip.Addr) {
	delete(a.usedV4, ip)
}

func (a *IPAllocator) Release(peerID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if v4, ok := a.peerToV4[peerID]; ok {
		delete(a.usedV4, v4)
		delete(a.peerToV4, peerID)
	}
	if v6, ok := a.peerToV6[peerID]; ok {
		delete(a.usedV6, v6)
		delete(a.peerToV6, peerID)
	}
}

func NewWireGuardManager(cfg *config.WireGuardConfig) (*WireGuardManager, error) {
	client, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("init wgctrl: %w", err)
	}
	return NewWireGuardManagerWithClient(cfg, client)
}

func NewWireGuardManagerWithClient(cfg *config.WireGuardConfig, client WireGuardDeviceClient) (*WireGuardManager, error) {
	v4Prefix, err := netip.ParsePrefix(cfg.SubnetV4)
	if err != nil {
		return nil, fmt.Errorf("parse subnet_v4: %w", err)
	}

	v6Prefix, err := netip.ParsePrefix(cfg.SubnetV6)
	if err != nil {
		return nil, fmt.Errorf("parse subnet_v6: %w", err)
	}

	return &WireGuardManager{
		config:      cfg,
		client:      client,
		interfaces:  make(map[string]*wgtypes.Device),
		peerIPs:     make(map[string]netip.Addr),
		peerCredIDs: make(map[wgtypes.Key]string),
		ipAllocator: NewIPAllocator(v4Prefix, v6Prefix),
	}, nil
}

// LoadOrGeneratePrivateKey reads a private key from path or generates and saves one.
func LoadOrGeneratePrivateKey(keyPath string) (wgtypes.Key, error) {
	if keyPath != "" {
		if data, err := os.ReadFile(keyPath); err == nil {
			trimmed := strings.TrimSpace(string(data))
			key, err := wgtypes.ParseKey(trimmed)
			if err == nil {
				return key, nil
			}
		}
	}

	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("generate private key: %w", err)
	}

	if keyPath != "" {
		if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err == nil {
			_ = os.WriteFile(keyPath, []byte(key.String()+"\n"), 0o600)
		}
	}

	return key, nil
}

// EnsureInterfaceWithKey creates or configures a WireGuard link with the specified private key and listen port.
func (m *WireGuardManager) EnsureInterfaceWithKey(ctx context.Context, name string, privKey wgtypes.Key, listenPort int) (*wgtypes.Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if dev, exists := m.interfaces[name]; exists {
		return dev, nil
	}

	if name == "" {
		name = fmt.Sprintf("%s%d", m.config.InterfacePrefix, len(m.interfaces))
	}
	logger.InfoContext(ctx, "creating or ensuring WireGuard interface", "name", name)

	la := netlink.NewLinkAttrs()
	la.Name = name
	if m.config.MTU > 0 {
		la.MTU = m.config.MTU
	} else {
		la.MTU = 1420
	}

	wgLink := &netlink.GenericLink{
		LinkAttrs: la,
		LinkType:  "wireguard",
	}

	// LinkAdd may fail if device already exists, which is acceptable
	if err := netlink.LinkAdd(wgLink); err != nil && !strings.Contains(err.Error(), "file exists") {
		return nil, fmt.Errorf("create interface link: %w", err)
	}

	existingLink, err := netlink.LinkByName(name)
	if err == nil {
		_ = netlink.LinkSetUp(existingLink)

		// Assign Gateway IPv4 if available
		if m.config.SubnetV4 != "" {
			if prefix, err := netip.ParsePrefix(m.config.SubnetV4); err == nil {
				base := prefix.Addr().As4()
				gwV4 := fmt.Sprintf("%d.%d.%d.1/%d", base[0], base[1], base[2], prefix.Bits())
				if addrV4, err := netlink.ParseAddr(gwV4); err == nil {
					_ = netlink.AddrAdd(existingLink, addrV4)
				}
			}
		}
	}

	if listenPort <= 0 {
		listenPort = 51820
	}

	cfg := wgtypes.Config{
		PrivateKey:   &privKey,
		ListenPort:   &listenPort,
		ReplacePeers: false,
	}
	if err := m.client.ConfigureDevice(name, cfg); err != nil {
		return nil, fmt.Errorf("configure device with key/port: %w", err)
	}

	dev, err := m.client.Device(name)
	if err != nil {
		return nil, fmt.Errorf("get device: %w", err)
	}

	m.interfaces[name] = dev
	logger.InfoContext(ctx, "WireGuard interface ready", "name", name, "public_key", dev.PublicKey.String(), "listen_port", dev.ListenPort)
	return dev, nil
}

func (m *WireGuardManager) EnsureInterface(ctx context.Context, name string) error {
	// Reuse the existing kernel device (and its private key) when present,
	// e.g. after an agent restart with a persisted wg interface. Generating
	// a fresh key here would invalidate the server public key stored in the
	// control plane and drop all peers until re-provisioning.
	if dev, err := m.client.Device(name); err == nil {
		m.mu.Lock()
		m.interfaces[name] = dev
		m.mu.Unlock()
		return nil
	}
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return err
	}
	_, err = m.EnsureInterfaceWithKey(ctx, name, key, 51820)
	return err
}

func (m *WireGuardManager) GetInterfaceConfig(name string) (*wgtypes.Device, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dev, ok := m.interfaces[name]
	if !ok {
		return nil, fmt.Errorf("interface not found: %s", name)
	}
	return dev, nil
}

func (m *WireGuardManager) ListInterfaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.interfaces))
	for name := range m.interfaces {
		names = append(names, name)
	}
	return names
}

func (m *WireGuardManager) AddPeer(ctx context.Context, iface, peerID, publicKey, presharedKey, allowedIPs string, keepalive int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.interfaces[iface]
	if !ok {
		return fmt.Errorf("interface not found: %s", iface)
	}

	pubKey, err := wgtypes.ParseKey(publicKey)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	var psk *wgtypes.Key
	if presharedKey != "" {
		k, err := wgtypes.ParseKey(presharedKey)
		if err != nil {
			return fmt.Errorf("parse preshared key: %w", err)
		}
		psk = &k
	}

	var allowed []net.IPNet
	if allowedIPs != "" {
		for _, cidr := range strings.Split(allowedIPs, ",") {
			cidr = strings.TrimSpace(cidr)
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				return fmt.Errorf("parse allowed ip %s: %w", cidr, err)
			}
			allowed = append(allowed, *ipNet)
		}
	} else {
		v4, v6, err := m.ipAllocator.Allocate(peerID)
		if err != nil {
			return fmt.Errorf("allocate ip: %w", err)
		}
		m.peerIPs[peerID] = v4

		allowed = []net.IPNet{
			{IP: v4.AsSlice(), Mask: net.CIDRMask(32, 32)},
			{IP: v6.AsSlice(), Mask: net.CIDRMask(128, 128)},
		}
	}

	keepaliveInterval := time.Duration(keepalive) * time.Second
	peerCfg := wgtypes.PeerConfig{
		PublicKey:                   pubKey,
		PresharedKey:                psk,
		AllowedIPs:                  allowed,
		PersistentKeepaliveInterval: &keepaliveInterval,
		ReplaceAllowedIPs:           true,
	}

	cfg := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peerCfg},
	}

	if err := m.client.ConfigureDevice(iface, cfg); err != nil {
		return fmt.Errorf("configure device: %w", err)
	}

	dev, err := m.client.Device(iface)
	if err != nil {
		return fmt.Errorf("get device: %w", err)
	}
	m.interfaces[iface] = dev

	logger.InfoContext(ctx, "Added WireGuard peer", "iface", iface, "peer_id", peerID, "public_key", publicKey)
	return nil
}

func (m *WireGuardManager) RemovePeer(ctx context.Context, iface, peerID, publicKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.interfaces[iface]
	if !ok {
		return fmt.Errorf("interface not found: %s", iface)
	}

	pubKey, err := wgtypes.ParseKey(publicKey)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	peerCfg := wgtypes.PeerConfig{
		PublicKey: pubKey,
		Remove:    true,
	}

	cfg := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peerCfg},
	}

	if err := m.client.ConfigureDevice(iface, cfg); err != nil {
		return fmt.Errorf("configure device: %w", err)
	}

	m.ipAllocator.Release(peerID)
	delete(m.peerIPs, peerID)

	dev, err := m.client.Device(iface)
	if err != nil {
		return fmt.Errorf("get device: %w", err)
	}
	m.interfaces[iface] = dev

	logger.InfoContext(ctx, "Removed WireGuard peer", "iface", iface, "peer_id", peerID)
	return nil
}

// SyncPeers performs zero-downtime atomic batch synchronization of peers.
// If isFull is true, peers missing from desired are pruned (Remove: true).
// If isFull is false (delta update), only additions and modifications are applied without purging existing peers.
func (m *WireGuardManager) SyncPeers(ctx context.Context, iface string, desired []DesiredPeer, isFull bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dev, ok := m.interfaces[iface]
	if !ok {
		return fmt.Errorf("interface not found: %s", iface)
	}

	existingPeers := make(map[wgtypes.Key]*wgtypes.Peer)
	for i := range dev.Peers {
		p := &dev.Peers[i]
		existingPeers[p.PublicKey] = p
	}

	var peerConfigs []wgtypes.PeerConfig
	desiredKeys := make(map[wgtypes.Key]bool)

	for _, d := range desired {
		pubKey, err := wgtypes.ParseKey(d.PublicKey)
		if err != nil {
			logger.WarnContext(ctx, "invalid peer public key", "peer_id", d.PeerID, "error", err)
			continue
		}
		desiredKeys[pubKey] = true
		if d.PeerID != "" {
			m.peerCredIDs[pubKey] = d.PeerID
		}

		var psk *wgtypes.Key
		if d.PresharedKey != "" {
			k, err := wgtypes.ParseKey(d.PresharedKey)
			if err == nil {
				psk = &k
			}
		}

		var allowed []net.IPNet
		for _, cidr := range strings.Split(d.AllowedIPs, ",") {
			cidr = strings.TrimSpace(cidr)
			if cidr == "" {
				continue
			}
			if _, ipNet, err := net.ParseCIDR(cidr); err == nil {
				allowed = append(allowed, *ipNet)
			}
		}

		keepaliveInterval := time.Duration(d.Keepalive) * time.Second

		pCfg := wgtypes.PeerConfig{
			PublicKey:                   pubKey,
			PresharedKey:                psk,
			AllowedIPs:                  allowed,
			PersistentKeepaliveInterval: &keepaliveInterval,
			ReplaceAllowedIPs:           true,
			// IMPORTANT: Endpoint is nil to retain kernel-learned roaming endpoints
		}
		peerConfigs = append(peerConfigs, pCfg)
	}

	// Purge peers ONLY if this is a full synchronization
	if isFull {
		for key := range existingPeers {
			if !desiredKeys[key] {
				peerConfigs = append(peerConfigs, wgtypes.PeerConfig{
					PublicKey: key,
					Remove:    true,
				})
				delete(m.peerCredIDs, key)
			}
		}
	}

	if len(peerConfigs) == 0 {
		return nil
	}

	cfg := wgtypes.Config{
		Peers:        peerConfigs,
		ReplacePeers: false,
	}

	if err := m.client.ConfigureDevice(iface, cfg); err != nil {
		return fmt.Errorf("batch configure device peers: %w", err)
	}

	updated, err := m.client.Device(iface)
	if err != nil {
		return fmt.Errorf("get updated device: %w", err)
	}
	m.interfaces[iface] = updated

	logger.InfoContext(ctx, "batch synchronized WireGuard peers",
		"iface", iface, "configured_peers", len(peerConfigs), "is_full", isFull)
	return nil
}

func (m *WireGuardManager) GetMetrics(ctx context.Context) ([]PeerMetric, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var metrics []PeerMetric

	for iface := range m.interfaces {
		dev, err := m.client.Device(iface)
		if err != nil {
			logger.ErrorContext(ctx, "failed to get device for metrics", "iface", iface, "error", err)
			continue
		}

		for _, p := range dev.Peers {
			var endpoint string
			if p.Endpoint != nil {
				endpoint = p.Endpoint.String()
			}

			isOnline := time.Since(p.LastHandshakeTime) < 3*time.Minute && !p.LastHandshakeTime.IsZero()

			peerID := p.PublicKey.String()
			if credID, ok := m.peerCredIDs[p.PublicKey]; ok && credID != "" {
				peerID = credID
			}

			metrics = append(metrics, PeerMetric{
				PeerID:   peerID,
				RXBytes:  p.ReceiveBytes,
				TXBytes:  p.TransmitBytes,
				LastSeen: p.LastHandshakeTime,
				Endpoint: endpoint,
				IsOnline: isOnline,
			})
		}
	}

	return metrics, nil
}

func (m *WireGuardManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for iface := range m.interfaces {
		link, err := netlink.LinkByName(iface)
		if err == nil {
			_ = netlink.LinkSetDown(link)
			_ = netlink.LinkDel(link)
		}
	}

	m.interfaces = make(map[string]*wgtypes.Device)
	return m.client.Close()
}
