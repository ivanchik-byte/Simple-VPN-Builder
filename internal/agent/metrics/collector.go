package metrics

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/manager"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

const maxQueuedReports = 128

// xrayBaselinePrefix namespaces Xray delta baselines inside the shared
// lastRX/lastTX maps so WG peer IDs and Xray emails can never collide.
const xrayBaselinePrefix = "xray:"

type Collector struct {
	client      *grpc.Client
	wgManager   *manager.WireGuardManager
	xrayManager *manager.XrayManager
	config      *config.Config
	stopCh      chan struct{}
	stopOnce    sync.Once

	mu     sync.Mutex
	lastRX map[string]int64
	lastTX map[string]int64
	queue  []*agentv1.MetricsReport
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
		lastRX:      make(map[string]int64),
		lastTX:      make(map[string]int64),
		queue:       make([]*agentv1.MetricsReport, 0, maxQueuedReports),
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
		logger.ErrorContext(ctx, "failed to collect metrics", "error", err)
		return
	}

	if len(metrics) > 0 {
		report := &agentv1.MetricsReport{
			Timestamp: time.Now().Unix(),
			Protocols: metrics,
		}
		c.enqueueReport(report)
	}

	c.flushQueue(ctx)
}

func (c *Collector) enqueueReport(report *agentv1.MetricsReport) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.queue) >= maxQueuedReports {
		// Drop oldest report to avoid memory unbounded growth
		c.queue = c.queue[1:]
	}
	c.queue = append(c.queue, report)
}

func (c *Collector) flushQueue(ctx context.Context) {
	c.mu.Lock()
	if len(c.queue) == 0 {
		c.mu.Unlock()
		return
	}
	reportsToSend := make([]*agentv1.MetricsReport, len(c.queue))
	copy(reportsToSend, c.queue)
	c.mu.Unlock()

	sentIndex := 0
	for i, r := range reportsToSend {
		if err := c.client.SendMetrics(ctx, r); err != nil {
			logger.WarnContext(ctx, "failed to dispatch metrics report, retaining queue for retry", "error", err)
			break
		}
		sentIndex = i + 1
	}

	if sentIndex > 0 {
		c.mu.Lock()
		c.queue = c.queue[sentIndex:]
		c.mu.Unlock()
	}
}

func (c *Collector) collect(ctx context.Context) ([]*agentv1.ProtocolMetrics, error) {
	var allMetrics []*agentv1.ProtocolMetrics

	if c.wgManager != nil {
		wgMetrics, err := c.wgManager.GetMetrics(ctx)
		if err != nil {
			logger.ErrorContext(ctx, "failed to get WireGuard metrics", "error", err)
		} else if len(wgMetrics) > 0 {
			peers := make([]*agentv1.PeerMetric, len(wgMetrics))
			c.mu.Lock()

			// Active peer map to prune stale peer baselines and prevent memory leaks (MIN-04)
			activePeerIDs := make(map[string]bool, len(wgMetrics))

			for i, m := range wgMetrics {
				activePeerIDs[m.PeerID] = true
				prevRX, hasPrevRX := c.lastRX[m.PeerID]
				prevTX, hasPrevTX := c.lastTX[m.PeerID]

				var deltaRX, deltaTX int64

				// Baseline initialization on agent boot or new peer (MAJ-04):
				// Never attribute historical cumulative bytes to the current 30-second interval.
				if !hasPrevRX {
					deltaRX = 0
				} else if m.RXBytes >= prevRX {
					deltaRX = m.RXBytes - prevRX
				} else {
					deltaRX = m.RXBytes
				}

				if !hasPrevTX {
					deltaTX = 0
				} else if m.TXBytes >= prevTX {
					deltaTX = m.TXBytes - prevTX
				} else {
					deltaTX = m.TXBytes
				}

				c.lastRX[m.PeerID] = m.RXBytes
				c.lastTX[m.PeerID] = m.TXBytes

				peers[i] = &agentv1.PeerMetric{
					PeerId:        m.PeerID,
					RxBytes:       deltaRX,
					TxBytes:       deltaTX,
					LastHandshake: m.LastSeen.Unix(),
					Endpoint:      m.Endpoint,
					IsOnline:      m.IsOnline,
				}
			}

			// Prune inactive peers (WG keys only; xray:* baselines are owned
			// by the Xray block below and pruned there).
			for pid := range c.lastRX {
				if strings.HasPrefix(pid, xrayBaselinePrefix) {
					continue
				}
				if !activePeerIDs[pid] {
					delete(c.lastRX, pid)
					delete(c.lastTX, pid)
				}
			}
			c.mu.Unlock()

			allMetrics = append(allMetrics, &agentv1.ProtocolMetrics{
				Protocol: "wireguard",
				Peers:    peers,
			})
		}
	}

	if c.xrayManager != nil {
		xrayMetrics, err := c.xrayManager.GetMetrics(ctx)
		if err != nil {
			logger.ErrorContext(ctx, "failed to get Xray metrics", "error", err)
		} else if len(xrayMetrics) > 0 {
			peers := make([]*agentv1.PeerMetric, len(xrayMetrics))
			c.mu.Lock()

			// N3 Critical: Xray reports ABSOLUTE cumulative counters, exactly
			// like WireGuard. Report per-interval DELTAS mirrored on the WG
			// logic: first-seen baselines accrue nothing, counter resets are
			// treated as fresh baselines. Keys are xray:-prefixed so WG and
			// Xray baselines never collide. Guarded by the collector mutex.
			activeXrayIDs := make(map[string]bool, len(xrayMetrics))

			for i, m := range xrayMetrics {
				key := xrayBaselinePrefix + m.PeerID
				activeXrayIDs[key] = true
				prevRX, hasPrevRX := c.lastRX[key]
				prevTX, hasPrevTX := c.lastTX[key]

				var deltaRX, deltaTX int64
				if !hasPrevRX {
					deltaRX = 0
				} else if m.RXBytes >= prevRX {
					deltaRX = m.RXBytes - prevRX
				} else {
					deltaRX = m.RXBytes
				}

				if !hasPrevTX {
					deltaTX = 0
				} else if m.TXBytes >= prevTX {
					deltaTX = m.TXBytes - prevTX
				} else {
					deltaTX = m.TXBytes
				}

				c.lastRX[key] = m.RXBytes
				c.lastTX[key] = m.TXBytes

				peers[i] = &agentv1.PeerMetric{
					PeerId:        m.PeerID,
					RxBytes:       deltaRX,
					TxBytes:       deltaTX,
					LastHandshake: m.LastSeen.Unix(),
					Endpoint:      m.Endpoint,
					IsOnline:      m.IsOnline,
				}
			}

			// Prune stale Xray baselines to prevent memory leaks.
			for key := range c.lastRX {
				if strings.HasPrefix(key, xrayBaselinePrefix) && !activeXrayIDs[key] {
					delete(c.lastRX, key)
					delete(c.lastTX, key)
				}
			}
			c.mu.Unlock()

			allMetrics = append(allMetrics, &agentv1.ProtocolMetrics{
				Protocol: "xray",
				Peers:    peers,
			})
		}
	}

	return allMetrics, nil
}

func (c *Collector) Stop(ctx context.Context) error {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
	return nil
}
