// Package integration holds end-to-end tests that need real Postgres and
// Redis (the docker-compose dev stack). They are opt-in: every test calls
// requireIntegration, which skips unless TEST_INTEGRATION=1 is set, so
// `go test ./...` stays hermetic and CI/dev runs without docker still pass.
package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/P-kaizoku/small-light/internal/cache"
	"github.com/P-kaizoku/small-light/internal/config"
	"github.com/P-kaizoku/small-light/internal/database"
	"github.com/P-kaizoku/small-light/internal/handler"
	"github.com/P-kaizoku/small-light/internal/repository"
	"github.com/P-kaizoku/small-light/internal/server"
	"github.com/P-kaizoku/small-light/internal/service"
	"github.com/P-kaizoku/small-light/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// testRedisDB keeps integration tests off the dev database (0). The rate
// limiter and click counters are keyed inside this DB, so truncating/flushing
// it never touches real data.
const testRedisDB = 15

func requireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("TEST_INTEGRATION") == "" {
		t.Skip("set TEST_INTEGRATION=1 to run integration tests")
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type testEnv struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

// setup connects to the docker Postgres + Redis, runs migrations (idempotent),
// and wipes test data so every test starts clean. It owns the connections and
// closes them via t.Cleanup.
func setup(t *testing.T) testEnv {
	t.Helper()
	requireIntegration(t)

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, envOr("DATABASE_URL", "postgres://myuser:mysecretpassword@localhost:5432/smalllight"))
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	// Schema must exist before any test touches tables; RunMigrations is a
	// no-op when already applied (migrate.ErrNoChange is swallowed).
	migrateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := database.RunMigrations(migrateCtx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     envOr("REDIS_ADDR", "localhost:6379"),
		Password: envOr("REDIS_PASSWORD", "myredispassword"),
		DB:       testRedisDB,
	})
	t.Cleanup(func() { rdb.Close() })

	clean(t, pool, rdb)
	return testEnv{pool: pool, rdb: rdb}
}

// clean wipes both data stores: truncate resets links/users (RESTART IDENTITY
// also resets any owned sequences) and FlushDB clears every Redis key the
// middleware/cache workers left behind — including the rate-limit counters,
// so a test cannot inherit a half-used 20/min redirect budget.
func clean(t *testing.T, pool *pgxpool.Pool, rdb *redis.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `TRUNCATE click_events, links, users RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
}

// stack holds the assembled application wiring for full-stack HTTP tests. It
// mirrors cmd/server/main.go but runs through httptest instead of a real port.
type stack struct {
	ts       *httptest.Server
	env      testEnv
	linkRepo *repository.LinkRepository
	counter  *cache.ClickCounter
}

func newStack(t *testing.T) *stack {
	t.Helper()
	env := setup(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	userRepo := repository.NewUserRepository(env.pool)
	linkRepo := repository.NewLinkRepository(env.pool)
	linkCache := cache.NewLink(env.rdb)
	counter := cache.NewClickCounter(env.rdb)

	authSvc := service.NewAuthService(userRepo, []byte("test-secret"), time.Hour)
	linkSvc := service.NewLinkService(linkRepo, time.Hour, linkCache, counter)
	h := handler.New(authSvc, linkSvc, time.Hour, logger)

	cfg := config.Config{Port: "8080"} // Addr is unused; httptest supplies the listener
	srv := server.New(cfg, logger, env.pool, env.rdb, h)

	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	return &stack{ts: ts, env: env, linkRepo: linkRepo, counter: counter}
}

// flushClicks drives the real Clicks worker to a drained state: run it on a
// short interval, wait for at least one tick, then cancel and wait for the
// final flush. Using the worker's Run() (not an internal method) exercises the
// actual ticker + shutdown-drain path the binary uses.
func flushClicks(t *testing.T, st *stack) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := worker.NewClicks(st.counter, st.linkRepo, logger, 50*time.Millisecond)

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(runCtx)
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("click worker did not stop after cancel")
	}
}

// noRedirectClient stops the default redirect-following so 302s can be
// asserted on directly (the redirect endpoint is the thing under test).
var noRedirectClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}
