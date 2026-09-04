package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
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

	// 1. Linux Kernel Sysctl Tuning
	sysctl := manager.NewSysctlApplier()
	if err := sysctl.Apply(ctx, manager.RecommendedSysctlSettings); err != nil {
		log.WarnContext(ctx, "failed applying kernel sysctl optimizations", "error", err)
	}

	// 2. Linux Firewall Initialization (nftables or iptables via NewFirewallManager)
	primaryIface := fmt.Sprintf("%s0", cfg.Agent.WireGuard.InterfacePrefix)
	fw := manager.NewFirewallManager(manager.FirewallConfig{
		Interface:         primaryIface,
		SubnetV4:          cfg.Agent.WireGuard.SubnetV4,
		SubnetV6:          cfg.Agent.WireGuard.SubnetV6,
		OutboundInterface: "eth0",
	})
	if err := fw.Apply(ctx); err != nil {
		log.WarnContext(ctx, "firewall rules could not be applied", "backend", fw.Backend(), "error", err)
	}

	// 3. Managers
	wgManager, err := manager.NewWireGuardManager(&cfg.Agent.WireGuard)
	if err != nil {
		log.ErrorContext(ctx, "Failed to init WireGuard manager", "error", err)
		os.Exit(1)
	}
	xrayManager := manager.NewXrayManager(&cfg.Agent.Xray)

	// 4. gRPC Client & Routing
	grpcClient := grpc.NewClient(cfg)
	configSyncer := syncer.New(grpcClient, wgManager, xrayManager, cfg)
	executor := manager.NewCommandExecutor(wgManager, func(c context.Context) error {
		return nil
	})
	grpcClient.SetHandlers(configSyncer, executor)

	if err := grpcClient.Connect(ctx); err != nil {
		log.WarnContext(ctx, "Failed initial connect to control plane, supervisor will retry", "error", err)
	}
	grpcClient.StartReconnectSupervisor(ctx)
	defer grpcClient.Close()

	// 5. Watchdog for link self-healing
	watchdog := manager.NewInterfaceWatchdog([]string{primaryIface}, 15*time.Second, func(c context.Context, iface string) error {
		_ = wgManager.EnsureInterface(c, iface)
		return fw.Apply(c)
	})
	watchdog.Start(ctx)
	defer watchdog.Stop()

	// 6. Metrics and Syncer
	metricsCollector := metrics.NewCollector(wgManager, xrayManager, grpcClient, cfg)
	go configSyncer.Run(ctx)
	go metricsCollector.Run(ctx)

	// 7. Health and Readiness HTTP Probes
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	healthMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	healthServer := &http.Server{
		Addr:              ":8081",
		Handler:           healthMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.ErrorContext(ctx, "health HTTP server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.InfoContext(ctx, "Shutting down agent...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	_ = healthServer.Shutdown(shutdownCtx)
	_ = configSyncer.Stop(shutdownCtx)
	_ = metricsCollector.Stop(shutdownCtx)
	_ = wgManager.Stop(shutdownCtx)
	_ = xrayManager.Stop(shutdownCtx)
	_ = fw.Clear(shutdownCtx)

	log.InfoContext(ctx, "Agent stopped gracefully")
}
