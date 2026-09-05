package manager

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/xtls/xray-core/app/proxyman/command"
	statscommand "github.com/xtls/xray-core/app/stats/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/vless"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

// XrayApiClient defines dynamic gRPC client methods for Xray-core.
type XrayApiClient interface {
	AddClient(ctx context.Context, inboundTag, email, uuid, flow string) error
	RemoveClient(ctx context.Context, inboundTag, email string) error
	QueryUserStats(ctx context.Context, email string, reset bool) (int64, int64, error)
	QueryAllUserStats(ctx context.Context, reset bool) (map[string][2]int64, error)
	Close() error
}

type grpcXrayClient struct {
	conn          *grpc.ClientConn
	handlerClient command.HandlerServiceClient
	statsClient   statscommand.StatsServiceClient
}

func NewGrpcXrayClient(addr string) (*grpcXrayClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to xray api: %w", err)
	}
	return &grpcXrayClient{
		conn:          conn,
		handlerClient: command.NewHandlerServiceClient(conn),
		statsClient:   statscommand.NewStatsServiceClient(conn),
	}, nil
}

func (c *grpcXrayClient) AddClient(ctx context.Context, inboundTag, email, uuid, flow string) error {
	req := &command.AlterInboundRequest{
		Tag: inboundTag,
		Operation: serial.ToTypedMessage(&command.AddUserOperation{
			User: &protocol.User{
				Email: email,
				Account: serial.ToTypedMessage(&vless.Account{
					Id:   uuid,
					Flow: flow,
				}),
			},
		}),
	}
	_, err := c.handlerClient.AlterInbound(ctx, req)
	return err
}

func (c *grpcXrayClient) RemoveClient(ctx context.Context, inboundTag, email string) error {
	req := &command.AlterInboundRequest{
		Tag: inboundTag,
		Operation: serial.ToTypedMessage(&command.RemoveUserOperation{
			Email: email,
		}),
	}
	_, err := c.handlerClient.AlterInbound(ctx, req)
	return err
}

func (c *grpcXrayClient) QueryUserStats(ctx context.Context, email string, reset bool) (int64, int64, error) {
	pattern := fmt.Sprintf("user>>>%s>>>traffic>>>", email)
	resp, err := c.statsClient.QueryStats(ctx, &statscommand.QueryStatsRequest{
		Pattern: pattern,
		Reset_:  reset,
	})
	if err != nil {
		return 0, 0, err
	}

	var rxBytes, txBytes int64
	for _, stat := range resp.Stat {
		if stat.Name == fmt.Sprintf("user>>>%s>>>traffic>>>downlink", email) {
			txBytes = stat.Value // Server downlink = client downloaded (TX from server)
		} else if stat.Name == fmt.Sprintf("user>>>%s>>>traffic>>>uplink", email) {
			rxBytes = stat.Value // Server uplink = client uploaded (RX from server)
		}
	}
	return rxBytes, txBytes, nil
}

func (c *grpcXrayClient) QueryAllUserStats(ctx context.Context, reset bool) (map[string][2]int64, error) {
	resp, err := c.statsClient.QueryStats(ctx, &statscommand.QueryStatsRequest{
		Pattern: "user>>>",
		Reset_:  reset,
	})
	if err != nil {
		return nil, err
	}

	res := make(map[string][2]int64)
	for _, stat := range resp.Stat {
		// Pattern: user>>>{email}>>>traffic>>>{uplink|downlink}
		parts := strings.Split(stat.Name, ">>>")
		if len(parts) >= 4 && parts[0] == "user" && parts[2] == "traffic" {
			email := parts[1]
			pair := res[email]
			if parts[3] == "uplink" {
				pair[0] = stat.Value
			} else if parts[3] == "downlink" {
				pair[1] = stat.Value
			}
			res[email] = pair
		}
	}
	return res, nil
}

