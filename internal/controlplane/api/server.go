package api

import (
	"net/http"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
)

type Server struct {
	services *service.Services
	config   *config.Config
	httpSrv  *http.Server
}

func NewServer(services *service.Services, cfg *config.Config) *Server {
	return &Server{
		services: services,
		config:   cfg,
	}
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	s.httpSrv = &http.Server{
		Addr:    s.config.Server.HTTPAddr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		s.httpSrv.Shutdown(context.Background())
	}()

	return s.httpSrv.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}