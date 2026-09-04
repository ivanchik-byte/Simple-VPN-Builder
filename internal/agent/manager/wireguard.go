package manager

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// WireGuardDeviceClient abstracts wgctrl.Client for testing and production.
type WireGuardDeviceClient interface {
	Device(name string) (*wgtypes.Device, error)
	ConfigureDevice(name string, cfg wgtypes.Config) error
	Close() error
}

// DesiredPeer defines the target WireGuard peer configuration from the control plane.
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

	if existingV4, ok4 := a.peerToV4[peerID]; ok4 {
		if existingV6, ok6 := a.peerToV6[peerID]; ok6 {
			return existingV4, existingV6, nil
		}
	}

	var ipv4, ipv6 netip.Addr

	v4Bytes := a.networkV4.Addr().As4()
	baseV4 := binary.BigEndian.Uint32(v4Bytes[:])
	hostBits := 32 - a.networkV4.Bits()
	var maxHosts uint32
	if hostBits >= 32 {
		maxHosts = 0xFFFFFFFF
	} else {
		maxHosts = (1 << hostBits) - 2
	}

	for i := uint32(0); i < maxHosts; i++ {
		if a.nextV4 > maxHosts {
			a.nextV4 = 2
		}
		curr := baseV4 + a.nextV4
		a.nextV4++

		var ipBytes [4]byte
		binary.BigEndian.PutUint32(ipBytes[:], curr)
		ip := netip.AddrFrom4(ipBytes)
		if !a.usedV4[ip] {
			ipv4 = ip
			break
		}
	}
	if !ipv4.IsValid() {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("IPv4 pool exhausted")
	}

	v6Bytes := a.networkV6.Addr().As16()
	for i := uint64(0); i < 65535; i++ {
		currV6 := a.nextV6
		a.nextV6++

		var ipBytes [16]byte
		copy(ipBytes[:8], v6Bytes[:8])
		binary.BigEndian.PutUint64(ipBytes[8:], currV6)

		ip := netip.AddrFrom16(ipBytes)
		if !a.usedV6[ip] {
			ipv6 = ip
			break
		}
	}
	if !ipv6.IsValid() {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("IPv6 pool exhausted")
	}

	a.usedV4[ipv4] = true
	a.usedV6[ipv6] = true
	a.peerToV4[peerID] = ipv4
	a.peerToV6[peerID] = ipv6

	return ipv4, ipv6, nil
}

func (a *IPAllocator) Release(peerID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if ip, exists := a.peerToV4[peerID]; exists {
		delete(a.usedV4, ip)
		delete(a.peerToV4, peerID)
	}
	if ip, exists := a.peerToV6[peerID]; exists {
		delete(a.usedV6, ip)
		delete(a.peerToV6, peerID)
	}
}

func NewWireGuardManager(cfg *config.WireGuardConfig) (*WireGuardManager, error) {
	client, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("create wgctrl client: %w", err)
	}
	return NewWireGuardManagerWithClient(cfg, client)
}

func NewWireGuardManagerWithClient(cfg *config.WireGuardConfig, client WireGuardDeviceClient) (*WireGuardManager, error) {
	v4Prefix, err := netip.ParsePrefix(cfg.SubnetV4)
	if err != nil {
		return nil, fmt.Errorf("parse IPv4 subnet: %w", err)
	}
	v6Prefix, err := netip.ParsePrefix(cfg.SubnetV6)
	if err != nil {
		return nil, fmt.Errorf("parse IPv6 subnet: %w", err)
	}

	return &WireGuardManager{
		config:      cfg,
		client:      client,
		interfaces:  make(map[string]*wgtypes.Device),
		peerIPs:     make(map[string]netip.Addr),
		ipAllocator: NewIPAllocator(v4Prefix, v6Prefix),
	}, nil
}

