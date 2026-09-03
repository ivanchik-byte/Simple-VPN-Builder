package auth

import (
	"context"
	"testing"
	"time"

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
