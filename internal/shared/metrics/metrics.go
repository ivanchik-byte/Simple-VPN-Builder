package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Control Plane RED Metrics
	CPHTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "vpn",
			Subsystem: "cp",
			Name:      "http_requests_total",
			Help:      "Total HTTP requests handled by the Control Plane.",
		},
		[]string{"method", "path", "status_code"},
	)

	CPHTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "vpn",
			Subsystem: "cp",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency histogram in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
		[]string{"method", "path"},
	)

	CPGRPCMessagesReceivedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "vpn",
			Subsystem: "cp",
			Name:      "grpc_messages_received_total",
			Help:      "Total gRPC stream messages received from agents.",
		},
		[]string{"msg_type"},
	)

	CPGRPCMessagesSentTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "vpn",
			Subsystem: "cp",
			Name:      "grpc_messages_sent_total",
			Help:      "Total gRPC stream messages sent to agents.",
		},
		[]string{"msg_type"},
	)

	CPAuthFailuresTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "vpn",
			Subsystem: "cp",
			Name:      "auth_failures_total",
			Help:      "Total authentication rejections.",
		},
		[]string{"auth_type", "reason"},
	)

	// Control Plane USE Metrics
	CPDBPoolConnsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "vpn",
			Subsystem: "cp",
			Name:      "db_pool_conns_active",
			Help:      "Current active connections checked out from the database pool.",
		},
	)

	CPDBPoolConnsIdle = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "vpn",
			Subsystem: "cp",
			Name:      "db_pool_conns_idle",
			Help:      "Current idle connections in the database pool.",
		},
	)

	CPDBPoolConnsMax = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "vpn",
			Subsystem: "cp",
			Name:      "db_pool_conns_max",
			Help:      "Maximum configured connections in the database pool.",
		},
	)

	CPDBPoolWaitCountTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "vpn",
			Subsystem: "cp",
			Name:      "db_pool_wait_count_total",
			Help:      "Total number of times a connection had to wait to be acquired from the pool.",
		},
	)

	CPGRPCActiveAgents = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "vpn",
			Subsystem: "cp",
			Name:      "grpc_active_agents",
			Help:      "Current number of active connected agent bidirectional streams.",
		},
	)

	// Agent Edge RED & USE Metrics
	EdgePeersActive = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "vpn",
			Subsystem: "edge",
			Name:      "peers_active",
			Help:      "Count of active peers with handshake under 180s.",
		},
		[]string{"interface"},
	)

	EdgePeersTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "vpn",
			Subsystem: "edge",
			Name:      "peers_total",
			Help:      "Total configured peers on the interface.",
		},
		[]string{"interface"},
	)

	EdgeTrafficRxBytesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "vpn",
			Subsystem: "edge",
			Name:      "traffic_rx_bytes_total",
			Help:      "Total bytes received from client tunnels.",
		},
		[]string{"interface", "protocol"},
	)

	EdgeTrafficTxBytesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "vpn",
			Subsystem: "edge",
			Name:      "traffic_tx_bytes_total",
			Help:      "Total bytes transmitted to client tunnels.",
		},
		[]string{"interface", "protocol"},
	)

	EdgePacketsDroppedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "vpn",
			Subsystem: "edge",
			Name:      "packets_dropped_total",
			Help:      "Total packets dropped by kernel or proxy subsystems.",
		},
		[]string{"interface", "reason"},
	)

	EdgeGRPCStreamStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "vpn",
			Subsystem: "edge",
			Name:      "grpc_stream_status",
			Help:      "1 if agent bidirectional stream is active, 0 otherwise.",
		},
	)

	EdgeGRPCReconnectsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "vpn",
			Subsystem: "edge",
			Name:      "grpc_reconnects_total",
			Help:      "Total agent reconnection attempts to the control plane.",
		},
		[]string{"reason"},
	)
)
