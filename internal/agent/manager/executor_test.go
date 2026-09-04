package manager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

func TestCommandExecutor_Execute(t *testing.T) {
	ctx := context.Background()

	t.Run("nil command", func(t *testing.T) {
		exec := NewCommandExecutor(nil, nil)
		res := exec.Execute(ctx, nil)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.Equal(t, int32(1), res.ExitCode)
		assert.Equal(t, "unknown", res.CommandId)
	})

	t.Run("ping command", func(t *testing.T) {
		exec := NewCommandExecutor(nil, nil)
		res := exec.Execute(ctx, &agentv1.Command{
			CommandId: "cmd-1",
			Type:      "ping",
		})
		require.NotNil(t, res)
		assert.True(t, res.Success)
		assert.Equal(t, int32(0), res.ExitCode)
		assert.Equal(t, "cmd-1", res.CommandId)
		assert.True(t, strings.HasPrefix(res.Output, "pong: agent uptime"))
	})

	t.Run("reload_config success", func(t *testing.T) {
		called := false
		resync := func(ctx context.Context) error {
			called = true
			return nil
		}

		exec := NewCommandExecutor(nil, resync)
		res := exec.Execute(ctx, &agentv1.Command{
			CommandId: "cmd-2",
			Type:      "reload_config",
		})
		require.NotNil(t, res)
		assert.True(t, res.Success)
		assert.Equal(t, int32(0), res.ExitCode)
		assert.True(t, called)
		assert.Contains(t, res.Output, "reloaded successfully")
	})

	t.Run("reload_config failure", func(t *testing.T) {
		resync := func(ctx context.Context) error {
			return errors.New("simulated error")
		}

		exec := NewCommandExecutor(nil, resync)
		res := exec.Execute(ctx, &agentv1.Command{
			CommandId: "cmd-3",
			Type:      "reload_config",
		})
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.Equal(t, int32(1), res.ExitCode)
		assert.Contains(t, res.Output, "config reload failed: simulated error")
	})

	t.Run("restart_protocol", func(t *testing.T) {
		exec := NewCommandExecutor(nil, nil)
		res := exec.Execute(ctx, &agentv1.Command{
			CommandId: "cmd-4",
			Type:      "restart_protocol",
		})
		require.NotNil(t, res)
		assert.True(t, res.Success)
		assert.Equal(t, int32(0), res.ExitCode)
		assert.Contains(t, res.Output, "cycled successfully")
	})

	t.Run("rotate_certs", func(t *testing.T) {
		exec := NewCommandExecutor(nil, nil)
		res := exec.Execute(ctx, &agentv1.Command{
			CommandId: "cmd-5",
			Type:      "rotate_certs",
		})
		require.NotNil(t, res)
		assert.True(t, res.Success)
		assert.Equal(t, int32(0), res.ExitCode)
		assert.Contains(t, res.Output, "reload scheduled")
	})

	t.Run("unsupported command", func(t *testing.T) {
		exec := NewCommandExecutor(nil, nil)
		res := exec.Execute(ctx, &agentv1.Command{
			CommandId: "cmd-6",
			Type:      "reboot_system",
		})
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.Equal(t, int32(127), res.ExitCode)
		assert.Contains(t, res.Output, "unsupported command type: reboot_system")
	})
}