func (c *grpcXrayClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// VLESSClient holds tenant credential data for Xray.
type VLESSClient struct {
	CredentialID string
	UUID         string
	Email        string
	Flow         string
}

// RealityServerSettings configures XTLS Reality camouflage.
type RealityServerSettings struct {
	Dest        string   `json:"dest"`
	ServerNames []string `json:"server_names"`
	PrivateKey  string   `json:"private_key"`
	PublicKey   string   `json:"public_key"`
	ShortIDs    []string `json:"short_ids"`
}

// GenerateRealityKeypair creates an X25519 private/public keypair.
func GenerateRealityKeypair() (privateKeyBase64, publicKeyBase64 string, err error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate x25519 key: %w", err)
	}

	privBytes := priv.Bytes()
	pubBytes := priv.PublicKey().Bytes()

	return base64.RawURLEncoding.EncodeToString(privBytes),
		base64.RawURLEncoding.EncodeToString(pubBytes), nil
}

// GenerateShortID produces an 8-byte (16 hex chars) short ID.
func GenerateShortID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// LoadOrGenerateRealitySettings reads reality settings from keyPath or generates deterministic new ones and saves them.
func LoadOrGenerateRealitySettings(keyPath, defaultDest string, defaultServerNames []string) (RealityServerSettings, error) {
	if keyPath != "" {
		if data, err := os.ReadFile(keyPath); err == nil {
			var settings RealityServerSettings
			if jsonErr := json.Unmarshal(data, &settings); jsonErr == nil && settings.PrivateKey != "" && settings.PublicKey != "" {
				return settings, nil
			}
		}
	}

	privKey, pubKey, err := GenerateRealityKeypair()
	if err != nil {
		return RealityServerSettings{}, fmt.Errorf("generate reality keypair: %w", err)
	}

	shortID, err := GenerateShortID()
	if err != nil {
		return RealityServerSettings{}, fmt.Errorf("generate short id: %w", err)
	}

	dest := defaultDest
	if dest == "" {
		dest = "swdist.apple.com:443"
	}

	names := defaultServerNames
	if len(names) == 0 {
		names = []string{"swdist.apple.com"}
	}

	settings := RealityServerSettings{
		Dest:        dest,
		ServerNames: names,
		PrivateKey:  privKey,
		PublicKey:   pubKey,
		ShortIDs:    []string{shortID},
	}

	if keyPath != "" {
		if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err == nil {
			if encoded, err := json.MarshalIndent(settings, "", "  "); err == nil {
				_ = os.WriteFile(keyPath, encoded, 0o600)
			}
		}
	}

	return settings, nil
}

type XrayManager struct {
	config        *config.XrayConfig
	configDir     string
	inboundTag    string
	realityCfg    RealityServerSettings
	apiClient     XrayApiClient
	activeClients map[string]VLESSClient // email -> client
	cmd           *exec.Cmd
	running       bool
	lastError     error
	mu            sync.RWMutex
}

func (m *XrayManager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

func (m *XrayManager) LastError() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastError
}

func NewXrayManager(cfg *config.XrayConfig) *XrayManager {
	dir := "/etc/vpnbuilder/xray"
	if cfg != nil && cfg.ConfigDir != "" {
		dir = cfg.ConfigDir
	}
	return &XrayManager{
		config:        cfg,
		configDir:     dir,
		inboundTag:    "vless-reality-in",
		activeClients: make(map[string]VLESSClient),
	}
}

func (m *XrayManager) SetAPIClient(client XrayApiClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apiClient = client
}

func (m *XrayManager) SetRealitySettings(settings RealityServerSettings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.realityCfg = settings
}

func (m *XrayManager) RealitySettings() RealityServerSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.realityCfg
}

func (m *XrayManager) EnsureConfigDir() error {
	return os.MkdirAll(m.configDir, 0o755)
}

func (m *XrayManager) BuildInboundConfig(port int) map[string]any {
	m.mu.RLock()
	reality := m.realityCfg
	m.mu.RUnlock()

	if reality.Dest == "" {
		reality.Dest = "swdist.apple.com:443"
	}
	if len(reality.ServerNames) == 0 {
		reality.ServerNames = []string{"swdist.apple.com"}
	}
	if len(reality.ShortIDs) == 0 {
		reality.ShortIDs = []string{"0123456789abcdef"}
	}

	return map[string]any{
		"tag":      m.inboundTag,
		"port":     port,
		"protocol": "vless",
		"settings": map[string]any{
			"clients":    []map[string]any{},
			"decryption": "none",
		},
		"streamSettings": map[string]any{
			"network":  "tcp",
			"security": "reality",
			"realitySettings": map[string]any{
				"show":        false,
				"dest":        reality.Dest,
				"xver":        0,
				"serverNames": reality.ServerNames,
				"privateKey":  reality.PrivateKey,
				"shortIds":    reality.ShortIDs,
				"spiderX":     "/",
			},
		},
		"sniffing": map[string]any{
			"enabled":      true,
			"destOverride": []string{"http", "tls", "quic"},
			"routeOnly":    true,
		},
	}
}

