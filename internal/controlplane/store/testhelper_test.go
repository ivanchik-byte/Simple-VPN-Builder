package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

var (
	testPool *pgxpool.Pool
	initOnce sync.Once
	initErr  error
)

func setupTestDB(t *testing.T) (*store.Repositories, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	initOnce.Do(func() {
		pgContainer, err := tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("vpnbuilder_test"),
			tcpostgres.WithUsername("testuser"),
			tcpostgres.WithPassword("testpass"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(60*time.Second),
			),
		)
		if err != nil {
			initErr = fmt.Errorf("start postgres container: %w", err)
			return
		}

		connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			initErr = fmt.Errorf("get connection string: %w", err)
			return
		}

		pool, err := pgxpool.New(ctx, connStr)
		if err != nil {
			initErr = fmt.Errorf("connect to test db: %w", err)
			return
		}

		if err := store.RunMigrations(pool); err != nil {
			initErr = fmt.Errorf("run migrations: %w", err)
			return
		}

		testPool = pool
	})

	require.NoError(t, initErr)
	require.NotNil(t, testPool)

	// Clean database tables between tests to guarantee isolation
	cleanTables(t, testPool)

	return store.NewRepositories(testPool), testPool
}

func cleanTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	queries := []string{
		"SET audit.allow_maintenance = 'on'",
		"TRUNCATE TABLE traffic_stats, credentials, users, nodes, plans, audit_logs, api_keys, admins, webhooks CASCADE",
		"SET audit.allow_maintenance = 'off'",
	}
	for _, q := range queries {
		_, err := pool.Exec(ctx, q)
		require.NoError(t, err)
	}
}
