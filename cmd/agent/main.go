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
	sharedmetrics "github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
		Interface: primaryIface,
		SubnetV4:  cfg.Agent.WireGuard.SubnetV4,
		SubnetV6:  cfg.Agent.WireGuard.SubnetV6,
	})
	if err := fw.Apply(ctx); err != nil {
		log.WarnContext(ctx, "firewall rules could not be applied", "backend", fw.Backend(), "error", err)
	}

	// 3. Deterministic Node WireGuard Key Loading (CRIT-02)
	nodeKey, err := manager.LoadOrGeneratePrivateKey("/etc/vpnbuilder/wireguard.key")
	if err != nil {
		log.ErrorContext(ctx, "failed to load or generate node private key", "error", err)
		os.Exit(1)
	}

	wgManager, err := manager.NewWireGuardManager(&cfg.Agent.WireGuard)
	if err != nil {
		log.ErrorContext(ctx, "Failed to init WireGuard manager", "error", err)
		os.Exit(1)
	}
	xrayManager := manager.NewXrayManager(&cfg.Agent.Xray)
	realitySettings, err := manager.LoadOrGenerateRealitySettings("/etc/vpnbuilder/reality.json", "swdist.apple.com:443", []string{"swdist.apple.com"})
	if err != nil {
		log.WarnContext(ctx, "failed to load or generate reality settings", "error", err)
	} else {
		xrayManager.SetRealitySettings(realitySettings)
	}
	daemonCfg, err := xrayManager.BuildDaemonConfig(443)
	if err == nil {
		_ = xrayManager.WriteConfig(ctx, daemonCfg)
	}
	if err := xrayManager.Start(ctx); err != nil {
		log.WarnContext(ctx, "failed to start xray manager", "error", err)
	}

	// Ensure primary interface exists with deterministic key and port
	dev, err := wgManager.EnsureInterfaceWithKey(ctx, primaryIface, nodeKey, 51820)
	if err != nil {
		log.WarnContext(ctx, "could not pre-bind wireguard interface with key", "error", err)
	}

	// 4. gRPC Client & Routing
	grpcClient := grpc.NewClient(cfg)
	if dev != nil {
		grpcClient.SetPublicKey(dev.PublicKey.String())
	} else {
		grpcClient.SetPublicKey(nodeKey.PublicKey().String())
	}

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
		_, _ = wgManager.EnsureInterfaceWithKey(c, iface, nodeKey, 51820)
		return fw.Apply(c)
	})
	watchdog.Start(ctx)

	// 6. Metrics and Syncer
	metricsCollector := metrics.NewCollector(wgManager, xrayManager, grpcClient, cfg)
	go configSyncer.Run(ctx)
	go metricsCollector.Run(ctx)

	// 7. Health and Readiness HTTP Probes & Prometheus Metrics
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	healthMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	healthMux.Handle("/metrics", promhttp.Handler())

	// Set initial gRPC stream status metric
	sharedmetrics.EdgeGRPCStreamStatus.Set(1)

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

	// 1. Stop watchdog FIRST to prevent resurrection during teardown (MAJ-01)
	watchdog.Stop()

	// 2. Teardown subsystems
	_ = healthServer.Shutdown(shutdownCtx)
	_ = configSyncer.Stop(shutdownCtx)
	_ = metricsCollector.Stop(shutdownCtx)
	_ = fw.Clear(shutdownCtx)
	_ = wgManager.Stop(shutdownCtx)
	_ = xrayManager.Stop(shutdownCtx)

	log.InfoContext(ctx, "Agent stopped gracefully")
}
