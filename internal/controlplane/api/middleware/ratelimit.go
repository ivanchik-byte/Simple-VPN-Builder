package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
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
	// sessionValidator reports whether a session token is genuine.
	// Verified sessions bypass IP throttling; forged cookies do not.
	sessionValidator func(token string) bool
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

// SetSessionValidator installs an optional genuine-session check.
func (rl *RateLimiter) SetSessionValidator(fn func(token string) bool) {
	rl.sessionValidator = fn
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
		// Health checks and metrics should never be rate limited (CRIT-13 / K8s readiness)
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		// Never rate limit static assets, favicon, or UI telemetry polling
		if strings.HasPrefix(r.URL.Path, "/admin/static/") ||
			r.URL.Path == "/favicon.ico" ||
			strings.HasPrefix(r.URL.Path, "/admin/partials/") {
			next.ServeHTTP(w, r)
			return
		}

		// Verified admin sessions bypass IP throttling; forged cookies stay limited.
		if rl.sessionValidator != nil {
			if cookie, err := r.Cookie("vpn_admin_token"); err == nil {
				if rl.sessionValidator(strings.TrimSpace(cookie.Value)) {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

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
			// API clients always get RFC 7807 JSON, regardless of Accept headers.
			if strings.HasPrefix(r.URL.Path, "/api/") {
				response.RespondRateLimited(w, r, retryAfter)
				return
			}
			if strings.Contains(r.Header.Get("Accept"), "text/html") || strings.HasPrefix(r.URL.Path, "/admin/") {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en" class="dark"><head><meta charset="utf-8"><title>429: Rate Limit Exceeded</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>body{background:#09090b;color:#f4f4f6;font-family:Inter,-apple-system,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;padding:1rem;}
.card{width:100%%;max-width:28rem;border:1px solid rgba(255,255,255,0.08);border-radius:0.75rem;background:#121215;padding:2rem;box-shadow:0 20px 50px rgba(0,0,0,0.5);text-align:center;position:relative;overflow:hidden;}
.glow{position:absolute;top:0;left:0;right:0;height:6rem;background:radial-gradient(60%% 100%% at 50%% 0%%,rgba(248,113,113,0.14),transparent 70%%);pointer-events:none;}
.code{display:inline-flex;align-items:center;justify-content:center;padding:0 1.25rem;height:3.5rem;border-radius:1rem;background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.08);margin-bottom:1rem;font-family:monospace;font-size:1.5rem;font-weight:700;color:#f87171;}
h1{font-size:1.125rem;font-weight:600;margin:0 0 0.5rem;}
p{font-size:0.75rem;color:#71717a;font-family:monospace;margin:0 0 1.5rem;line-height:1.6;}
.count{font-family:monospace;margin-bottom:1.5rem;border:1px solid rgba(255,255,255,0.08);background:#09090b;border-radius:0.5rem;padding:0.625rem 1rem;font-size:0.75rem;color:#a1a1aa;}
.count b{color:#f4f4f6;}
.row{display:flex;gap:0.75rem;justify-content:center;}
.btn{padding:0.5rem 1rem;border-radius:0.5rem;font-size:0.75rem;font-weight:500;text-transform:uppercase;letter-spacing:0.05em;text-decoration:none;transition:opacity 0.15s;}
.primary{background:#f4f4f6;color:#09090b;}
.ghost{border:1px solid rgba(255,255,255,0.14);color:#a1a1aa;background:rgba(255,255,255,0.04);cursor:pointer;font-family:monospace;}</style></head>
<body><div class="card"><div class="glow"></div><div style="position:relative">
<div class="code">429</div>
<h1>Rate Limit Exceeded</h1>
<p>Too many requests. Slow down and try again in a moment.</p>
<div class="count">Retrying in <b id="err-countdown">%d</b>s (or retry right now).</div>
<div class="row"><a class="btn primary" href="javascript:location.reload()">Retry Now</a><button class="btn ghost" onclick="window.history.back()">Go Back</button></div>
</div></div>
<script>(function(){var s=%d;var el=document.getElementById('err-countdown');var iv=setInterval(function(){s-=1;if(s<=0){clearInterval(iv);location.reload();return;}if(el)el.textContent=s;},1000);})();</script>
</body></html>`, retryAfter, retryAfter)
				return
			}
			response.RespondRateLimited(w, r, retryAfter)
			return
		}

		next.ServeHTTP(w, r)
	})
}
