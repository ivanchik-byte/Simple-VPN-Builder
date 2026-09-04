package manager

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/vishvananda/netlink"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

// FirewallConfig encapsulates parameters for VPN network masquerading and packet filtering.
type FirewallConfig struct {
	Interface         string
	SubnetV4          string
	SubnetV6          string
	OutboundInterface string
	FwMark            int
}

// FirewallManager controls host packet filtering, NAT, and TCP MSS clamping.
type FirewallManager interface {
	Apply(ctx context.Context) error
	Clear(ctx context.Context) error
	Backend() string
}

type nftablesFirewall struct {
	cfg     FirewallConfig
	execCmd func(ctx context.Context, name string, arg ...string) *exec.Cmd
}

// DetectDefaultOutboundInterface detects the system default route's outbound interface (MIN-03).
func DetectDefaultOutboundInterface() (string, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return "eth0", err
	}
	for _, r := range routes {
		if r.Dst == nil || r.Dst.IP.IsUnspecified() {
			link, err := netlink.LinkByIndex(r.LinkIndex)
			if err == nil && link.Attrs() != nil && link.Attrs().Name != "" {
				return link.Attrs().Name, nil
			}
		}
	}
	return "eth0", nil
}

// NewFirewallManager detects available host tools and instantiates the appropriate firewall manager.
func NewFirewallManager(cfg FirewallConfig) FirewallManager {
	if cfg.OutboundInterface == "" || cfg.OutboundInterface == "eth0" {
		if iface, err := DetectDefaultOutboundInterface(); err == nil && iface != "" {
			cfg.OutboundInterface = iface
		}
	}

	if _, err := exec.LookPath("nft"); err == nil {
		return &nftablesFirewall{
			cfg:     cfg,
			execCmd: exec.CommandContext,
		}
	}
	return &iptablesFirewall{
		cfg:     cfg,
		execCmd: exec.CommandContext,
	}
}

func (f *nftablesFirewall) Backend() string {
	return "nftables"
}

// Apply deploys an isolated 'table inet vpnbuilder' with masquerading, dynamic MSS clamping, and PMTUD preservation.
// It uses atomic replacement (delete table if exists) to avoid duplicate rules (MAJ-02).
func (f *nftablesFirewall) Apply(ctx context.Context) error {
	rules := f.buildRuleset()

	cmd := f.execCmd(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(rules)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to apply nftables ruleset: %w, output: %s", err, string(output))
	}

	logger.InfoContext(ctx, "applied nftables ruleset for VPN interface",
		"table", "vpnbuilder", "interface", f.cfg.Interface, "outbound", f.cfg.OutboundInterface)
	return nil
}

// Clear flushes and removes the vpnbuilder nftables table.
func (f *nftablesFirewall) Clear(ctx context.Context) error {
	cmd := f.execCmd(ctx, "nft", "delete", "table", "inet", "vpnbuilder")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "No such file or directory") {
			return nil
		}
		return fmt.Errorf("delete nftables table: %w", err)
	}
	return nil
}

