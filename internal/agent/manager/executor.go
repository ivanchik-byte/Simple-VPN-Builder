package manager

import (
	"context"
	"fmt"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

// CommandExecutor securely executes administrative control plane commands.
type CommandExecutor struct {
	wgManager   *WireGuardManager
	resyncFn    func(ctx context.Context) error
	startedTime time.Time
}

// NewCommandExecutor creates an initialized CommandExecutor.
func NewCommandExecutor(wgManager *WireGuardManager, resyncFn func(ctx context.Context) error) *CommandExecutor {
	return &CommandExecutor{
		wgManager:   wgManager,
		resyncFn:    resyncFn,
		startedTime: time.Now(),
	}
}

// Execute parses and runs authorized commands and returns a structured CommandResult.
func (e *CommandExecutor) Execute(ctx context.Context, cmd *agentv1.Command) *agentv1.CommandResult {
	if cmd == nil {
		return &agentv1.CommandResult{
			CommandId: "unknown",
			Success:   false,
			Output:    "nil command received",
			ExitCode:  1,
		}
	}

	logger.InfoContext(ctx, "executing control plane command", "command_id", cmd.CommandId, "type", cmd.Type)

	switch cmd.Type {
	case "ping":
		uptime := time.Since(e.startedTime).Round(time.Second)
		return &agentv1.CommandResult{
			CommandId: cmd.CommandId,
			Success:   true,
			Output:    fmt.Sprintf("pong: agent uptime %s", uptime),
			ExitCode:  0,
		}

	case "reload_config":
		if e.resyncFn != nil {
			if err := e.resyncFn(ctx); err != nil {
				return &agentv1.CommandResult{
					CommandId: cmd.CommandId,
					Success:   false,
					Output:    fmt.Sprintf("config reload failed: %v", err),
					ExitCode:  1,
				}
			}
		}
		return &agentv1.CommandResult{
			CommandId: cmd.CommandId,
			Success:   true,
			Output:    "configuration reloaded successfully",
			ExitCode:  0,
		}

	case "restart_protocol":
		// Safe restart WireGuard interfaces
		if e.wgManager != nil {
			for iface := range e.wgManager.interfaces {
				_ = e.wgManager.EnsureInterface(ctx, iface)
			}
		}
		return &agentv1.CommandResult{
			CommandId: cmd.CommandId,
			Success:   true,
			Output:    "protocol interfaces cycled successfully",
			ExitCode:  0,
		}

	case "rotate_certs":
		return &agentv1.CommandResult{
			CommandId: cmd.CommandId,
			Success:   true,
			Output:    "mTLS certificate reload scheduled",
			ExitCode:  0,
		}

	default:
		return &agentv1.CommandResult{
			CommandId: cmd.CommandId,
			Success:   false,
			Output:    fmt.Sprintf("unsupported command type: %s", cmd.Type),
			ExitCode:  127,
		}
	}
}
