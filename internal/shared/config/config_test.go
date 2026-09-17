package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, ":8110", cfg.Server.HTTPAddr)
	assert.Equal(t, ":9090", cfg.Server.GRPCAddr)
	assert.Equal(t, 25, cfg.Database.MaxOpenConns)
	assert.Equal(t, 5, cfg.Database.MaxIdleConns)
	assert.Equal(t, 5*time.Minute, cfg.Database.ConnMaxLifetime)
	assert.Equal(t, 1*time.Minute, cfg.Database.ConnMaxIdleTime)
	assert.Equal(t, "localhost:6379", cfg.Redis.Addr)
	assert.Equal(t, 15*time.Minute, cfg.Auth.JWTAccessTTL)
	assert.Equal(t, 168*time.Hour, cfg.Auth.JWTRefreshTTL)
	assert.Equal(t, 12, cfg.Auth.BcryptCost)
	assert.Equal(t, "wg", cfg.Adapter.WireGuard.InterfacePrefix)
	assert.Equal(t, "10.8.0.0/16", cfg.Adapter.WireGuard.SubnetV4)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)
}

func TestLoadAgentDefaults(t *testing.T) {
	cfg, err := LoadAgent("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.NotEmpty(t, cfg.Agent.NodeName)
	assert.Equal(t, "wg", cfg.Agent.WireGuard.InterfacePrefix)
	assert.Equal(t, "info", cfg.Log.Level)
}

func TestLoadWithConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configContent := `
server:
  http_addr: ":8888"
  grpc_addr: ":9999"
database:
  max_open_conns: 50
  dsn: "postgres://custom:pass@db:5432/test?sslmode=disable"
auth:
  bcrypt_cost: 14
log:
  level: "debug"
`
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, ":8888", cfg.Server.HTTPAddr)
	assert.Equal(t, ":9999", cfg.Server.GRPCAddr)
	assert.Equal(t, 50, cfg.Database.MaxOpenConns)
	assert.Equal(t, "postgres://custom:pass@db:5432/test?sslmode=disable", cfg.Database.DSN)
	assert.Equal(t, 14, cfg.Auth.BcryptCost)
	assert.Equal(t, "debug", cfg.Log.Level)
}

func TestLoadEnvironmentOverrides(t *testing.T) {
	t.Setenv("VPNBUILDER_SERVER_HTTP_ADDR", ":7070")
	t.Setenv("VPNBUILDER_DATABASE_MAX_OPEN_CONNS", "40")
	t.Setenv("VPNBUILDER_LOG_LEVEL", "warn")
	t.Setenv("VPNBUILDER_AUTH_JWT_SECRET", "test-secret-at-least-32-bytes-long!!")
	t.Setenv("VPNBUILDER_ENV", "prod")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, ":7070", cfg.Server.HTTPAddr)
	assert.Equal(t, 40, cfg.Database.MaxOpenConns)
	assert.Equal(t, "warn", cfg.Log.Level)
	assert.Equal(t, "test-secret-at-least-32-bytes-long!!", cfg.Auth.JWTSecret)
	assert.Equal(t, "prod", cfg.Env)
}

func TestLoadAgentEnvironmentOverrides(t *testing.T) {
	t.Setenv("VPNBUILDER_AGENT_NODE_NAME", "test-node-01")
	t.Setenv("VPNBUILDER_AGENT_CONTROL_PLANE", "127.0.0.1:9090")
	t.Setenv("VPNBUILDER_LOG_LEVEL", "error")

	cfg, err := LoadAgent("")
	require.NoError(t, err)

	assert.Equal(t, "test-node-01", cfg.Agent.NodeName)
	assert.Equal(t, "127.0.0.1:9090", cfg.Agent.ControlPlane)
	assert.Equal(t, "error", cfg.Log.Level)
}
