package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

type XrayManager struct {
	config    *config.XrayConfig
	configDir string
	running   bool
	mu        sync.Mutex
}

func NewXrayManager(cfg *config.XrayConfig) *XrayManager {
	return &XrayManager{
		config:    cfg,
		configDir: "/etc/vpnbuilder/xray",
	}
}

func (m *XrayManager) EnsureConfigDir() error {
	return os.MkdirAll(m.configDir, 0755)
}

func (m *XrayManager) WriteConfig(ctx context.Context, configData []byte) error {
	if err := m.EnsureConfigDir(); err != nil {
		return fmt.Errorf("ensure config dir: %w", err)
	}

	configPath := filepath.Join(m.configDir, "config.json")
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	logger.DebugContext(ctx, "Xray config written", "path", configPath)
	return nil
}

func (m *XrayManager) GenerateConfig(ctx context.Context, inbounds []InboundConfig) ([]byte, error) {
	cfg := map[string]any{
		"log": map[string]any{
			"loglevel": m.config.LogLevel,
		},
		"inbounds": inbounds,
		"outbounds": []map[string]any{
			{
				"protocol": "freedom",
				"tag":      "direct",
			},
			{
				"protocol": "blackhole",
				"tag":      "block",
			},
		},
		"routing": map[string]any{
			"rules": []map[string]any{
				{
					"type":       "field",
					"outboundTag": "block",
					"ip":          []string{"geoip:private"},
				},
			},
		},
	}

	return json.MarshalIndent(cfg, "", "  ")
}

type InboundConfig struct {
	Tag          string
	Protocol     string
	Listen       string
	Port         int
	Network      string
	Security     string
	Settings     map[string]any
	StreamSettings map[string]any
	Sniffing     map[string]any
}

func (m *XrayManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	logger.InfoContext(ctx, "Starting Xray", "config_dir", m.configDir)
	m.running = true
	return nil
}

func (m *XrayManager) Reload(ctx context.Context) error {
	logger.InfoContext(ctx, "Reloading Xray configuration")
	return nil
}

func (m *XrayManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	logger.InfoContext(ctx, "Stopping Xray")
	m.running = false
	return nil
}

func (m *XrayManager) GetMetrics(ctx context.Context) ([]PeerMetric, error) {
	return []PeerMetric{}, nil
}

type PeerMetric struct {
	PeerID   string
	RXBytes  int64
	TXBytes  int64
	LastSeen time.Time
	Online   bool
}