package middleware

// HOW TO TEST RateLimit:
//
// Use miniredis — an in-process Redis server that needs no docker, so this
// stays a UNIT test:
//
//	mr := miniredis.RunT(t)                       // starts + auto-shuts down
//	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
//	t.Cleanup(func() { rdb.Close() })
//
// Then build the middleware and serve requests through it:
//
//	rl := RateLimit(rdb, limit, window)
//	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//		w.WriteHeader(http.StatusOK)
//	})
//	req := httptest.NewRequest(http.MethodGet, "/", nil)
//	rec := httptest.NewRecorder()
//	rl(next).ServeHTTP(rec, req)
//
// NOTE: httptest.NewRequest sets RemoteAddr to "192.0.2.1:1234". To test
// per-IP separation, override it: req.RemoteAddr = "192.0.2.2:1234".

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestRateLimitAllowsWithinLimit asserts the fixed-window counter: with limit
// 2, window 1m, two requests from the same client pass (200) and the third
// gets 429. Required — this is the core "N requests per window" contract.
func TestRateLimitAllowsWithinLimit(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	rl := RateLimit(rdb, 2, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		rl(next).ServeHTTP(rec, req)

		want := http.StatusOK
		if i > 2 {
			want = http.StatusTooManyRequests
		}
		if rec.Code != want {
			t.Errorf("request %d: status = %d, want %d", i, rec.Code, want)
		}
	}
}

// TestRateLimitSeparatesByIP asserts buckets are per-client: with limit 1, a
// request from IP A and one from IP B must BOTH pass. Required — a shared
// bucket would let one client starve everyone else behind it.
func TestRateLimitSeparatesByIP(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	rl := RateLimit(rdb, 1, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, ip := range []string{"192.0.2.1:1234", "192.0.2.2:1234"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		rl(next).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("ip %s: status = %d, want 200 (separate buckets)", ip, rec.Code)
		}
	}
}

// TestRateLimitSetsRetryAfter asserts the 429 response advertises how long the
// client must wait. Required — Retry-After is the standard hint a rate-limited
// client needs to back off correctly.
func TestRateLimitSetsRetryAfter(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	rl := RateLimit(rdb, 1, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Request 1 consumes the single slot; request 2 trips the limit.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		rl(next).ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	rl(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want %q", got, "60")
	}
}

// TestRateLimitFailOpen asserts a Redis outage does NOT block traffic.
// Required — this matches the cache package's policy: a Redis failure must
// degrade the service, never take it down.
func TestRateLimitFailOpen(t *testing.T) {
	// Point the client at a dead port; INCR will error on every request.
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1"})
	t.Cleanup(func() { rdb.Close() })

	rl := RateLimit(rdb, 2, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	rl(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (fail open on Redis outage)", rec.Code)
	}
}
