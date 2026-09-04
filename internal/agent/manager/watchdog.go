package manager

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

// InterfaceWatchdog monitors Linux network link events and self-heals deleted/down VPN interfaces and rules.
type InterfaceWatchdog struct {
	interfaces    []string
	healFn        func(ctx context.Context, iface string) error
	checkInterval time.Duration
	stopCh        chan struct{}
	stopOnce      sync.Once
	mu            sync.Mutex
	isStopped     bool
}

// NewInterfaceWatchdog creates a new InterfaceWatchdog.
func NewInterfaceWatchdog(interfaces []string, checkInterval time.Duration, healFn func(ctx context.Context, iface string) error) *InterfaceWatchdog {
	if checkInterval <= 0 {
		checkInterval = 10 * time.Second
	}
	return &InterfaceWatchdog{
		interfaces:    interfaces,
		healFn:        healFn,
		checkInterval: checkInterval,
		stopCh:        make(chan struct{}),
	}
}

// Start launches background link subscription and periodic health checks.
func (w *InterfaceWatchdog) Start(ctx context.Context) {
	linkUpdates := make(chan netlink.LinkUpdate)
	done := make(chan struct{})

	// Subscribe to netlink events if available (linux only)
	err := netlink.LinkSubscribe(linkUpdates, done)
	if err != nil {
		logger.WarnContext(ctx, "failed to subscribe to netlink link updates, falling back to polling", "error", err)
	}

	go func() {
		defer close(done)
		ticker := time.NewTicker(w.checkInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stopCh:
				return
			case update, ok := <-linkUpdates:
				if !ok {
					// Disable closed channel branch to keep ticker polling running (MIN-01)
					linkUpdates = nil
					continue
				}
				w.mu.Lock()
				stopped := w.isStopped
				w.mu.Unlock()
				if !stopped {
					w.handleLinkUpdate(ctx, update)
				}
			case <-ticker.C:
				w.mu.Lock()
				stopped := w.isStopped
				w.mu.Unlock()
				if !stopped {
					w.CheckAll(ctx)
				}
			}
		}
	}()
}

// Stop shuts down the watchdog and guarantees no further self-heal attempts (MAJ-01).
func (w *InterfaceWatchdog) Stop() {
	w.mu.Lock()
	w.isStopped = true
	w.mu.Unlock()

	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
}

func (w *InterfaceWatchdog) handleLinkUpdate(ctx context.Context, update netlink.LinkUpdate) {
	if update.Link == nil {
		return
	}
	attrs := update.Link.Attrs()
	if attrs == nil {
		return
	}

	for _, target := range w.interfaces {
		if attrs.Name == target {
			// If link is deleted (unix.RTM_DELLINK == 17) or FlagUp not set (NIT-01)
			if update.Header.Type == unix.RTM_DELLINK || (attrs.Flags&net.FlagUp == 0) {
				logger.WarnContext(ctx, "detected down or deleted interface, initiating self-heal", "interface", target)
				if w.healFn != nil {
					if err := w.healFn(ctx, target); err != nil {
						logger.ErrorContext(ctx, "failed to self-heal interface", "interface", target, "error", err)
					}
				}
			}
		}
	}
}

// CheckAll explicitly checks if each monitored interface exists and is UP.
func (w *InterfaceWatchdog) CheckAll(ctx context.Context) {
	for _, ifaceName := range w.interfaces {
		link, err := netlink.LinkByName(ifaceName)
		if err != nil || link == nil || (link.Attrs().Flags&net.FlagUp == 0) {
			logger.WarnContext(ctx, "watchdog found interface missing or down", "interface", ifaceName, "error", err)
			if w.healFn != nil {
				if healErr := w.healFn(ctx, ifaceName); healErr != nil {
					logger.ErrorContext(ctx, "watchdog failed to heal interface", "interface", ifaceName, "error", healErr)
				} else {
					logger.InfoContext(ctx, "watchdog successfully healed interface", "interface", ifaceName)
				}
			}
		}
	}
}