func (f *nftablesFirewall) buildRuleset() string {
	var b strings.Builder

	// Idempotent: flush and delete previous table before creating fresh (MAJ-02)
	b.WriteString("table inet vpnbuilder\n")
	b.WriteString("delete table inet vpnbuilder\n")
	b.WriteString("table inet vpnbuilder {\n")

	// 1. NAT chain for Masquerade
	b.WriteString("    chain postrouting {\n")
	b.WriteString("        type nat hook postrouting priority srcnat; policy accept;\n")
	if f.cfg.SubnetV4 != "" {
		b.WriteString(fmt.Sprintf("        ip saddr %s oifname \"%s\" masquerade\n", f.cfg.SubnetV4, f.cfg.OutboundInterface))
	}
	if f.cfg.SubnetV6 != "" {
		b.WriteString(fmt.Sprintf("        ip6 saddr %s oifname \"%s\" masquerade\n", f.cfg.SubnetV6, f.cfg.OutboundInterface))
	}
	b.WriteString("    }\n\n")

	// 2. Filter Forward chain (Permit VPN traffic, preserve ICMP for PMTUD)
	b.WriteString("    chain forward {\n")
	b.WriteString("        type filter hook forward priority filter; policy accept;\n")
	b.WriteString(fmt.Sprintf("        iifname \"%s\" accept\n", f.cfg.Interface))
	b.WriteString(fmt.Sprintf("        oifname \"%s\" ct state related,established accept\n", f.cfg.Interface))
	b.WriteString("        icmp type { echo-request, echo-reply, destination-unreachable, time-exceeded } accept\n")
	b.WriteString("        icmpv6 type { echo-request, echo-reply, destination-unreachable, packet-too-big, time-exceeded } accept\n")
	b.WriteString("    }\n\n")

	// 3. Dynamic TCP MSS Clamping to prevent MTU black-holes
	b.WriteString("    chain mangle_forward {\n")
	b.WriteString("        type filter hook forward priority mangle; policy accept;\n")
	b.WriteString(fmt.Sprintf("        iifname \"%s\" tcp flags syn / syn,rst tcp option maxseg size set rt mtu\n", f.cfg.Interface))
	b.WriteString(fmt.Sprintf("        oifname \"%s\" tcp flags syn / syn,rst tcp option maxseg size set rt mtu\n", f.cfg.Interface))
	b.WriteString("    }\n")
	b.WriteString("}\n")

	return b.String()
}

type iptablesFirewall struct {
	cfg     FirewallConfig
	execCmd func(ctx context.Context, name string, arg ...string) *exec.Cmd
}

func (f *iptablesFirewall) Backend() string {
	return "iptables"
}

func (f *iptablesFirewall) Apply(ctx context.Context) error {
	// First clear pre-existing rules to maintain idempotency
	_ = f.Clear(ctx)

	// Masquerade
	if f.cfg.SubnetV4 != "" {
		cmd := f.execCmd(ctx, "iptables", "-t", "nat", "-A", "POSTROUTING", "-s", f.cfg.SubnetV4, "-o", f.cfg.OutboundInterface, "-j", "MASQUERADE")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("iptables masquerade failed: %w, output: %s", err, string(out))
		}
	}

	// Forwarding
	_ = f.execCmd(ctx, "iptables", "-A", "FORWARD", "-i", f.cfg.Interface, "-j", "ACCEPT").Run()
	_ = f.execCmd(ctx, "iptables", "-A", "FORWARD", "-o", f.cfg.Interface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run()

	// Dynamic MSS Clamping
	_ = f.execCmd(ctx, "iptables", "-t", "mangle", "-A", "FORWARD", "-p", "tcp", "--tcp-flags", "SYN,RST", "SYN", "-j", "TCPMSS", "--clamp-mss-to-pmtu").Run()

	return nil
}

func (f *iptablesFirewall) Clear(ctx context.Context) error {
	if f.cfg.SubnetV4 != "" {
		_ = f.execCmd(ctx, "iptables", "-t", "nat", "-D", "POSTROUTING", "-s", f.cfg.SubnetV4, "-o", f.cfg.OutboundInterface, "-j", "MASQUERADE").Run()
	}
	_ = f.execCmd(ctx, "iptables", "-D", "FORWARD", "-i", f.cfg.Interface, "-j", "ACCEPT").Run()
	_ = f.execCmd(ctx, "iptables", "-D", "FORWARD", "-o", f.cfg.Interface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
	_ = f.execCmd(ctx, "iptables", "-t", "mangle", "-D", "FORWARD", "-p", "tcp", "--tcp-flags", "SYN,RST", "SYN", "-j", "TCPMSS", "--clamp-mss-to-pmtu").Run()
	return nil
}
