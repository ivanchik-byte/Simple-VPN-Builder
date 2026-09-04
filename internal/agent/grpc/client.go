package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

// ConfigHandler handles incoming ConfigUpdate messages from the control plane.
type ConfigHandler interface {
	HandleConfigUpdate(ctx context.Context, update *agentv1.ConfigUpdate) error
	EnqueueConfigUpdate(update *agentv1.ConfigUpdate)
}

// CommandHandler handles incoming Command messages from the control plane.
type CommandHandler interface {
	Execute(ctx context.Context, cmd *agentv1.Command) *agentv1.CommandResult
}

type Client struct {
	config         *config.Config
	conn           *grpc.ClientConn
	client         agentv1.AgentServiceClient
	stream         agentv1.AgentService_ConnectClient
	cancelFunc     context.CancelFunc
	configHandler  ConfigHandler
	commandHandler CommandHandler
	publicKey      string
	sendMu         sync.Mutex
	reconnectCh    chan struct{}
	stopCh         chan struct{}
	stopOnce       sync.Once
	mu             sync.RWMutex
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		config:      cfg,
		reconnectCh: make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
	}
}

func (c *Client) SetHandlers(configHandler ConfigHandler, commandHandler CommandHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configHandler = configHandler
	c.commandHandler = commandHandler
}

func (c *Client) SetPublicKey(pubKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publicKey = pubKey
}

func (c *Client) Connect(ctx context.Context) error {
	var dialOpts []grpc.DialOption

	if c.config.Agent.CACert != "" && c.config.Agent.CertFile != "" && c.config.Agent.KeyFile != "" {
		caCert, err := os.ReadFile(c.config.Agent.CACert)
		if err != nil {
			return fmt.Errorf("read CA cert: %w", err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			return fmt.Errorf("failed to append CA cert")
		}

		clientCert, err := tls.LoadX509KeyPair(c.config.Agent.CertFile, c.config.Agent.KeyFile)
		if err != nil {
			return fmt.Errorf("load client cert: %w", err)
		}

		tlsConfig := &tls.Config{
			RootCAs:      certPool,
			Certificates: []tls.Certificate{clientCert},
			MinVersion:   tls.VersionTLS12,
		}
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	dialOpts = append(dialOpts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                10 * time.Second,
		Timeout:             3 * time.Second,
		PermitWithoutStream: true,
	}))

	conn, err := grpc.NewClient(c.config.Agent.ControlPlane, dialOpts...)
	if err != nil {
		return fmt.Errorf("dial control plane: %w", err)
	}
	c.conn = conn
	c.client = agentv1.NewAgentServiceClient(conn)

	return c.startStream(ctx)
}

func (c *Client) startStream(ctx context.Context) error {
	streamCtx, cancel := context.WithCancel(ctx)
	c.cancelFunc = cancel

	stream, err := c.client.Connect(streamCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("open stream: %w", err)
	}

	c.sendMu.Lock()
	c.stream = stream
	c.sendMu.Unlock()

	go c.recvLoop(streamCtx)

	if err := c.sendRegister(streamCtx); err != nil {
		cancel()
		return fmt.Errorf("send register: %w", err)
	}

	logger.InfoContext(streamCtx, "connected to control plane")
	return nil
}

func (c *Client) sendRegister(ctx context.Context) error {
	c.mu.RLock()
	pubKey := c.publicKey
	c.mu.RUnlock()

	if pubKey == "" {
		if key, err := wgtypes.GeneratePrivateKey(); err == nil {
			pubKey = key.PublicKey().String()
		}
	}

	kernelVer := runtime.GOOS
	if osrelease, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		kernelVer = strings.TrimSpace(string(osrelease))
	}

	return c.send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Register{
			Register: &agentv1.RegisterRequest{
				NodeName:           c.config.Agent.NodeName,
				WireguardPublicKey: pubKey,
				Version:            "1.0.0",
				Labels:             map[string]string{"region": c.config.Agent.Region},
				Architecture:       runtime.GOARCH,
				KernelVersion:      kernelVer,
			},
		},
	})
}

