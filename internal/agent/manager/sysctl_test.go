package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSysctlApplier_Apply(t *testing.T) {
	tempDir := t.TempDir()

	// Pre-create simulated /proc/sys hierarchy
	keyPath := filepath.Join(tempDir, "net", "ipv4")
	err := os.MkdirAll(keyPath, 0o755)
	require.NoError(t, err)

	applier := &linuxSysctlApplier{basePath: tempDir}

	settings := []SysctlSetting{
		{Key: "net.ipv4.ip_forward", Value: "1"},
		{Key: "non.existent.path", Value: "test"}, // should not fail, only log warning
	}

	err = applier.Apply(context.Background(), settings)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(keyPath, "ip_forward"))
	require.NoError(t, err)
	assert.Equal(t, "1\n", string(content))
}
