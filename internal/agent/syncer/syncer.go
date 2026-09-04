package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/manager"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

type Syncer struct {
	client      *grpc.Client
	wgManager   *manager.WireGuardManager
	xrayManager *manager.XrayManager
	config      *config.Config
	currentVer  int64
	startedAt   time.Time
	mu          sync.Mutex
	stopCh      chan struct{}
}

func New(
	client *grpc.Client,
	wgManager *manager.WireGuardManager,
	xrayManager *manager.XrayManager,
	cfg *config.Config,
) *Syncer {
	return &Syncer{
		client:      client,
		wgManager:   wgManager,
		xrayManager: xrayManager,
		config:      cfg,
		startedAt:   time.Now(),
		stopCh:      make(chan struct{}),
	}
}

func (s *Syncer) Run(ctx context.Context) {
	ticker := time.NewTicker(s.config.Agent.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.syncOnce(ctx)
		}
	}
}

func (s *Syncer) syncOnce(ctx context.Context) {
	logger.DebugContext(ctx, "syncing heartbeat with control plane")
	hb := &agentv1.Heartbeat{
		Timestamp: time.Now().Unix(),
		Status:    agentv1.NodeStatus_NODE_STATUS_ONLINE,
		System: &agentv1.SystemInfo{
			UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		},
	}
	if err := s.client.SendHeartbeat(ctx, hb); err != nil {
		logger.ErrorContext(ctx, "failed to send heartbeat", "error", err)
	}
}

func (s *Syncer) HandleConfigUpdate(ctx context.Context, update *agentv1.ConfigUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if update == nil {
		return fmt.Errorf("nil config update")
	}

	if update.ConfigVersion <= s.currentVer {
		logger.DebugContext(ctx, "ignoring stale config", "received", update.ConfigVersion, "current", s.currentVer)
		return nil
	}

	logger.InfoContext(ctx, "applying config update", "version", update.ConfigVersion, "full", update.IsFull)

	if err := s.applyConfig(ctx, update); err != nil {
		logger.ErrorContext(ctx, "failed to apply config", "error", err)
		if ackErr := s.client.SendConfigAck(ctx, update.ConfigVersion, false, err.Error()); ackErr != nil {
			logger.WarnContext(ctx, "failed to send negative config ack", "error", ackErr)
		}
		return err
	}

	s.currentVer = update.ConfigVersion
	if ackErr := s.client.SendConfigAck(ctx, update.ConfigVersion, true, ""); ackErr != nil {
		logger.WarnContext(ctx, "failed to send positive config ack", "error", ackErr)
	}
	return nil
}

func (s *Syncer) applyConfig(ctx context.Context, update *agentv1.ConfigUpdate) error {
	iface := fmt.Sprintf("%s0", s.config.Agent.WireGuard.InterfacePrefix)
	if update.NodeConfig != nil && update.NodeConfig.WireguardInterface != "" {
		iface = update.NodeConfig.WireguardInterface
	}

	if s.wgManager != nil {
		if err := s.wgManager.EnsureInterface(ctx, iface); err != nil {
			return fmt.Errorf("ensure wg interface: %w", err)
		}
	}

	desiredPeers := make([]manager.DesiredPeer, 0)

	for _, userCfg := range update.Users {
		for _, cred := range userCfg.Credentials {
			switch cred.Protocol {
			case "wireguard", "amneziawg":
				var cfg struct {
					PublicKey    string `json:"public_key"`
					PresharedKey string `json:"preshared_key"`
					AllowedIPs   string `json:"allowed_ips"`
					Keepalive    int    `json:"keepalive"`
					Endpoint     string `json:"endpoint"`
				}
				if len(cred.ConfigBytes) > 0 {
					if err := json.Unmarshal(cred.ConfigBytes, &cfg); err != nil {
						logger.WarnContext(ctx, "failed to parse wg config json, trying raw string", "error", err)
					}
				}

				if cfg.PublicKey != "" {
					desiredPeers = append(desiredPeers, manager.DesiredPeer{
						PeerID:       cred.CredentialId,
						PublicKey:    cfg.PublicKey,
						PresharedKey: cfg.PresharedKey,
						AllowedIPs:   cfg.AllowedIPs,
						Keepalive:    cfg.Keepalive,
					})
				}
			case "vless", "vmess", "trojan", "shadowsocks":
				// Xray handled separately in Phase 6
			}
		}
	}

	if s.wgManager != nil {
		if err := s.wgManager.SyncPeers(ctx, iface, desiredPeers); err != nil {
			return fmt.Errorf("differential peer sync failed: %w", err)
		}
	}

	return nil
}

func (s *Syncer) CurrentVersion() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentVer
}

func (s *Syncer) Stop(ctx context.Context) error {
	close(s.stopCh)
	return nil
}