func (c *Client) recvLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
			c.sendMu.Lock()
			stream := c.stream
			c.sendMu.Unlock()

			if stream == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			msg, err := stream.Recv()
			if err != nil {
				logger.ErrorContext(ctx, "stream receive error", "error", err)
				c.triggerReconnect()
				return
			}
			c.handleMessage(ctx, msg)
		}
	}
}

func (c *Client) triggerReconnect() {
	select {
	case c.reconnectCh <- struct{}{}:
	default:
	}
}

// StartReconnectSupervisor manages continuous automatic reconnection on stream drop.
func (c *Client) StartReconnectSupervisor(ctx context.Context) {
	go func() {
		backoff := 1 * time.Second
		const maxBackoff = 30 * time.Second

		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-c.reconnectCh:
				logger.WarnContext(ctx, "reconnecting to control plane", "backoff", backoff)

				timer := time.NewTimer(backoff)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-c.stopCh:
					timer.Stop()
					return
				case <-timer.C:
				}

				if err := c.startStream(ctx); err != nil {
					logger.ErrorContext(ctx, "reconnect failed", "error", err)
					backoff *= 2
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
					c.triggerReconnect()
				} else {
					logger.InfoContext(ctx, "reconnected successfully")
					backoff = 1 * time.Second
				}
			}
		}
	}()
}

func (c *Client) handleMessage(ctx context.Context, msg *agentv1.ControlMessage) {
	if msg == nil {
		return
	}

	c.mu.RLock()
	configH := c.configHandler
	cmdH := c.commandHandler
	c.mu.RUnlock()

	switch payload := msg.Payload.(type) {
	case *agentv1.ControlMessage_Config:
		logger.InfoContext(ctx, "received config update", "version", payload.Config.ConfigVersion)
		if configH != nil {
			// Enqueue sequentially to prevent race conditions and decoupling from streamCtx (CRIT-03)
			configH.EnqueueConfigUpdate(payload.Config)
		}
	case *agentv1.ControlMessage_Command:
		logger.InfoContext(ctx, "received command", "id", payload.Command.CommandId, "type", payload.Command.Type)
		if cmdH != nil {
			go func() {
				res := cmdH.Execute(ctx, payload.Command)
				if res != nil {
					_ = c.SendCommandResult(ctx, res.CommandId, res.Success, res.Output, res.ExitCode)
				}
			}()
		}
	case *agentv1.ControlMessage_Ping:
		logger.DebugContext(ctx, "received control plane ping")
	}
}

func (c *Client) send(msg *agentv1.AgentMessage) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.stream == nil {
		return fmt.Errorf("stream is not connected")
	}
	return c.stream.Send(msg)
}

func (c *Client) SendHeartbeat(ctx context.Context, hb *agentv1.Heartbeat) error {
	return c.send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Heartbeat{Heartbeat: hb},
	})
}

func (c *Client) SendMetrics(ctx context.Context, mr *agentv1.MetricsReport) error {
	return c.send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Metrics{Metrics: mr},
	})
}

func (c *Client) SendConfigAck(ctx context.Context, version int64, success bool, errMsg string) error {
	return c.send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_ConfigAck{
			ConfigAck: &agentv1.ConfigAck{
				ConfigVersion: version,
				Success:       success,
				Error:         errMsg,
			},
		},
	})
}

func (c *Client) SendCommandResult(ctx context.Context, cmdID string, success bool, output string, exitCode int32) error {
	return c.send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_CommandResult{
			CommandResult: &agentv1.CommandResult{
				CommandId: cmdID,
				Success:   success,
				Output:    output,
				ExitCode:  exitCode,
			},
		},
	})
}

func (c *Client) Close() error {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
	c.sendMu.Lock()
	if c.stream != nil {
		_ = c.stream.CloseSend()
		c.stream = nil
	}
	c.sendMu.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
