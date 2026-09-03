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
}

// RedisBlacklist persists revoked token IDs in Redis with automatic TTL expiration.
type RedisBlacklist struct {
	client *redis.Client
	prefix string
}

func NewRedisBlacklist(client *redis.Client) *RedisBlacklist {
	return &RedisBlacklist{
		client: client,
		prefix: "jwt:revoked:",
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

// MemoryBlacklist provides an in-memory thread-safe blacklist for testing or standalone modes.
type MemoryBlacklist struct {
	mu      sync.RWMutex
	revoked map[string]time.Time
}

func NewMemoryBlacklist() *MemoryBlacklist {
	return &MemoryBlacklist{
		revoked: make(map[string]time.Time),
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
