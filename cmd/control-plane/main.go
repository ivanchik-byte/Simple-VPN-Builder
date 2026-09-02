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
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
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

	dbpool, err := pgxpool.New(ctx, cfg.Database.DSN)
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

	queries := store.New(dbpool)
	repos := store.NewRepositories(queries)

	jwtManager := auth.NewJWTManager(cfg.Auth.JWTSecret, cfg.Auth.JWTAccessTTL, cfg.Auth.JWTRefreshTTL)
	apiKeyManager := auth.NewAPIKeyManager(repos.APIKeyRepo)
	passwordManager := auth.NewPasswordManager(cfg.Auth.BcryptCost)

	services := service.NewServices(repos, jwtManager, apiKeyManager, passwordManager, cfg)

	grpcServer := grpc.NewServer(services, cfg)
	go func() {
		if err := grpcServer.Start(ctx); err != nil && err != http.ErrServerClosed {
			log.ErrorContext(ctx, "gRPC server error", "error", err)
		}
	}()

	httpServer := api.NewServer(services, cfg)
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

	grpcServer.Stop(shutdownCtx)
	httpServer.Stop(shutdownCtx)

	log.InfoContext(ctx, "Servers stopped gracefully")
}