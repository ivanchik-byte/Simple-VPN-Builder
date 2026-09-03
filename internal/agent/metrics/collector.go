package metrics

import (
	"context"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/manager"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

type Collector struct {
	client      *grpc.Client
	wgManager   *manager.WireGuardManager
	xrayManager *manager.XrayManager
	config      *config.Config
	stopCh      chan struct{}
}

func NewCollector(
	wgManager *manager.WireGuardManager,
	xrayManager *manager.XrayManager,
	client *grpc.Client,
	cfg *config.Config,
) *Collector {
	return &Collector{
		wgManager:   wgManager,
		xrayManager: xrayManager,
		client:      client,
		config:      cfg,
		stopCh:      make(chan struct{}),
	}
}

func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(c.config.Agent.MetricsInterval)
	defer ticker.Stop()

	c.collectAndSend(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.collectAndSend(ctx)
		}
	}
}

func (c *Collector) collectAndSend(ctx context.Context) {
	metrics, err := c.collect(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to collect metrics", "error", err)
		return
	}

	if len(metrics) == 0 {
		return
	}

	report := &agentv1.MetricsReport{
		Timestamp: time.Now().Unix(),
		Protocols: metrics,
	}

	if err := c.client.SendMetrics(ctx, report); err != nil {
		logger.ErrorContext(ctx, "Failed to send metrics", "error", err)
	}
}

func (c *Collector) collect(ctx context.Context) ([]*agentv1.ProtocolMetrics, error) {
	var allMetrics []*agentv1.ProtocolMetrics

	wgMetrics, err := c.wgManager.GetMetrics(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to get WireGuard metrics", "error", err)
	} else if len(wgMetrics) > 0 {
		peers := make([]*agentv1.PeerMetric, len(wgMetrics))
		for i, m := range wgMetrics {
			peers[i] = &agentv1.PeerMetric{
				PeerId:        m.PeerID,
				RxBytes:       m.RXBytes,
				TxBytes:       m.TXBytes,
				LastHandshake: m.LastSeen.Unix(),
				Endpoint:      m.Endpoint,
				IsOnline:      m.IsOnline,
			}
		}
		allMetrics = append(allMetrics, &agentv1.ProtocolMetrics{
			Protocol: "wireguard",
			Peers:    peers,
		})
	}

	xrayMetrics, err := c.xrayManager.GetMetrics(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to get Xray metrics", "error", err)
	} else if len(xrayMetrics) > 0 {
		peers := make([]*agentv1.PeerMetric, len(xrayMetrics))
		for i, m := range xrayMetrics {
			peers[i] = &agentv1.PeerMetric{
				PeerId:        m.PeerID,
				RxBytes:       m.RXBytes,
				TxBytes:       m.TXBytes,
				LastHandshake: m.LastSeen.Unix(),
				Endpoint:      m.Endpoint,
				IsOnline:      m.IsOnline,
			}
		}
		allMetrics = append(allMetrics, &agentv1.ProtocolMetrics{
			Protocol: "xray",
			Peers:    peers,
		})
	}

	return allMetrics, nil
}

func (c *Collector) Stop(ctx context.Context) error {
	close(c.stopCh)
	return nil
}
