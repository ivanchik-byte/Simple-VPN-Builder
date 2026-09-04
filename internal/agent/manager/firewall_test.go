package manager

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNftablesFirewall_BuildRuleset(t *testing.T) {
	cfg := FirewallConfig{
		Interface:         "wg0",
		SubnetV4:          "10.8.0.0/24",
		SubnetV6:          "fd00::/64",
		OutboundInterface: "eth0",
	}

	fw := &nftablesFirewall{cfg: cfg}
	rules := fw.buildRuleset()

	assert.Contains(t, rules, "table inet vpnbuilder")
	assert.Contains(t, rules, "delete table inet vpnbuilder")
	assert.Contains(t, rules, "iifname \"wg0\" accept")
	assert.Contains(t, rules, "tcp flags syn / syn,rst tcp option maxseg size set rt mtu")
	assert.Contains(t, rules, "ip saddr 10.8.0.0/24 oifname \"eth0\" masquerade")
	assert.Contains(t, rules, "ip6 saddr fd00::/64 oifname \"eth0\" masquerade")
	assert.Contains(t, rules, "icmp type { echo-request, echo-reply, destination-unreachable, time-exceeded } accept")
}

func TestNftablesFirewall_ApplyAndClear_Mock(t *testing.T) {
	cfg := FirewallConfig{
		Interface:         "wg0",
		SubnetV4:          "10.8.0.0/24",
		OutboundInterface: "eth0",
	}

	executedCommands := make([]string, 0)
	mockExec := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		full := name + " " + strings.Join(arg, " ")
		executedCommands = append(executedCommands, full)
		return exec.CommandContext(ctx, "echo", "ok")
	}

	fw := &nftablesFirewall{
		cfg:     cfg,
		execCmd: mockExec,
	}

	err := fw.Apply(context.Background())
	require.NoError(t, err)
	assert.Len(t, executedCommands, 1)
	assert.Equal(t, "nft -f -", executedCommands[0])

	err = fw.Clear(context.Background())
	require.NoError(t, err)
	assert.Len(t, executedCommands, 2)
	assert.Equal(t, "nft delete table inet vpnbuilder", executedCommands[1])
}

func TestIptablesFirewall_ApplyAndClear_Mock(t *testing.T) {
	cfg := FirewallConfig{
		Interface:         "wg0",
		SubnetV4:          "10.8.0.0/24",
		OutboundInterface: "eth0",
	}

	executedCommands := make([]string, 0)
	mockExec := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		full := name + " " + strings.Join(arg, " ")
		executedCommands = append(executedCommands, full)
		return exec.CommandContext(ctx, "echo", "ok")
	}

	fw := &iptablesFirewall{
		cfg:     cfg,
		execCmd: mockExec,
	}

	err := fw.Apply(context.Background())
	require.NoError(t, err)
	// 4 Clear commands + 4 Apply commands = 8
	assert.Len(t, executedCommands, 8)

	err = fw.Clear(context.Background())
	require.NoError(t, err)
	// 8 + 4 Clear commands = 12
	assert.Len(t, executedCommands, 12)
}
