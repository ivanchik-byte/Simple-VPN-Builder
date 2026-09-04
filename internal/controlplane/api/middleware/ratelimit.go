package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/redis/go-redis/v9"
)

type RateLimiter struct {
	client    *redis.Client
	limit     int
	window    time.Duration
	memLimits map[string][]time.Time
	memMu     sync.Mutex
}

func NewRateLimiter(client *redis.Client, limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = 60
	}
	if window <= 0 {
		window = 1 * time.Minute
	}
	return &RateLimiter{
		client:    client,
		limit:     limit,
		window:    window,
		memLimits: make(map[string][]time.Time),
	}
}

// Allow checks if the given key is allowed under rate limits.
func (rl *RateLimiter) Allow(ctx context.Context, key string) (allowed bool, remaining int, retryAfterSec int, err error) {
	if rl.client != nil {
		return rl.allowRedis(ctx, key)
	}
	return rl.allowMemory(key)
}

func (rl *RateLimiter) allowRedis(ctx context.Context, key string) (bool, int, int, error) {
	redisKey := fmt.Sprintf("ratelimit:%s", key)
	now := time.Now().UnixNano()
	windowStart := now - rl.window.Nanoseconds()

	pipe := rl.client.Pipeline()
	// Remove old timestamps outside current window
	pipe.ZRemRangeByScore(ctx, redisKey, "0", fmt.Sprintf("%d", windowStart))
	// Add current timestamp
	pipe.ZAdd(ctx, redisKey, redis.Z{Score: float64(now), Member: fmt.Sprintf("%d", now)})
	// Count elements in window
	countCmd := pipe.ZCard(ctx, redisKey)
	// Set TTL on key
	pipe.Expire(ctx, redisKey, rl.window+time.Second)

	_, err := pipe.Exec(ctx)
	if err != nil {
		// Fallback to in-memory on Redis error to maintain service availability
		return rl.allowMemory(key)
	}

	count := int(countCmd.Val())
	if count > rl.limit {
		return false, 0, int(rl.window.Seconds()), nil
	}

	remaining := rl.limit - count
	return true, remaining, 0, nil
}

func (rl *RateLimiter) allowMemory(key string) (bool, int, int, error) {
	rl.memMu.Lock()
	defer rl.memMu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	timestamps := rl.memLimits[key]
	validTimestamps := make([]time.Time, 0, len(timestamps))
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	if len(validTimestamps) == 0 {
		delete(rl.memLimits, key)
	}

	if len(validTimestamps) >= rl.limit {
		rl.memLimits[key] = validTimestamps
		return false, 0, int(rl.window.Seconds()), nil
	}

	validTimestamps = append(validTimestamps, now)
	rl.memLimits[key] = validTimestamps
	remaining := rl.limit - len(validTimestamps)
	return true, remaining, 0, nil
}

// StartJanitor periodically prunes stale IP entries from memory to prevent leaks (MAJ-05).
func (rl *RateLimiter) StartJanitor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rl.memMu.Lock()
				now := time.Now()
				cutoff := now.Add(-rl.window)
				for k, timestamps := range rl.memLimits {
					if len(timestamps) == 0 || timestamps[len(timestamps)-1].Before(cutoff) {
						delete(rl.memLimits, k)
					}
				}
				rl.memMu.Unlock()
			}
		}
	}()
}

// Middleware creates an HTTP middleware limiting requests by client IP.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if ip == "" {
			ip = "unknown"
		}

		allowed, remaining, retryAfter, _ := rl.Allow(r.Context(), ip)

		w.Header().Set("RateLimit-Limit", fmt.Sprintf("%d", rl.limit))
		w.Header().Set("RateLimit-Remaining", fmt.Sprintf("%d", remaining))

		if !allowed {
			response.RespondRateLimited(w, r, retryAfter)
			return
		}

		next.ServeHTTP(w, r)
	})
}
