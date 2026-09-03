package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_Healthz(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{HTTPAddr: ":0"},
	}
	router := NewRouter(cfg, Handlers{}, nil, nil, nil, nil)
	server := NewServer(nil, cfg, router)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, "ok", body["status"])

	assert.NoError(t, server.Stop(context.Background()))
}

func TestServer_Readyz(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{HTTPAddr: ":0"},
	}
	router := NewRouter(cfg, Handlers{}, nil, nil, nil, nil)
	server := NewServer(nil, cfg, router)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body HealthResponse
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, "ok", body.Status)
	assert.Equal(t, "ok", body.Postgres)
	assert.Equal(t, "ok", body.Redis)
	require.NotNil(t, server)
}

func TestServer_Readyz_Degraded(t *testing.T) {
	ctx := context.Background()
	// Create closed pool to trigger ping failure
	pool, err := pgxpool.New(ctx, "postgres://invalid:pass@127.0.0.1:54399/none?connect_timeout=1")
	require.NoError(t, err)
	pool.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:54399",
	})
	_ = rdb.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{HTTPAddr: ":0"},
	}
	router := NewRouter(cfg, Handlers{}, nil, nil, pool, rdb)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var body HealthResponse
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, "degraded", body.Status)
}

func TestServer_StartAndStop(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{HTTPAddr: "127.0.0.1:0"},
	}
	router := NewRouter(cfg, Handlers{}, nil, nil, nil, nil)
	server := NewServer(nil, cfg, router)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = server.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	err := server.Stop(shutdownCtx)
	assert.NoError(t, err)
}
