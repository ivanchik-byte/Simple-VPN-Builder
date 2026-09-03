package store_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

func TestNodeRepository_CRUD(t *testing.T) {
	repos, _ := setupTestDB(t)
	ctx := context.Background()

	node, err := repos.Nodes.Create(ctx, store.CreateNodeParams{
		Name:            "frankfurt-01",
		Endpoint:        "198.51.100.1:51820",
		GrpcEndpoint:    "198.51.100.1:9090",
		Region:          pgtype.Text{String: "eu-central", Valid: true},
		CapacityGbps:    pgtype.Int4{Int32: 10, Valid: true},
		Status:          pgtype.Text{String: "online", Valid: true},
		PublicKey:       "wg-public-key-base64-1234567890=",
		CertFingerprint: "sha256-cert-fingerprint-hex-0011223344",
	})
	require.NoError(t, err)
	assert.Equal(t, "frankfurt-01", node.Name)
	assert.Equal(t, "online", node.Status.String)

	got, err := repos.Nodes.GetByID(ctx, node.ID)
	require.NoError(t, err)
	assert.Equal(t, node.ID, got.ID)

	byName, err := repos.Nodes.GetByName(ctx, "frankfurt-01")
	require.NoError(t, err)
	assert.Equal(t, node.ID, byName.ID)

	list, count, err := repos.Nodes.List(ctx, store.NodeFilter{
		Status: "online",
		Limit:  10,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.Len(t, list, 1)

	actives, err := repos.Nodes.ListActive(ctx)
	require.NoError(t, err)
	assert.Len(t, actives, 1)

	err = repos.Nodes.UpdateHeartbeat(ctx, node.ID, "online")
	require.NoError(t, err)

	updated, err := repos.Nodes.Update(ctx, store.UpdateNodeParams{
		ID:              node.ID,
		Name:            "frankfurt-01-renamed",
		Endpoint:        node.Endpoint,
		GrpcEndpoint:    node.GrpcEndpoint,
		Region:          node.Region,
		CapacityGbps:    pgtype.Int4{Int32: 20, Valid: true},
		Status:          node.Status,
		Tags:            node.Tags,
		PublicKey:       node.PublicKey,
		CertFingerprint: node.CertFingerprint,
	})
	require.NoError(t, err)
	assert.Equal(t, "frankfurt-01-renamed", updated.Name)
	assert.Equal(t, int32(20), updated.CapacityGbps.Int32)

	err = repos.Nodes.Delete(ctx, node.ID)
	require.NoError(t, err)

	_, err = repos.Nodes.GetByID(ctx, node.ID)
	assert.Error(t, err)
}

func TestPlanRepository_CRUD_And_Triggers(t *testing.T) {
	repos, _ := setupTestDB(t)
	ctx := context.Background()

	plan, err := repos.Plans.Create(ctx, store.CreatePlanParams{
		Name:         "pro-unlimited",
		MonthlyPrice: pgtype.Numeric{Valid: false},
		TrafficLimit: pgtype.Int8{Int64: 500 * 1024 * 1024 * 1024, Valid: true},
		DeviceLimit:  pgtype.Int4{Int32: 5, Valid: true},
		Protocols:    []string{"wireguard", "vless"},
		IsActive:     pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, "pro-unlimited", plan.Name)
	assert.False(t, plan.CreatedAt.Time.IsZero())
	assert.False(t, plan.UpdatedAt.Time.IsZero())

	got, err := repos.Plans.GetByID(ctx, plan.ID)
	require.NoError(t, err)
	assert.Equal(t, plan.ID, got.ID)

	byName, err := repos.Plans.GetByName(ctx, "pro-unlimited")
	require.NoError(t, err)
	assert.Equal(t, plan.ID, byName.ID)

	plans, err := repos.Plans.List(ctx)
	require.NoError(t, err)
	assert.Len(t, plans, 1)

	// Test update trigger on plans table
	time.Sleep(10 * time.Millisecond)
	updated, err := repos.Plans.Update(ctx, store.UpdatePlanParams{
		ID:           plan.ID,
		Name:         "pro-ultra",
		MonthlyPrice: plan.MonthlyPrice,
		TrafficLimit: plan.TrafficLimit,
		DeviceLimit:  pgtype.Int4{Int32: 10, Valid: true},
		Protocols:    plan.Protocols,
		Features:     plan.Features,
		IsActive:     plan.IsActive,
	})
	require.NoError(t, err)
	assert.Equal(t, "pro-ultra", updated.Name)
	assert.Equal(t, int32(10), updated.DeviceLimit.Int32)

	err = repos.Plans.Delete(ctx, plan.ID)
	require.NoError(t, err)
}

func TestUserRepository_CRUD_And_Tokens(t *testing.T) {
	repos, _ := setupTestDB(t)
	ctx := context.Background()

	plan, err := repos.Plans.Create(ctx, store.CreatePlanParams{
		Name: "standard",
	})
	require.NoError(t, err)

	user, err := repos.Users.Create(ctx, store.CreateUserParams{
		Email:        pgtype.Text{String: "user@example.com", Valid: true},
		Username:     "testuser",
		PasswordHash: pgtype.Text{String: "hash123", Valid: true},
		Status:       pgtype.Text{String: "active", Valid: true},
		PlanID:       pgtype.UUID{Bytes: plan.ID, Valid: true},
		TrafficLimit: pgtype.Int8{Int64: 100 * 1024 * 1024 * 1024, Valid: true},
		TrafficUsed:  pgtype.Int8{Int64: 0, Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, "testuser", user.Username)
	assert.NotEqual(t, uuid.Nil, user.SubscriptionToken)

	byID, err := repos.Users.GetByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, byID.ID)

	byUsername, err := repos.Users.GetByUsername(ctx, "testuser")
	require.NoError(t, err)
	assert.Equal(t, user.ID, byUsername.ID)

	byEmail, err := repos.Users.GetByEmail(ctx, "user@example.com")
	require.NoError(t, err)
	assert.Equal(t, user.ID, byEmail.ID)

	byToken, err := repos.Users.GetBySubscriptionToken(ctx, user.SubscriptionToken)
	require.NoError(t, err)
	assert.Equal(t, user.ID, byToken.ID)

	rotated, err := repos.Users.RotateSubscriptionToken(ctx, user.ID)
	require.NoError(t, err)
	assert.NotEqual(t, user.SubscriptionToken, rotated.SubscriptionToken)

	err = repos.Users.UpdateTraffic(ctx, user.ID, 1024*1024)
	require.NoError(t, err)

	refreshed, err := repos.Users.GetByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1024*1024), refreshed.TrafficUsed.Int64)

	err = repos.Users.ResetTraffic(ctx, user.ID)
	require.NoError(t, err)

	refreshed, err = repos.Users.GetByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), refreshed.TrafficUsed.Int64)

	users, count, err := repos.Users.List(ctx, store.UserFilter{
		Status: "active",
		PlanID: &plan.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.Len(t, users, 1)

	err = repos.Users.Delete(ctx, user.ID)
	require.NoError(t, err)
}

func TestCredentialRepository_AWG_And_NodePeers(t *testing.T) {
	repos, _ := setupTestDB(t)
	ctx := context.Background()

	node, err := repos.Nodes.Create(ctx, store.CreateNodeParams{
		Name:            "helsinki-01",
		Endpoint:        "198.51.100.2:51820",
		GrpcEndpoint:    "198.51.100.2:9090",
		PublicKey:       "server-pub-key-12345=",
		CertFingerprint: "sha256-fingerprint-12345",
	})
	require.NoError(t, err)

	user, err := repos.Users.Create(ctx, store.CreateUserParams{
		Username: "awg_user",
	})
	require.NoError(t, err)

	ip := netip.MustParseAddr("10.8.0.2")
	cred, err := repos.Credentials.Create(ctx, store.CreateCredentialParams{
		UserID:     user.ID,
		NodeID:     node.ID,
		Protocol:   "wireguard",
		PrivateKey: pgtype.Text{String: "priv1", Valid: true},
		PublicKey:  pgtype.Text{String: "pub1", Valid: true},
		Ipv4:       &ip,
		Status:     pgtype.Text{String: "active", Valid: true},
		AwgJc:      pgtype.Int4{Int32: 5, Valid: true},
		AwgJmin:    pgtype.Int4{Int32: 50, Valid: true},
		AwgJmax:    pgtype.Int4{Int32: 80, Valid: true},
		AwgS1:      pgtype.Int4{Int32: 64, Valid: true},
		AwgS2:      pgtype.Int4{Int32: 64, Valid: true},
		AwgH1:      pgtype.Int8{Int64: 12345678, Valid: true},
		AwgH2:      pgtype.Int8{Int64: 23456789, Valid: true},
		AwgH3:      pgtype.Int8{Int64: 34567890, Valid: true},
		AwgH4:      pgtype.Int8{Int64: 45678901, Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(5), cred.AwgJc.Int32)
	assert.Equal(t, int64(12345678), cred.AwgH1.Int64)

	byNodeProto, err := repos.Credentials.GetByUserNodeProtocol(ctx, user.ID, node.ID, "wireguard")
	require.NoError(t, err)
	assert.Equal(t, cred.ID, byNodeProto.ID)

	byUser, err := repos.Credentials.ListByUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, byUser, 1)

	byNode, err := repos.Credentials.ListByNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Len(t, byNode, 1)

	activeByNode, err := repos.Credentials.ListActiveByNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Len(t, activeByNode, 1)

	err = repos.Credentials.Delete(ctx, cred.ID)
	require.NoError(t, err)
}

func TestTrafficRepository_Upsert_And_Aggregates(t *testing.T) {
	repos, _ := setupTestDB(t)
	ctx := context.Background()

	node, err := repos.Nodes.Create(ctx, store.CreateNodeParams{
		Name:            "traffic-node",
		Endpoint:        "198.51.100.3:51820",
		GrpcEndpoint:    "198.51.100.3:9090",
		PublicKey:       "key-traffic",
		CertFingerprint: "fingerprint-traffic",
	})
	require.NoError(t, err)

	user, err := repos.Users.Create(ctx, store.CreateUserParams{
		Username: "traffic_user",
	})
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Hour)

	stat, err := repos.Traffic.Upsert(ctx, store.UpsertTrafficStatsParams{
		UserID:     user.ID,
		NodeID:     node.ID,
		Protocol:   "wireguard",
		HourBucket: now,
		RxBytes:    pgtype.Int8{Int64: 1000, Valid: true},
		TxBytes:    pgtype.Int8{Int64: 2000, Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1000), stat.RxBytes.Int64)

	// Second upsert increments the existing bucket
	stat2, err := repos.Traffic.Upsert(ctx, store.UpsertTrafficStatsParams{
		UserID:     user.ID,
		NodeID:     node.ID,
		Protocol:   "wireguard",
		HourBucket: now,
		RxBytes:    pgtype.Int8{Int64: 500, Valid: true},
		TxBytes:    pgtype.Int8{Int64: 500, Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1500), stat2.RxBytes.Int64)
	assert.Equal(t, int64(2500), stat2.TxBytes.Int64)

	aggUser, err := repos.Traffic.GetAggregateByUser(ctx, user.ID, now.Add(-time.Hour), now.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1500), aggUser.TotalRx)
	assert.Equal(t, int64(2500), aggUser.TotalTx)

	nodeAggs, err := repos.Traffic.GetAggregateByNode(ctx, now.Add(-time.Hour), now.Add(time.Hour))
	require.NoError(t, err)
	assert.Len(t, nodeAggs, 1)
	assert.Equal(t, int64(1500), nodeAggs[0].TotalRx)
}

func TestAdminRepository_CRUD(t *testing.T) {
	repos, _ := setupTestDB(t)
	ctx := context.Background()

	admin, err := repos.Admins.Create(ctx, store.CreateAdminParams{
		Email:        "secadmin@vpn.internal",
		PasswordHash: "bcrypt-hash-secadmin",
		Role:         pgtype.Text{String: "superadmin", Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, "secadmin@vpn.internal", admin.Email)

	byID, err := repos.Admins.GetByID(ctx, admin.ID)
	require.NoError(t, err)
	assert.Equal(t, admin.ID, byID.ID)

	byEmail, err := repos.Admins.GetByEmail(ctx, "secadmin@vpn.internal")
	require.NoError(t, err)
	assert.Equal(t, admin.ID, byEmail.ID)

	err = repos.Admins.UpdateLastLogin(ctx, admin.ID)
	require.NoError(t, err)

	admins, err := repos.Admins.List(ctx)
	require.NoError(t, err)
	assert.Len(t, admins, 1)

	err = repos.Admins.Delete(ctx, admin.ID)
	require.NoError(t, err)
}

func TestAPIKeyRepository_CRUD(t *testing.T) {
	repos, _ := setupTestDB(t)
	ctx := context.Background()

	key, err := repos.APIKeys.Create(ctx, store.CreateAPIKeyParams{
		Name:    "ci-terraform-key",
		KeyHash: "sha256-hashed-api-key-value-123456",
		Prefix:  "vpn_test",
		Scopes:  []string{"read", "write"},
	})
	require.NoError(t, err)
	assert.Equal(t, "vpn_test", key.Prefix)

	byPrefix, err := repos.APIKeys.GetByPrefix(ctx, "vpn_test")
	require.NoError(t, err)
	assert.Equal(t, key.ID, byPrefix.ID)

	keys, err := repos.APIKeys.List(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 1)

	err = repos.APIKeys.Delete(ctx, key.ID)
	require.NoError(t, err)
}

func TestWebhookRepository_CRUD(t *testing.T) {
	repos, _ := setupTestDB(t)
	ctx := context.Background()

	wh, err := repos.Webhooks.Create(ctx, store.CreateWebhookParams{
		Url:      "https://bot.example.com/webhook",
		Secret:   "whsec_supersecretkey123",
		Events:   []string{"user.expired", "user.traffic_warning"},
		IsActive: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, "https://bot.example.com/webhook", wh.Url)

	byID, err := repos.Webhooks.GetByID(ctx, wh.ID)
	require.NoError(t, err)
	assert.Equal(t, wh.ID, byID.ID)

	actives, err := repos.Webhooks.ListActive(ctx)
	require.NoError(t, err)
	assert.Len(t, actives, 1)

	err = repos.Webhooks.Delete(ctx, wh.ID)
	require.NoError(t, err)
}

func TestTransactor_CommitAndRollback(t *testing.T) {
	repos, _ := setupTestDB(t)
	ctx := context.Background()

	// 1. Test successful transaction commit
	err := repos.Tx.WithTx(ctx, func(q *store.Queries) error {
		_, createErr := q.CreatePlan(ctx, store.CreatePlanParams{
			Name: "plan-tx-commit",
		})
		return createErr
	})
	require.NoError(t, err)

	_, err = repos.Plans.GetByName(ctx, "plan-tx-commit")
	assert.NoError(t, err)

	// 2. Test rollback on error
	err = repos.Tx.WithTx(ctx, func(q *store.Queries) error {
		_, createErr := q.CreatePlan(ctx, store.CreatePlanParams{
			Name: "plan-tx-rollback",
		})
		require.NoError(t, createErr)

		// Deliberately trigger error to rollback
		return errors.New("simulated business logic failure")
	})
	assert.Error(t, err)

	// Verify plan was rolled back and does not exist in db
	_, err = repos.Plans.GetByName(ctx, "plan-tx-rollback")
	assert.Error(t, err)
}

func TestMigrator_RollbackAndReapply(t *testing.T) {
	_, pool := setupTestDB(t)

	// Test rolling back migration step
	err := store.RollbackMigration(pool)
	require.NoError(t, err)

	// Reapply migrations cleanly
	err = store.RunMigrations(pool)
	require.NoError(t, err)
}

