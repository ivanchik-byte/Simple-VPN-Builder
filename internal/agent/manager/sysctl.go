package manager

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

// SysctlSetting represents a kernel sysctl parameter key-value pair.
type SysctlSetting struct {
	Key   string
	Value string
}

// RecommendedSysctlSettings defines network optimizations for high-throughput VPN nodes.
var RecommendedSysctlSettings = []SysctlSetting{
	{Key: "net.ipv4.ip_forward", Value: "1"},
	{Key: "net.ipv6.conf.all.forwarding", Value: "1"},
	{Key: "net.core.default_qdisc", Value: "fq"},
	{Key: "net.ipv4.tcp_congestion_control", Value: "bbr"},
	{Key: "net.core.rmem_max", Value: "67108864"},
	{Key: "net.core.wmem_max", Value: "67108864"},
	{Key: "net.core.rmem_default", Value: "16777216"},
	{Key: "net.core.wmem_default", Value: "16777216"},
	{Key: "net.core.netdev_max_backlog", Value: "10000"},
	{Key: "net.ipv4.conf.all.rp_filter", Value: "2"},
	{Key: "net.ipv4.conf.default.rp_filter", Value: "2"},
}

// SysctlApplier handles reading and writing kernel parameters.
type SysctlApplier interface {
	Apply(ctx context.Context, settings []SysctlSetting) error
}

type linuxSysctlApplier struct {
	basePath string
}

// NewSysctlApplier creates an applier writing directly to /proc/sys.
func NewSysctlApplier() SysctlApplier {
	return &linuxSysctlApplier{basePath: "/proc/sys"}
}

// Apply iterates over settings and writes them to /proc/sys files.
// If the filesystem is read-only or permission is denied, it logs a warning instead of aborting.
func (a *linuxSysctlApplier) Apply(ctx context.Context, settings []SysctlSetting) error {
	for _, s := range settings {
		path := a.procPath(s.Key)
		err := os.WriteFile(path, []byte(s.Value+"\n"), 0o644)
		if err != nil {
			logger.WarnContext(ctx, "failed to apply sysctl parameter (ignoring in non-privileged environment)",
				"key", s.Key, "target", s.Value, "error", err)
			continue
		}
		logger.DebugContext(ctx, "applied sysctl setting", "key", s.Key, "value", s.Value)
	}
	return nil
}

func (a *linuxSysctlApplier) procPath(key string) string {
	relPath := strings.ReplaceAll(key, ".", "/")
	return fmt.Sprintf("%s/%s", a.basePath, relPath)
}
