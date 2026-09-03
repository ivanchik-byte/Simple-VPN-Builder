package main

import (
	"context"
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
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
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

	repos := store.NewRepositories(dbpool)

	blacklist := auth.NewRedisBlacklist(rdb)
	jwtManager := auth.NewJWTManager(cfg.Auth.JWTSecret, cfg.Auth.JWTAccessTTL, cfg.Auth.JWTRefreshTTL).WithBlacklist(blacklist)
	apiKeyManager := auth.NewAPIKeyManager(repos.Queries)
	passwordManager := auth.NewPasswordManager(cfg.Auth.BcryptCost)
	totpManager := auth.NewTOTPManager("Simple-VPN-Builder")

	services := service.NewServices(repos.Queries, jwtManager, apiKeyManager, passwordManager, cfg)

	grpcServer := cpgrpc.NewServer(services, cfg)
	go func() {
		if err := grpcServer.Start(ctx); err != nil && err != grpc.ErrServerStopped {
			log.ErrorContext(ctx, "gRPC server error", "error", err)
		}
	}()

	rateLimiter := middleware.NewRateLimiter(rdb, 120, time.Minute)
	authenticator := middleware.NewAuthenticator(jwtManager, apiKeyManager)
	auditService := middleware.NewAuditService(repos.AuditLogs)

	authHandler := handler.NewAuthHandler(repos.Admins, jwtManager, passwordManager, totpManager, blacklist)
	nodeHandler := handler.NewNodeHandler(repos.Nodes, auditService)
	userHandler := handler.NewUserHandler(repos.Users, repos.Plans, auditService)
	planHandler := handler.NewPlanHandler(repos.Plans, auditService)
	credHandler := handler.NewCredentialHandler(repos.Credentials, repos.Users, repos.Nodes, auditService)
	analyticsHandler := handler.NewAnalyticsHandler(repos.Traffic)
	adminHandler := handler.NewAdminHandler(repos.Admins, repos.APIKeys, apiKeyManager, passwordManager, auditService)

	handlers := api.Handlers{
		Auth:       authHandler,
		Node:       nodeHandler,
		User:       userHandler,
		Plan:       planHandler,
		Credential: credHandler,
		Analytics:  analyticsHandler,
		Admin:      adminHandler,
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
