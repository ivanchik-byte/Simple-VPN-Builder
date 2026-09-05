package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	internalgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/metrics"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/tracer"
)

func TestHardening_PrometheusMetrics_Registration(t *testing.T) {
	// Increment metrics
	metrics.CPHTTPRequestsTotal.WithLabelValues("GET", "/healthz", "200").Inc()
	metrics.CPHTTPRequestDuration.WithLabelValues("GET", "/healthz").Observe(0.012)
	metrics.CPGRPCActiveAgents.Set(5)
	metrics.EdgePeersActive.WithLabelValues("wg0").Set(42)
	metrics.EdgeTrafficRxBytesTotal.WithLabelValues("wg0", "wireguard").Add(1048576)

	assert.NotNil(t, metrics.CPHTTPRequestsTotal)
	assert.NotNil(t, metrics.CPHTTPRequestDuration)
	assert.NotNil(t, metrics.CPGRPCActiveAgents)
	assert.NotNil(t, metrics.EdgePeersActive)
}

func TestHardening_StreamRateLimitInterceptor(t *testing.T) {
	interceptor := internalgrpc.StreamRateLimitInterceptor(rate.Limit(5), 2)
	require.NotNil(t, interceptor)

	// Mock server stream
	mockStream := &mockServerStream{
		recvMsgFunc: func(m any) error {
			return nil
		},
	}

	info := &grpc.StreamServerInfo{
		FullMethod:     "/vpnbuilder.agent.v1.AgentService/Connect",
		IsClientStream: true,
		IsServerStream: true,
	}

	err := interceptor(nil, mockStream, info, func(srv any, ss grpc.ServerStream) error {
		// 1st and 2nd messages within burst
		require.NoError(t, ss.RecvMsg(nil))
		require.NoError(t, ss.RecvMsg(nil))

		// 3rd rapid message should trigger rate limit (exceeds burst 2)
		err := ss.RecvMsg(nil)
		require.Error(t, err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(err))
		return nil
	})

	require.NoError(t, err)
}

func TestHardening_OTelTracingAndLoggerCorrelation(t *testing.T) {
	tp, err := tracer.InitTracer("test-service", 1.0)
	require.NoError(t, err)
	defer func() {
		_ = tracer.ShutdownTracer(context.Background(), tp)
	}()

	tr := tp.Tracer("test-tracer")
	ctx, span := tr.Start(context.Background(), "test-operation")
	defer span.End()

	sc := span.SpanContext()
	require.True(t, sc.IsValid())

	// Log with context and verify no panics
	logger.InfoContext(ctx, "testing otel correlated log entry", "key", "val")
}

type mockServerStream struct {
	grpc.ServerStream
	recvMsgFunc func(m any) error
}

func (m *mockServerStream) RecvMsg(msg any) error {
	if m.recvMsgFunc != nil {
		return m.recvMsgFunc(msg)
	}
	return nil
}

func (m *mockServerStream) SendMsg(msg any) error {
	return nil
}

func (m *mockServerStream) Context() context.Context {
	return context.Background()
}
