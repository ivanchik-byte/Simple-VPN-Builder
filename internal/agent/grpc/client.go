package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

type Client struct {
	config     *config.Config
	conn       *grpc.ClientConn
	client     agentv1.AgentServiceClient
	stream     agentv1.AgentService_ConnectClient
	cancelFunc context.CancelFunc
	sendMu     sync.Mutex
}

func NewClient(cfg *config.Config) *Client {
	return &Client{config: cfg}
}

func (c *Client) Connect(ctx context.Context) error {
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

	ctx, c.cancelFunc = context.WithCancel(ctx)

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	conn, err := grpc.NewClient(c.config.Agent.ControlPlane, dialOpts...)
	if err != nil {
		return fmt.Errorf("dial control plane: %w", err)
	}
	c.conn = conn
	c.client = agentv1.NewAgentServiceClient(conn)

	stream, err := c.client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	c.stream = stream

	go c.recvLoop(ctx)

	if err := c.sendRegister(ctx); err != nil {
		return fmt.Errorf("send register: %w", err)
	}

	logger.InfoContext(ctx, "Connected to control plane")
	return nil
}

func (c *Client) sendRegister(ctx context.Context) error {
	return c.stream.Send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Register{
			Register: &agentv1.RegisterRequest{
				NodeName:           c.config.Agent.NodeName,
				WireguardPublicKey: "", // TODO: generate/load
				Version:            "dev",
				Labels:             map[string]string{},
				Architecture:       "amd64",
				KernelVersion:      "unknown",
			},
		},
	})
}

func (c *Client) recvLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := c.stream.Recv()
			if err != nil {
				logger.ErrorContext(ctx, "Stream receive error", "error", err)
				return
			}
			c.handleMessage(ctx, msg)
		}
	}
}

func (c *Client) handleMessage(ctx context.Context, msg *agentv1.ControlMessage) {
	switch payload := msg.Payload.(type) {
	case *agentv1.ControlMessage_Config:
		logger.InfoContext(ctx, "Received config update", "version", payload.Config.ConfigVersion)
		// TODO: handle config update via syncer
	case *agentv1.ControlMessage_Command:
		logger.InfoContext(ctx, "Received command", "type", payload.Command.Type)
		// TODO: execute command
	case *agentv1.ControlMessage_Ping:
		// Respond with pong if needed
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
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
	if c.stream != nil {
		_ = c.stream.CloseSend()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
