package manager

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

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

// NewFirewallManager detects available host tools and instantiates the appropriate firewall manager.
func NewFirewallManager(cfg FirewallConfig) FirewallManager {
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
func (f *nftablesFirewall) Apply(ctx context.Context) error {
	rules := f.buildRuleset()

	cmd := f.execCmd(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(rules)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.WarnContext(ctx, "failed to apply nftables ruleset (ignoring in non-privileged environment)",
			"error", err, "output", string(output))
		return nil
	}

	logger.InfoContext(ctx, "applied nftables ruleset for VPN interface",
		"table", "vpnbuilder", "interface", f.cfg.Interface)
	return nil
}

// Clear flushes and removes the vpnbuilder nftables table.
func (f *nftablesFirewall) Clear(ctx context.Context) error {
	cmd := f.execCmd(ctx, "nft", "delete", "table", "inet", "vpnbuilder")
	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "No such file or directory") {
		logger.DebugContext(ctx, "nftables table deletion notice", "output", string(output), "error", err)
	}
	return nil
}

func (f *nftablesFirewall) buildRuleset() string {
	var b strings.Builder

	b.WriteString("table inet vpnbuilder {\n")

	// Filter / Forwarding chain
	b.WriteString("  chain forward {\n")
	b.WriteString("    type filter hook forward priority filter; policy accept;\n")
	b.WriteString(fmt.Sprintf("    iifname \"%s\" accept\n", f.cfg.Interface))
	b.WriteString(fmt.Sprintf("    oifname \"%s\" ct state related,established accept\n", f.cfg.Interface))
	// Allow ICMP PMTUD
	b.WriteString("    ip protocol icmp icmp type destination-unreachable accept\n")
	b.WriteString("    ip6 nexthdr ipv6-icmp icmpv6 type packet-too-big accept\n")
	b.WriteString("  }\n")

	// Mangle chain: Dynamic TCP MSS Clamping
	b.WriteString("  chain mangle_forward {\n")
	b.WriteString("    type filter hook forward priority mangle; policy accept;\n")
	b.WriteString(fmt.Sprintf("    iifname \"%s\" tcp flags syn / syn,rst tcp option maxseg size set rt mtu\n", f.cfg.Interface))
	b.WriteString(fmt.Sprintf("    oifname \"%s\" tcp flags syn / syn,rst tcp option maxseg size set rt mtu\n", f.cfg.Interface))
	b.WriteString("  }\n")

	// NAT chain: Source NAT / Masquerade
	b.WriteString("  chain postrouting {\n")
	b.WriteString("    type nat hook postrouting priority srcnat; policy accept;\n")
	if f.cfg.SubnetV4 != "" {
		b.WriteString(fmt.Sprintf("    ip saddr %s oifname != \"%s\" masquerade\n", f.cfg.SubnetV4, f.cfg.Interface))
	}
	if f.cfg.SubnetV6 != "" {
		b.WriteString(fmt.Sprintf("    ip6 saddr %s oifname != \"%s\" masquerade\n", f.cfg.SubnetV6, f.cfg.Interface))
	}
	b.WriteString("  }\n")

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
	commands := [][]string{
		// Forwarding
		{"iptables", "-A", "FORWARD", "-i", f.cfg.Interface, "-j", "ACCEPT"},
		{"iptables", "-A", "FORWARD", "-o", f.cfg.Interface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
		// MSS Clamping
		{"iptables", "-t", "mangle", "-A", "FORWARD", "-p", "tcp", "--tcp-flags", "SYN,RST", "SYN", "-j", "TCPMSS", "--clamp-mss-to-pmtu"},
		// Masquerade
		{"iptables", "-t", "nat", "-A", "POSTROUTING", "-s", f.cfg.SubnetV4, "!", "-o", f.cfg.Interface, "-j", "MASQUERADE"},
	}

	for _, args := range commands {
		cmd := f.execCmd(ctx, args[0], args[1:]...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			logger.WarnContext(ctx, "failed to apply iptables command (ignoring in non-privileged environment)",
				"cmd", strings.Join(args, " "), "error", err, "output", string(output))
		}
	}
	return nil
}

func (f *iptablesFirewall) Clear(ctx context.Context) error {
	commands := [][]string{
		{"iptables", "-D", "FORWARD", "-i", f.cfg.Interface, "-j", "ACCEPT"},
		{"iptables", "-D", "FORWARD", "-o", f.cfg.Interface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
		{"iptables", "-t", "mangle", "-D", "FORWARD", "-p", "tcp", "--tcp-flags", "SYN,RST", "SYN", "-j", "TCPMSS", "--clamp-mss-to-pmtu"},
		{"iptables", "-t", "nat", "-D", "POSTROUTING", "-s", f.cfg.SubnetV4, "!", "-o", f.cfg.Interface, "-j", "MASQUERADE"},
	}

	for _, args := range commands {
		cmd := f.execCmd(ctx, args[0], args[1:]...)
		_ = cmd.Run()
	}
	return nil
}
