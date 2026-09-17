package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryBlacklist_RevokeAndCheck(t *testing.T) {
	bl := NewMemoryBlacklist()
	ctx := context.Background()

	revoked, err := bl.IsRevoked(ctx, "token-1")
	require.NoError(t, err)
	assert.False(t, revoked)

	err = bl.Revoke(ctx, "token-1", 1*time.Hour)
	require.NoError(t, err)

	revoked, err = bl.IsRevoked(ctx, "token-1")
	require.NoError(t, err)
	assert.True(t, revoked)

	// Test expired token
	err = bl.Revoke(ctx, "token-expired", -1*time.Second)
	require.NoError(t, err)

	revoked, err = bl.IsRevoked(ctx, "token-expired")
	require.NoError(t, err)
	assert.False(t, revoked)

	// Empty JTI checks
	assert.NoError(t, bl.Revoke(ctx, "", time.Hour))
	revoked, err = bl.IsRevoked(ctx, "")
	assert.NoError(t, err)
	assert.False(t, revoked)
}

func TestRedisBlacklist_EmptyAndErrors(t *testing.T) {
	bl := NewRedisBlacklist(nil)
	ctx := context.Background()

	assert.NoError(t, bl.Revoke(ctx, "", 0))
	revoked, err := bl.IsRevoked(ctx, "")
	assert.NoError(t, err)
	assert.False(t, revoked)
}

func TestMemoryBlacklist_RevokeAdmin(t *testing.T) {
	bl := NewMemoryBlacklist()
	ctx := context.Background()
	manager := NewJWTManager("test-secret-key-32-bytes-long-now!", 15*time.Minute, 7*24*time.Hour)
	manager.WithBlacklist(bl)

	adminID := uuid.New()
	oldAccess, err := manager.GenerateAccessToken(adminID, "a@vpn.test", "admin")
	require.NoError(t, err)
	oldRefresh, err := manager.GenerateRefreshToken(adminID)
	require.NoError(t, err)

	_, err = manager.ValidateAccessToken(oldAccess)
	require.NoError(t, err)

	require.NoError(t, bl.RevokeAdmin(ctx, adminID.String(), 7*24*time.Hour))

	_, err = manager.ValidateAccessToken(oldAccess)
	assert.ErrorIs(t, err, ErrTokenRevoked)
	_, err = manager.ValidateRefreshToken(oldRefresh)
	assert.ErrorIs(t, err, ErrTokenRevoked)

	// Tokens minted after rotation stay valid (IssuedAt has 1s resolution,
	// so cross a second boundary first).
	time.Sleep(1100 * time.Millisecond)
	newAccess, err := manager.GenerateAccessToken(adminID, "a@vpn.test", "admin")
	require.NoError(t, err)
	_, err = manager.ValidateAccessToken(newAccess)
	assert.NoError(t, err)
}