func (m *XrayManager) BuildDaemonConfig(vlessPort int) ([]byte, error) {
	inbound := m.BuildInboundConfig(vlessPort)

	apiPort := m.config.APIPort
	if apiPort <= 0 {
		apiPort = 10085
	}

	cfg := map[string]any{
		"log": map[string]any{
			"loglevel": m.config.LogLevel,
		},
		"api": map[string]any{
			"tag": "api",
			"services": []string{
				"HandlerService",
				"StatsService",
				"LoggerService",
			},
		},
		"stats": map[string]any{},
		"inbounds": []any{
			map[string]any{
				"tag":      "api-inbound",
				"listen":   "127.0.0.1",
				"port":     apiPort,
				"protocol": "dokodemo-door",
				"settings": map[string]any{
					"address": "127.0.0.1",
				},
			},
			inbound,
		},
		"outbounds": []map[string]any{
			{
				"protocol": "freedom",
				"tag":      "direct",
				"settings": map[string]any{
					"domainStrategy": "UseIP",
				},
			},
			{
				"protocol": "blackhole",
				"tag":      "block",
			},
		},
		"routing": map[string]any{
			"domainStrategy": "IPIfNonMatch",
			"rules": []map[string]any{
				{
					"type":        "field",
					"inboundTag": []string{"api-inbound"},
					"outboundTag": "api",
				},
				{
					"type":        "field",
					"outboundTag": "block",
					"ip": []string{
						"geoip:private",
						"127.0.0.0/8",
						"10.0.0.0/8",
						"172.16.0.0/12",
						"192.168.0.0/16",
						"169.254.169.254/32",
						"::1/128",
						"fc00::/7",
						"fe80::/10",
					},
				},
				{
					"type":        "field",
					"outboundTag": "block",
					"protocol":    []string{"bittorrent"},
				},
				{
					"type":        "field",
					"outboundTag": "block",
					"domain": []string{
						"geosite:private",
						"geosite:bittorrent",
						"geosite:category-porn",
						"keyword:torrent",
						"keyword:tracker",
					},
				},
			},
		},
	}

	return json.MarshalIndent(cfg, "", "  ")
}

