package manager

import (
	"context"
	"fmt"
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

type WireGuardManager struct {
	config       *config.WireGuardConfig
	client       *wgctrl.Client
	interfaces   map[string]*wgtypes.Device
	peerIPs      map[string]netip.Addr
	mu           sync.RWMutex
	ipAllocator  *IPAllocator
}

type IPAllocator struct {
	networkV4 netip.Prefix
	networkV6 netip.Prefix
	usedV4    map[string]bool
	usedV6    map[string]bool
	nextV4    uint32
	nextV6    uint32
	mu        sync.Mutex
}

func NewIPAllocator(v4, v6 netip.Prefix) *IPAllocator {
	return &IPAllocator{
		networkV4: v4,
		networkV6: v6,
		usedV4:    make(map[string]bool),
		usedV6:    make(map[string]bool),
		nextV4:    2,
		nextV6:    2,
	}
}

func (a *IPAllocator) Allocate(peerID string) (netip.Addr, netip.Addr, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var ipv4, ipv6 netip.Addr

	for i := 0; i < 1000; i++ {
		if a.nextV4 >= (1<<24) {
			return netip.Addr{}, netip.Addr{}, fmt.Errorf("IPv4 pool exhausted")
		}
		ip := netip.AddrFrom4([4]byte{
			byte(a.networkV4.Addr().As4()[0]),
			byte(a.networkV4.Addr().As4()[1]),
			byte(a.networkV4.Addr().As4()[2]),
			byte(a.nextV4),
		})
		a.nextV4++
		key := ip.String()
		if !a.usedV4[key] {
			a.usedV4[key] = true
			ipv4 = ip
			break
		}
	}

	for i := 0; i < 1000; i++ {
		if a.nextV6 >= (1<<24) {
			return netip.Addr{}, netip.Addr{}, fmt.Errorf("IPv6 pool exhausted")
		}
		var addr [16]byte
		copy(addr[:], a.networkV6.Addr().As16()[:8])
		addr[15] = byte(a.nextV6)
		ip := netip.AddrFrom16(addr)
		a.nextV6++
		key := ip.String()
		if !a.usedV6[key] {
			a.usedV6[key] = true
			ipv6 = ip
			break
		}
	}

	if ipv4.IsValid() && ipv6.IsValid() {
		a.usedV4[peerID] = true
		a.usedV6[peerID] = true
		return ipv4, ipv6, nil
	}
	return netip.Addr{}, netip.Addr{}, fmt.Errorf("failed to allocate IPs")
}

func (a *IPAllocator) Release(peerID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.usedV4, peerID)
	delete(a.usedV6, peerID)
}

func NewWireGuardManager(cfg *config.WireGuardConfig) (*WireGuardManager, error) {
	client, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("create wgctrl client: %w", err)
	}

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

	dev, ok := m.interfaces[iface]
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

	var allowed []netip.Prefix
	for _, cidr := range strings.Split(allowedIPs, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			return fmt.Errorf("parse allowed IP: %w", err)
		}
		allowed = append(allowed, p)
	}

	peer := wgtypes.PeerConfig{
		PublicKey:         pubKey,
		PresharedKey:      psk,
		AllowedIPs:        allowed,
		PersistentKeepalive: &keepalive,
		ReplaceAllowedIPs: true,
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

	dev, ok := m.interfaces[iface]
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

func (m *WireGuardManager) GetMetrics(ctx context.Context) ([]PeerMetric, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var metrics []PeerMetric
	for ifaceName, dev := range m.interfaces {
		for _, peer := range dev.Peers {
			metrics = append(metrics, PeerMetric{
				PeerID:    peer.PublicKey.String(),
				RXBytes:   peer.ReceiveBytes,
				TXBytes:   peer.TransmitBytes,
				LastSeen:  time.Unix(peer.LastHandshakeTime.Unix(), 0),
				Endpoint:  peer.Endpoint.String(),
				IsOnline:  time.Since(peer.LastHandshakeTime) < 3*time.Minute,
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
			netlink.LinkDel(link)
		}
	}
	m.interfaces = make(map[string]*wgtypes.Device)

	if m.client != nil {
		m.client.Close()
	}
	return nil
}

type PeerMetric struct {
	PeerID    string
	RXBytes   int64
	TXBytes   int64
	LastSeen  time.Time
	Endpoint  string
	IsOnline  bool
}