package grpc

import (
	"context"
	"net"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"google.golang.org/grpc"
)

type Server struct {
	services *service.Services
	config   *config.Config
	grpcSrv  *grpc.Server
}

func NewServer(services *service.Services, cfg *config.Config) *Server {
	return &Server{
		services: services,
		config:   cfg,
		grpcSrv:  grpc.NewServer(),
	}
}

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

func (s *Server) Stop(ctx context.Context) error {
	s.grpcSrv.GracefulStop()
	return nil
}
