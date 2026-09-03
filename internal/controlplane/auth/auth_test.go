package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWTManager_AccessAndRefreshTokens(t *testing.T) {
	secret := "test-secret-key-32-bytes-long-now!"
	accessTTL := 15 * time.Minute
	refreshTTL := 24 * time.Hour
	manager := NewJWTManager(secret, accessTTL, refreshTTL)

	adminID := uuid.New()
	email := "admin@example.com"
	role := "superadmin"

	accessToken, err := manager.GenerateAccessToken(adminID, email, role)
	require.NoError(t, err)
	require.NotEmpty(t, accessToken)

	claims, err := manager.ValidateToken(accessToken)
	require.NoError(t, err)
	assert.Equal(t, adminID, claims.AdminID)
	assert.Equal(t, email, claims.Email)
	assert.Equal(t, role, claims.Role)

	refreshToken, err := manager.GenerateRefreshToken(adminID)
	require.NoError(t, err)
	require.NotEmpty(t, refreshToken)
}

func TestJWTManager_ExpiredToken(t *testing.T) {
	secret := "test-secret-key-32-bytes-long-now!"
	manager := NewJWTManager(secret, -1*time.Minute, -1*time.Minute)

	token, err := manager.GenerateAccessToken(uuid.New(), "admin@example.com", "admin")
	require.NoError(t, err)

	_, err = manager.ValidateToken(token)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestJWTManager_InvalidToken(t *testing.T) {
	secret := "test-secret-key-32-bytes-long-now!"
	manager := NewJWTManager(secret, 15*time.Minute, 24*time.Hour)

	_, err := manager.ValidateToken("invalid.token.structure")
	assert.ErrorIs(t, err, ErrInvalidToken)

	otherManager := NewJWTManager("different-secret-key-32-bytes-long!", 15*time.Minute, 24*time.Hour)
	token, err := otherManager.GenerateAccessToken(uuid.New(), "admin@example.com", "admin")
	require.NoError(t, err)

	_, err = manager.ValidateToken(token)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestPasswordManager_HashAndVerify(t *testing.T) {
	pm := NewPasswordManager(10)

	password := "super-secure-password-123"
	hash, err := pm.Hash(password)
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	err = pm.Verify(password, hash)
	assert.NoError(t, err)

	err = pm.Verify("wrong-password", hash)
	assert.Error(t, err)
}

func TestPasswordManager_MinCostHandling(t *testing.T) {
	pm := NewPasswordManager(2)
	assert.GreaterOrEqual(t, pm.cost, 4)
}

type mockAPIKeyReader struct {
	key store.ApiKey
	err error
}

func (m *mockAPIKeyReader) GetAPIKeyByPrefix(ctx context.Context, prefix string) (store.ApiKey, error) {
	return m.key, m.err
}

func TestAPIKeyManager_GenerateKey(t *testing.T) {
	manager := NewAPIKeyManager(nil)

	rawKey, keyHash, err := manager.GenerateKey()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(rawKey, "vpn_"))
	assert.Len(t, keyHash, 64)
}

func TestAPIKeyManager_ValidateKey(t *testing.T) {
	rawKey := "vpn_abcdef1234567890abcdef12"
	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	// 1. Success case
	reader := &mockAPIKeyReader{
		key: store.ApiKey{
			Prefix:  "vpn_abcd",
			KeyHash: keyHash,
			ExpiresAt: pgtype.Timestamptz{
				Time:  time.Now().Add(time.Hour),
				Valid: true,
			},
		},
	}
	manager := NewAPIKeyManager(reader)
	validated, err := manager.ValidateKey(context.Background(), rawKey)
	require.NoError(t, err)
	assert.Equal(t, "vpn_abcd", validated.Prefix)

	// 2. Invalid prefix
	_, err = manager.ValidateKey(context.Background(), "invalid_prefix")
	assert.ErrorIs(t, err, ErrInvalidAPIKey)

	// 3. Database not found error
	errReader := &mockAPIKeyReader{err: errors.New("not found")}
	errManager := NewAPIKeyManager(errReader)
	_, err = errManager.ValidateKey(context.Background(), rawKey)
	assert.ErrorIs(t, err, ErrInvalidAPIKey)

	// 4. Hash mismatch
	mismatchReader := &mockAPIKeyReader{
		key: store.ApiKey{
			Prefix:  "vpn_abcd",
			KeyHash: "wrong_hash",
		},
	}
	mismatchManager := NewAPIKeyManager(mismatchReader)
	_, err = mismatchManager.ValidateKey(context.Background(), rawKey)
	assert.ErrorIs(t, err, ErrInvalidAPIKey)

	// 5. Expired key
	expiredReader := &mockAPIKeyReader{
		key: store.ApiKey{
			Prefix:  "vpn_abcd",
			KeyHash: keyHash,
			ExpiresAt: pgtype.Timestamptz{
				Time:  time.Now().Add(-1 * time.Hour),
				Valid: true,
			},
		},
	}
	expiredManager := NewAPIKeyManager(expiredReader)
	_, err = expiredManager.ValidateKey(context.Background(), rawKey)
	assert.ErrorIs(t, err, ErrInvalidAPIKey)
}

func TestJWTManager_Revocation(t *testing.T) {
	bl := NewMemoryBlacklist()
	manager := NewJWTManager("test-secret-key-32-bytes-long-now!", 15*time.Minute, 24*time.Hour).WithBlacklist(bl)

	token, err := manager.GenerateAccessToken(uuid.New(), "admin@example.com", "admin")
	require.NoError(t, err)

	claims, err := manager.ValidateToken(token)
	require.NoError(t, err)
	assert.NotEmpty(t, claims.ID)

	// Revoke the token using its JTI
	err = bl.Revoke(context.Background(), claims.ID, time.Hour)
	require.NoError(t, err)

	_, err = manager.ValidateToken(token)
	assert.ErrorIs(t, err, ErrTokenRevoked)
}

func TestJWTManager_TokenTypeEnforcement(t *testing.T) {
	manager := NewJWTManager("test-secret-key-32-bytes-long-now!", 15*time.Minute, 24*time.Hour)

	accessToken, err := manager.GenerateAccessToken(uuid.New(), "admin@example.com", "admin")
	require.NoError(t, err)

	refreshToken, err := manager.GenerateRefreshToken(uuid.New())
	require.NoError(t, err)

	// Access token validation
	claims, err := manager.ValidateAccessToken(accessToken)
	require.NoError(t, err)
	assert.Equal(t, "access", claims.TokenType)

	// Refresh token cannot be validated as access token
	_, err = manager.ValidateAccessToken(refreshToken)
	assert.ErrorIs(t, err, ErrInvalidToken)

	// Refresh token validation
	rClaims, err := manager.ValidateRefreshToken(refreshToken)
	require.NoError(t, err)
	assert.Equal(t, "refresh", rClaims.TokenType)

	// Access token cannot be validated as refresh token
	_, err = manager.ValidateRefreshToken(accessToken)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestAPIKeyManager_ShortKeySafety(t *testing.T) {
	manager := NewAPIKeyManager(nil)
	ctx := context.Background()

	shortKeys := []string{"", "vpn", "vpn_", "vpn_1", "vpn_123"}
	for _, k := range shortKeys {
		_, err := manager.ValidateKey(ctx, k)
		assert.ErrorIs(t, err, ErrInvalidAPIKey, "Key %q should be invalid without panicking", k)
	}
}
