package grpc

import (
	"context"
	"net"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

// Server coordinates the control plane gRPC server listeners and registered RPC services.
type Server struct {
	config       *config.Config
	agentService *AgentServiceServer
	grpcSrv      *grpc.Server
}

// NewServer initializes the gRPC Server with keepalive settings, concurrency bounds, and registers AgentServiceServer.
func NewServer(cfg *config.Config, agentService *AgentServiceServer, opts ...grpc.ServerOption) *Server {
	defaultOpts := []grpc.ServerOption{
		grpc.MaxConcurrentStreams(4),
		grpc.MaxRecvMsgSize(4 * 1024 * 1024), // 4 MB
		grpc.MaxSendMsgSize(4 * 1024 * 1024),
		grpc.StreamInterceptor(StreamRateLimitInterceptor(20, 40)), // Token bucket: 20 msgs/sec, burst 40
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     15 * time.Minute,
			MaxConnectionAge:      2 * time.Hour,
			MaxConnectionAgeGrace: 5 * time.Minute,
			Time:                  30 * time.Second,
			Timeout:               5 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	finalOpts := append(defaultOpts, opts...)
	grpcSrv := grpc.NewServer(finalOpts...)

	if agentService != nil {
		agentv1.RegisterAgentServiceServer(grpcSrv, agentService)
	}

	return &Server{
		config:       cfg,
		agentService: agentService,
		grpcSrv:      grpcSrv,
	}
}

// Start opens a TCP listener on the configured gRPC address and serves incoming RPC connections.
func (s *Server) Start(ctx context.Context) error {
	lis, err := net.Listen("tcp", s.config.Server.GRPCAddr)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		s.grpcSrv.GracefulStop()
	}()

	return s.grpcSrv.Serve(lis)
}

// Stop gracefully stops the gRPC server.
func (s *Server) Stop(_ context.Context) error {
	s.grpcSrv.GracefulStop()
	return nil
}

// GRPCServer returns the underlying raw *grpc.Server instance.
func (s *Server) GRPCServer() *grpc.Server {
	return s.grpcSrv
}
