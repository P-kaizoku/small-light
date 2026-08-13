package middleware

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimitKey returns the Redis key that counts a client's requests in the
// current window. It is keyed by IP (the port is stripped from RemoteAddr), so
// the bucket is per-client. Trade-off: clients behind a shared NAT or office
// proxy all share one bucket. If per-token limits are needed later, key on
// UserID(r.Context()) instead — but the auth endpoints (register/login) carry
// no token, which is exactly why an IP-based default is used here.
func rateLimitKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return "rl:" + host
}

// RateLimit implements a fixed-window counter in Redis: at most `limit`
// requests per `window` per client, after which the client gets 429.
//
// How it works: every request INCRs the client's key, and the INCR result is
// the number of requests already seen in this window — the first request gets
// 1, the second gets 2, and so on. The first request also sets the key's TTL
// to `window`, so the counter (and its memory) expires once the window closes.
// This is a fixed-window (not sliding) limiter: cheap and simple, at the cost
// of allowing up to 2x the limit in a burst right at a window boundary.
//
// Redis failures FAIL OPEN, matching the cache package's policy: a Redis
// outage must never block traffic, so the request is let through.
func RateLimit(rdb *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			key := rateLimitKey(r)

			n, err := rdb.Incr(ctx, key).Result()
			if err != nil {
				// Redis down: fail open, swallow the error (see cache package).
				next.ServeHTTP(w, r)
				return
			}

			if n == 1 {
				// First request in this window: arm the expiry.
				rdb.Expire(ctx, key, window)
			}

			if n > int64(limit) {
				w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
				writeJSONError(w, http.StatusTooManyRequests, "too many requests")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
