package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
)

func TestGRPCServer_StartAndStop(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCAddr: "127.0.0.1:0",
		},
	}

	sessionMgr := NewSessionManager()
	server := NewServer(cfg, nil)
	require.NotNil(t, server)
	assert.NotNil(t, server.GRPCServer())
	assert.Equal(t, 0, sessionMgr.Count())

	ctx, cancel := context.WithCancel(context.Background())
	serverErrCh := make(chan error, 1)

	go func() {
		serverErrCh <- server.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// Graceful stop
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()

	err := server.Stop(stopCtx)
	assert.NoError(t, err)

	select {
	case err := <-serverErrCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("server failed to stop within timeout")
	}
}