func (m *WireGuardManager) EnsureInterface(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.interfaces[name]; exists {
		return nil
	}

	iface := fmt.Sprintf("%s%d", m.config.InterfacePrefix, len(m.interfaces))
	logger.InfoContext(ctx, "Creating WireGuard interface", "name", iface)

	la := netlink.NewLinkAttrs()
	la.Name = iface
	la.MTU = m.config.MTU

	wgLink := &netlink.GenericLink{
		LinkAttrs: la,
		LinkType:  "wireguard",
	}

	if err := netlink.LinkAdd(wgLink); err != nil {
		return fmt.Errorf("create interface: %w", err)
	}

	if err := netlink.LinkSetUp(wgLink); err != nil {
		return fmt.Errorf("bring up interface: %w", err)
	}

	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return fmt.Errorf("generate private key: %w", err)
	}

	cfg := wgtypes.Config{
		PrivateKey:   &key,
		ListenPort:   nil,
		ReplacePeers: false,
	}
	if err := m.client.ConfigureDevice(iface, cfg); err != nil {
		return fmt.Errorf("configure device: %w", err)
	}

	dev, err := m.client.Device(iface)
	if err != nil {
		return fmt.Errorf("get device: %w", err)
	}

	m.interfaces[iface] = dev
	logger.InfoContext(ctx, "WireGuard interface ready", "name", iface, "public_key", dev.PublicKey.String())
	return nil
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
	for _, cidr := range strings.Split(allowedIPs, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("parse allowed IP: %w", err)
		}
		allowed = append(allowed, *ipNet)
	}

	keepaliveInterval := time.Duration(keepalive) * time.Second
	peer := wgtypes.PeerConfig{
		PublicKey:                   pubKey,
		PresharedKey:                psk,
		AllowedIPs:                  allowed,
		PersistentKeepaliveInterval: &keepaliveInterval,
		ReplaceAllowedIPs:           true,
	}

	cfg := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peer},
	}

	if err := m.client.ConfigureDevice(iface, cfg); err != nil {
		return fmt.Errorf("add peer: %w", err)
	}

	updated, err := m.client.Device(iface)
	if err != nil {
		return fmt.Errorf("get updated device: %w", err)
	}
	m.interfaces[iface] = updated

	logger.InfoContext(ctx, "Added WireGuard peer", "iface", iface, "peer", peerID)
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

	peer := wgtypes.PeerConfig{
		PublicKey: pubKey,
		Remove:    true,
	}

	cfg := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peer},
	}

	if err := m.client.ConfigureDevice(iface, cfg); err != nil {
		return fmt.Errorf("remove peer: %w", err)
	}

	updated, err := m.client.Device(iface)
	if err != nil {
		return fmt.Errorf("get updated device: %w", err)
	}
	m.interfaces[iface] = updated

	m.ipAllocator.Release(peerID)
	logger.InfoContext(ctx, "Removed WireGuard peer", "iface", iface, "peer", peerID)
	return nil
}

// SyncPeers performs zero-downtime atomic batch synchronization of peers.
// It computes the set difference: adds new peers, updates modified peers, and purges removed peers.
// Learned roaming endpoints are preserved by leaving Endpoint nil on updates.
func (m *WireGuardManager) SyncPeers(ctx context.Context, iface string, desired []DesiredPeer) error {
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
			_, ipNet, err := net.ParseCIDR(cidr)
			if err == nil {
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

	// Purge peers no longer desired
	for key := range existingPeers {
		if !desiredKeys[key] {
			peerConfigs = append(peerConfigs, wgtypes.PeerConfig{
				PublicKey: key,
				Remove:    true,
			})
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
		"iface", iface, "configured_peers", len(peerConfigs))
	return nil
}

func (m *WireGuardManager) GetMetrics(ctx context.Context) ([]PeerMetric, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var metrics []PeerMetric
	for _, dev := range m.interfaces {
		for _, peer := range dev.Peers {
			endpoint := ""
			if peer.Endpoint != nil {
				endpoint = peer.Endpoint.String()
			}
			metrics = append(metrics, PeerMetric{
				PeerID:   peer.PublicKey.String(),
				RXBytes:  peer.ReceiveBytes,
				TXBytes:  peer.TransmitBytes,
				LastSeen: peer.LastHandshakeTime,
				Endpoint: endpoint,
				IsOnline: time.Since(peer.LastHandshakeTime) < 3*time.Minute,
			})
		}
	}
	return metrics, nil
}

func (m *WireGuardManager) GetInterfaceConfig(iface string) (*wgtypes.Device, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dev, ok := m.interfaces[iface]
	if !ok {
		return nil, fmt.Errorf("interface not found: %s", iface)
	}
	return dev, nil
}

func (m *WireGuardManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name := range m.interfaces {
		logger.InfoContext(ctx, "Removing WireGuard interface", "name", name)
		link, err := netlink.LinkByName(name)
		if err == nil {
			_ = netlink.LinkDel(link)
		}
	}
	m.interfaces = make(map[string]*wgtypes.Device)

	if m.client != nil {
		m.client.Close()
	}
	return nil
}
