package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/manager"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/metrics"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/agent/syncer"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "", "Path to config file")
	flag.Parse()

	cfg, err := config.LoadAgent(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	log := logger.Init(cfg.Log.Level, cfg.Log.Format, os.Stdout)
	log.Info("Starting VPN Builder Node Agent",
		"version", version,
		"commit", commit,
		"build_time", buildTime,
		"node_name", cfg.Agent.NodeName,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	grpcClient := grpc.NewClient(cfg)
	if err := grpcClient.Connect(ctx); err != nil {
		log.ErrorContext(ctx, "Failed to connect to control plane", "error", err)
		os.Exit(1)
	}
	defer grpcClient.Close()

	wgManager := manager.NewWireGuardManager(cfg.Agent.WireGuard)
	xrayManager := manager.NewXrayManager(cfg.Agent.Xray)

	configSyncer := syncer.New(grpcClient, wgManager, xrayManager, cfg)
	metricsCollector := metrics.NewCollector(wgManager, xrayManager, grpcClient, cfg)

	go configSyncer.Run(ctx)
	go metricsCollector.Run(ctx)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.InfoContext(ctx, "Shutting down agent...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	configSyncer.Stop(shutdownCtx)
	metricsCollector.Stop(shutdownCtx)
	wgManager.Stop(shutdownCtx)
	xrayManager.Stop(shutdownCtx)

	log.InfoContext(ctx, "Agent stopped gracefully")
}