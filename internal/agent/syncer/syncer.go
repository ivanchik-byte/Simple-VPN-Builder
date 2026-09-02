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
	logger.DebugContext(ctx, "Syncing config with control plane")
	// TODO: implement config sync logic
	// For now, just send a heartbeat
	hb := &agentv1.Heartbeat{
		Timestamp: time.Now().Unix(),
		Status:    agentv1.NodeStatus_NODE_STATUS_ONLINE,
		System: &agentv1.SystemInfo{
			UptimeSeconds: time.Since(time.Now()).Seconds(),
		},
	}
	if err := s.client.SendHeartbeat(ctx, hb); err != nil {
		logger.ErrorContext(ctx, "Failed to send heartbeat", "error", err)
	}
}

func (s *Syncer) HandleConfigUpdate(ctx context.Context, update *agentv1.ConfigUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if update.ConfigVersion <= s.currentVer {
		logger.DebugContext(ctx, "Ignoring stale config", "received", update.ConfigVersion, "current", s.currentVer)
		return nil
	}

	logger.InfoContext(ctx, "Applying config update", "version", update.ConfigVersion, "full", update.IsFull)

	if err := s.applyConfig(ctx, update); err != nil {
		logger.ErrorContext(ctx, "Failed to apply config", "error", err)
		s.client.SendConfigAck(ctx, update.ConfigVersion, false, err.Error())
		return err
	}

	s.currentVer = update.ConfigVersion
	s.client.SendConfigAck(ctx, update.ConfigVersion, true, "")
	return nil
}

func (s *Syncer) applyConfig(ctx context.Context, update *agentv1.ConfigUpdate) error {
	if update.NodeConfig != nil {
		if err := s.wgManager.EnsureInterface(ctx, update.NodeConfig.WireguardInterface); err != nil {
			return fmt.Errorf("ensure wg interface: %w", err)
		}
	}

	for _, userCfg := range update.Users {
		for _, cred := range userCfg.Credentials {
			if err := s.applyCredential(ctx, userCfg.UserId, cred); err != nil {
				return fmt.Errorf("apply credential %s: %w", cred.CredentialId, err)
			}
		}
	}

	return nil
}

func (s *Syncer) applyCredential(ctx context.Context, userID string, cred *agentv1.CredentialConfig) error {
	switch cred.Protocol {
	case "wireguard":
		return s.applyWireGuardCredential(ctx, userID, cred)
	case "vless", "vmess", "trojan", "shadowsocks":
		return s.applyXrayCredential(ctx, userID, cred)
	default:
		return fmt.Errorf("unknown protocol: %s", cred.Protocol)
	}
}

func (s *Syncer) applyWireGuardCredential(ctx context.Context, userID string, cred *agentv1.CredentialConfig) error {
	var cfg struct {
		PublicKey     string `json:"public_key"`
		PresharedKey  string `json:"preshared_key"`
		AllowedIPs    string `json:"allowed_ips"`
		Keepalive     int    `json:"keepalive"`
		Endpoint      string `json:"endpoint"`
	}
	if err := json.Unmarshal(cred.ConfigBytes, &cfg); err != nil {
		return fmt.Errorf("unmarshal wg config: %w", err)
	}

	iface := fmt.Sprintf("%s0", s.config.Agent.WireGuard.InterfacePrefix)
	return s.wgManager.AddPeer(ctx, iface, cred.CredentialId, cfg.PublicKey, cfg.PresharedKey, cfg.AllowedIPs, cfg.Keepalive)
}

func (s *Syncer) applyXrayCredential(ctx context.Context, userID string, cred *agentv1.CredentialConfig) error {
	// TODO: implement Xray credential application
	logger.DebugContext(ctx, "Xray credential application not yet implemented", "credential", cred.CredentialId)
	return nil
}

func (s *Syncer) Stop(ctx context.Context) error {
	close(s.stopCh)
	return nil
}