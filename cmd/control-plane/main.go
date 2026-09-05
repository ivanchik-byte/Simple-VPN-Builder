package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/handler"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	cpgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/web"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	log := logger.Init(cfg.Log.Level, cfg.Log.Format, os.Stdout)
	log.Info("Starting VPN Builder Control Plane",
		"version", version,
		"commit", commit,
		"build_time", buildTime,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poolCfg, err := pgxpool.ParseConfig(cfg.Database.DSN)
	if err != nil {
		log.ErrorContext(ctx, "Failed to parse database DSN", "error", err)
		os.Exit(1)
	}
	if cfg.Database.MaxOpenConns > 0 {
		poolCfg.MaxConns = int32(cfg.Database.MaxOpenConns)
	}
	if cfg.Database.MaxIdleConns > 0 {
		poolCfg.MinConns = int32(cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.Database.ConnMaxLifetime
	}
	if cfg.Database.ConnMaxIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.Database.ConnMaxIdleTime
	}

	dbpool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		log.ErrorContext(ctx, "Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbpool.Close()

	if err := dbpool.Ping(ctx); err != nil {
		log.ErrorContext(ctx, "Failed to ping database", "error", err)
		os.Exit(1)
	}
	log.InfoContext(ctx, "Database connected")

	if err := store.RunMigrations(dbpool); err != nil {
		log.ErrorContext(ctx, "Failed to run database migrations", "error", err)
		os.Exit(1)
	}
	log.InfoContext(ctx, "Database migrations applied successfully")

	// Seed default admin if no admins exist yet
	{
		tmpRepos := store.NewRepositories(dbpool)
		if admins, err := tmpRepos.Admins.List(ctx); err == nil && len(admins) == 0 {
			const defaultEmail = "admin@vpnbuilder.local"
			const defaultPassword = "Admin1234!"
			tmpPM := auth.NewPasswordManager(12)
			if hash, err := tmpPM.Hash(defaultPassword); err == nil {
				_, _ = tmpRepos.Admins.Create(ctx, store.CreateAdminParams{
					Email:        defaultEmail,
					PasswordHash: hash,
					Role:         pgtype.Text{String: "superadmin", Valid: true},
				})
				log.InfoContext(ctx, "Default admin created",
					"email", defaultEmail,
					"password", defaultPassword,
					"note", "Change this password immediately after first login")
			}
		}
	}

	repos := store.NewRepositories(dbpool)

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.ErrorContext(ctx, "Failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	log.InfoContext(ctx, "Redis connected")

	blacklist := auth.NewRedisBlacklist(rdb)
	jwtManager := auth.NewJWTManager(cfg.Auth.JWTSecret, cfg.Auth.JWTAccessTTL, cfg.Auth.JWTRefreshTTL).WithBlacklist(blacklist)
	apiKeyManager := auth.NewAPIKeyManager(repos.Queries)
	passwordManager := auth.NewPasswordManager(cfg.Auth.BcryptCost)
	totpManager := auth.NewTOTPManager("Simple-VPN-Builder")

	services := service.NewServices(repos.Queries, jwtManager, apiKeyManager, passwordManager, cfg)

	sessionMgr := cpgrpc.NewSessionManager()
	configBuilder := service.NewConfigBuilder(repos.Nodes, repos.Credentials, repos.Users)
	agentService := cpgrpc.NewAgentServiceServer(repos.Nodes, repos.Users, repos.Credentials, repos.Traffic, configBuilder, sessionMgr)

	var grpcOpts []grpc.ServerOption
	certFile := cfg.Server.TLSCert
	keyFile := cfg.Server.TLSKey
	if certFile == "" && cfg.CA.CertFile != "" && cfg.CA.KeyFile != "" {
		certFile = cfg.CA.CertFile
		keyFile = cfg.CA.KeyFile
	}

	if certFile != "" && keyFile != "" {
		serverCert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			log.ErrorContext(ctx, "Failed to load gRPC server certificate", "error", err)
			os.Exit(1)
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{serverCert},
			MinVersion:   tls.VersionTLS12,
		}

		if cfg.CA.CertFile != "" {
			caCert, err := os.ReadFile(cfg.CA.CertFile)
			if err != nil {
				log.ErrorContext(ctx, "Failed to read CA certificate for mTLS", "error", err)
				os.Exit(1)
			}
			caPool := x509.NewCertPool()
			if caPool.AppendCertsFromPEM(caCert) {
				tlsConfig.ClientCAs = caPool
				tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
			}
		}

		grpcOpts = append(grpcOpts, grpc.Creds(credentials.NewTLS(tlsConfig)))
		log.InfoContext(ctx, "gRPC server configured with mTLS")
	}

	grpcServer := cpgrpc.NewServer(cfg, agentService, grpcOpts...)
	go func() {
		if err := grpcServer.Start(ctx); err != nil && err != grpc.ErrServerStopped {
			log.ErrorContext(ctx, "gRPC server error", "error", err)
		}
	}()

	rateLimiter := middleware.NewRateLimiter(rdb, 120, time.Minute)
	rateLimiter.StartJanitor(ctx, 5*time.Minute)
	authenticator := middleware.NewAuthenticator(jwtManager, apiKeyManager)
	auditService := middleware.NewAuditService(repos.AuditLogs)

	authHandler := handler.NewAuthHandler(repos.Admins, jwtManager, passwordManager, totpManager, blacklist)
	nodeHandler := handler.NewNodeHandler(repos.Nodes, auditService)
	userHandler := handler.NewUserHandler(repos.Users, repos.Plans, auditService)
	credProvisioner := service.NewCredentialProvisioner(repos.Credentials, repos.Nodes, repos.Users)
	userHandler.SetProvisioner(credProvisioner)
	planHandler := handler.NewPlanHandler(repos.Plans, auditService)
	credHandler := handler.NewCredentialHandler(repos.Credentials, repos.Users, repos.Nodes, auditService)
	billingHandler := handler.NewBillingHandler(repos.Billing, repos.Users, repos.Plans, auditService)
	analyticsHandler := handler.NewAnalyticsHandler(repos.Traffic)
	adminHandler := handler.NewAdminHandler(repos.Admins, repos.APIKeys, apiKeyManager, passwordManager, auditService)
	subService := service.NewSubscriptionService(repos.Users, repos.Credentials, repos.Nodes)
	subHandler := handler.NewSubscriptionHandler(subService)
	systemHandler := handler.NewSystemHandler()

	tmplEngine, err := web.NewTemplateEngine()
	if err != nil {
		log.ErrorContext(ctx, "Failed to initialize web template engine", "error", err)
		os.Exit(1)
	}
	webHandler := web.NewHandler(tmplEngine, repos, jwtManager, passwordManager, totpManager, apiKeyManager, sessionMgr)
	webHandler.SetProvisioner(credProvisioner)

	tgBotToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	var tgSender service.TelegramSender
	if tgBotToken != "" {
		tgSender = service.NewHTTPTelegramSender(tgBotToken, "")
	}
	broadcastService := service.NewBroadcastService(repos.Billing, repos.Users, tgSender, log.Logger)
	billingHandler.SetBroadcastService(broadcastService)
	webHandler.SetBroadcastService(broadcastService)

	handlers := api.Handlers{
		Auth:         authHandler,
		Node:         nodeHandler,
		User:         userHandler,
		Plan:         planHandler,
		Credential:   credHandler,
		Billing:      billingHandler,
		Analytics:    analyticsHandler,
		Admin:        adminHandler,
		Subscription: subHandler,
		System:       systemHandler,
		Web:          webHandler,
	}

	router := api.NewRouter(cfg, handlers, authenticator, rateLimiter, dbpool, rdb)
	httpServer := api.NewServer(services, cfg, router)

	go func() {
		if err := httpServer.Start(ctx); err != nil && err != http.ErrServerClosed {
			log.ErrorContext(ctx, "HTTP server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.InfoContext(ctx, "Shutting down servers...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	_ = grpcServer.Stop(shutdownCtx)
	_ = httpServer.Stop(shutdownCtx)

	log.InfoContext(ctx, "Servers stopped gracefully")
}
