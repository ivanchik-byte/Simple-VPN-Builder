package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

func TestCollector_QueueAndDelta(t *testing.T) {
	cfg := &config.Config{
		Agent: config.AgentConfig{
			MetricsInterval: 10 * time.Millisecond,
		},
	}

	collector := NewCollector(nil, nil, nil, cfg)
	require.NotNil(t, collector)

	// Test enqueue
	collector.enqueueReport(&agentv1.MetricsReport{
		Timestamp: time.Now().Unix(),
		Protocols: []*agentv1.ProtocolMetrics{
			{
				Protocol: "wireguard",
				Peers: []*agentv1.PeerMetric{
					{PeerId: "peer-1", RxBytes: 1024, TxBytes: 2048},
				},
			},
		},
	})

	assert.Len(t, collector.queue, 1)

	// Verify buffer bounding
	for i := 0; i < 200; i++ {
		collector.enqueueReport(&agentv1.MetricsReport{
			Timestamp: int64(i),
		})
	}
	assert.Equal(t, maxQueuedReports, len(collector.queue))
}