func (m *XrayManager) WriteConfig(ctx context.Context, configData []byte) error {
	if err := m.EnsureConfigDir(); err != nil {
		return fmt.Errorf("ensure config dir: %w", err)
	}

	configPath := filepath.Join(m.configDir, "config.json")
	if err := os.WriteFile(configPath, configData, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	logger.DebugContext(ctx, "Xray config written", "path", configPath)
	return nil
}

// SyncClients dynamically provisions or revokes VLESS users via the gRPC HandlerService without restarting Xray.
func (m *XrayManager) SyncClients(ctx context.Context, desired []VLESSClient, isFull bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	desiredMap := make(map[string]VLESSClient, len(desired))
	for _, c := range desired {
		if c.Email == "" {
			c.Email = c.UUID
		}
		if c.Flow == "" {
			c.Flow = "xtls-rprx-vision"
		}
		desiredMap[c.Email] = c
	}

	// 1. Add or update clients (CRIT-03)
	for email, client := range desiredMap {
		existing, ok := m.activeClients[email]
		needsUpdate := !ok || existing.UUID != client.UUID || existing.Flow != client.Flow

		if needsUpdate {
			if ok && m.apiClient != nil {
				// Remove existing client first to avoid "user already exists" error in Xray
				_ = m.apiClient.RemoveClient(ctx, m.inboundTag, email)
			}

			if m.apiClient != nil {
				if err := m.apiClient.AddClient(ctx, m.inboundTag, client.Email, client.UUID, client.Flow); err != nil {
					logger.ErrorContext(ctx, "failed to dynamically add vless client to xray",
						"email", client.Email, "error", err)
					return fmt.Errorf("add vless client %s: %w", client.Email, err)
				}
			}
			m.activeClients[email] = client
		}
	}

	// 2. Remove obsolete clients on full sync (CRIT-03)
	if isFull {
		for email := range m.activeClients {
			if _, exists := desiredMap[email]; !exists {
				if m.apiClient != nil {
					if err := m.apiClient.RemoveClient(ctx, m.inboundTag, email); err != nil {
						logger.ErrorContext(ctx, "failed to dynamically remove vless client from xray",
							"email", email, "error", err)
						return fmt.Errorf("remove vless client %s: %w", email, err)
					}
				}
				delete(m.activeClients, email)
			}
		}
	}

	logger.InfoContext(ctx, "synchronized VLESS clients with Xray engine",
		"active_count", len(m.activeClients), "is_full", isFull)
	return nil
}

// GetMetrics queries Xray StatsService for all active clients in batch (MAJ-06).
func (m *XrayManager) GetMetrics(ctx context.Context) ([]PeerMetric, error) {
	m.mu.RLock()
	apiClient := m.apiClient
	clients := make([]VLESSClient, 0, len(m.activeClients))
	for _, c := range m.activeClients {
		clients = append(clients, c)
	}
	m.mu.RUnlock()

	if apiClient == nil || len(clients) == 0 {
		return []PeerMetric{}, nil
	}

	// Query all user traffic stats at once using batch query
	statsMap, err := apiClient.QueryAllUserStats(ctx, true)
	if err != nil {
		// Fallback to individual queries if batch query fails
		var fallbackMetrics []PeerMetric
		for _, c := range clients {
			rx, tx, qErr := apiClient.QueryUserStats(ctx, c.Email, true)
			if qErr != nil {
				continue
			}
			if rx > 0 || tx > 0 {
				fallbackMetrics = append(fallbackMetrics, PeerMetric{
					PeerID:   c.CredentialID,
					RXBytes:  rx,
					TXBytes:  tx,
					IsOnline: true,
				})
			}
		}
		return fallbackMetrics, nil
	}

	var metrics []PeerMetric
	for _, c := range clients {
		pair, ok := statsMap[c.Email]
		if !ok {
			continue
		}
		rx, tx := pair[0], pair[1]
		if rx > 0 || tx > 0 {
			metrics = append(metrics, PeerMetric{
				PeerID:   c.CredentialID,
				RXBytes:  rx,
				TXBytes:  tx,
				IsOnline: true,
			})
		}
	}

	return metrics, nil
}

func (m *XrayManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	binPath := m.config.BinaryPath
	if binPath == "" {
		binPath = "xray"
	}

	configPath := filepath.Join(m.configDir, "config.json")
	cmd := exec.CommandContext(ctx, binPath, "run", "-c", configPath)
	if err := cmd.Start(); err != nil {
		m.lastError = fmt.Errorf("xray failed to start: %w", err)
		logger.WarnContext(ctx, "xray binary not found or failed to start (ignoring in test mode)", "error", err)
	} else {
		m.lastError = nil
		m.cmd = cmd
		m.running = true

		// Supervise process exit and prevent zombie processes (CRIT-04)
		go func() {
			err := cmd.Wait()
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.running {
				m.running = false
				logger.Warn("xray process exited", "error", err)
			}
		}()
	}

	apiPort := m.config.APIPort
	if apiPort <= 0 {
		apiPort = 10085
	}
	if m.apiClient == nil {
		client, err := NewGrpcXrayClient(fmt.Sprintf("127.0.0.1:%d", apiPort))
		if err == nil {
			m.apiClient = client
		}
	}

	logger.InfoContext(ctx, "started Xray manager", "config_dir", m.configDir)
	return nil
}

func (m *XrayManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	if m.apiClient != nil {
		_ = m.apiClient.Close()
		m.apiClient = nil
	}

	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() {
			done <- m.cmd.Wait()
		}()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = m.cmd.Process.Kill()
			<-done
		}
		m.cmd = nil
	}

	m.running = false
	logger.InfoContext(ctx, "stopped Xray manager")
	return nil
}
