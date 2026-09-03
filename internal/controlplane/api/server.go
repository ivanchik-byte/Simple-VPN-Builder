package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
)

type Server struct {
	services *service.Services
	config   *config.Config
	httpSrv  *http.Server
	mu       sync.Mutex
}

func NewServer(services *service.Services, cfg *config.Config, handler http.Handler) *Server {
	s := &Server{
		services: services,
		config:   cfg,
	}
	s.httpSrv = &http.Server{
		Addr:              cfg.Server.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s
}

type HealthResponse struct {
	Status   string `json:"status"`
	Postgres string `json:"postgres"`
	Redis    string `json:"redis"`
}

func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if s.httpSrv != nil {
			_ = s.httpSrv.Shutdown(context.Background())
		}
		s.mu.Unlock()
	}()

	return s.httpSrv.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.httpSrv != nil {
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
}
