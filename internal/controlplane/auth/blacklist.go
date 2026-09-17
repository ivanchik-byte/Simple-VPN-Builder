package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// TokenBlacklist manages revoked JWT token identifiers.
type TokenBlacklist interface {
	Revoke(ctx context.Context, jti string, ttl time.Duration) error
	IsRevoked(ctx context.Context, jti string) (bool, error)
	// RevokeAdmin invalidates every token issued for adminID at or before now
	// (password rotation / compromise response). Note: JWT IssuedAt carries
	// 1s resolution, so tokens minted in the same second as the cut-off are
	// conservatively treated as revoked; fresh logins a second later pass.
	RevokeAdmin(ctx context.Context, adminID string, ttl time.Duration) error
	// AdminRevokedAt returns the cut-off instant before which admin tokens die.
	AdminRevokedAt(ctx context.Context, adminID string) (time.Time, bool, error)
}

// RedisBlacklist persists revoked token IDs in Redis with automatic TTL expiration.
type RedisBlacklist struct {
	client      *redis.Client
	prefix      string
	adminPrefix string
}

func NewRedisBlacklist(client *redis.Client) *RedisBlacklist {
	return &RedisBlacklist{
		client:      client,
		prefix:      "jwt:revoked:",
		adminPrefix: "jwt:revoked-admin:",
	}
}

func (b *RedisBlacklist) Revoke(ctx context.Context, jti string, ttl time.Duration) error {
	if jti == "" {
		return nil
	}
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	key := b.prefix + jti
	if err := b.client.Set(ctx, key, "1", ttl).Err(); err != nil {
		return fmt.Errorf("blacklist token in redis: %w", err)
	}
	return nil
}

func (b *RedisBlacklist) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}
	key := b.prefix + jti
	val, err := b.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("check token blacklist in redis: %w", err)
	}
	return val > 0, nil
}

// RevokeAdmin records a cut-off timestamp; tokens issued at or before it are dead.
func (b *RedisBlacklist) RevokeAdmin(ctx context.Context, adminID string, ttl time.Duration) error {
	if adminID == "" {
		return nil
	}
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour
	}
	key := b.adminPrefix + adminID
	if err := b.client.Set(ctx, key, time.Now().UnixNano(), ttl).Err(); err != nil {
		return fmt.Errorf("blacklist admin tokens in redis: %w", err)
	}
	return nil
}

// AdminRevokedAt returns the cut-off instant for admin-wide revocation.
func (b *RedisBlacklist) AdminRevokedAt(ctx context.Context, adminID string) (time.Time, bool, error) {
	if adminID == "" {
		return time.Time{}, false, nil
	}
	val, err := b.client.Get(ctx, b.adminPrefix+adminID).Result()
	if err != nil {
		if err == redis.Nil {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("check admin blacklist in redis: %w", err)
	}
	var nanos int64
	if _, err := fmt.Sscanf(val, "%d", &nanos); err != nil || nanos <= 0 {
		return time.Time{}, false, nil
	}
	return time.Unix(0, nanos), true, nil
}

// MemoryBlacklist provides an in-memory thread-safe blacklist for testing or standalone modes.
type MemoryBlacklist struct {
	mu           sync.RWMutex
	revoked      map[string]time.Time
	adminRevoked map[string]time.Time
}

func NewMemoryBlacklist() *MemoryBlacklist {
	return &MemoryBlacklist{
		revoked:      make(map[string]time.Time),
		adminRevoked: make(map[string]time.Time),
	}
}

func (b *MemoryBlacklist) Revoke(_ context.Context, jti string, ttl time.Duration) error {
	if jti == "" {
		return nil
	}
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.revoked[jti] = time.Now().Add(ttl)
	return nil
}

func (b *MemoryBlacklist) IsRevoked(_ context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	expiry, exists := b.revoked[jti]
	if !exists {
		return false, nil
	}
	if time.Now().After(expiry) {
		return false, nil
	}
	return true, nil
}

// RevokeAdmin records a cut-off timestamp; tokens issued at or before it are dead.
func (b *MemoryBlacklist) RevokeAdmin(_ context.Context, adminID string, ttl time.Duration) error {
	if adminID == "" {
		return nil
	}
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.adminRevoked[adminID] = time.Now()
	return nil
}

// AdminRevokedAt returns the cut-off instant for admin-wide revocation.
func (b *MemoryBlacklist) AdminRevokedAt(_ context.Context, adminID string) (time.Time, bool, error) {
	if adminID == "" {
		return time.Time{}, false, nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	cutoff, exists := b.adminRevoked[adminID]
	if !exists {
		return time.Time{}, false, nil
	}
	return cutoff, true, nil
}
