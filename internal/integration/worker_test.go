package integration

// Worker tests drive the real Run() loops (ticker + cancel) with a short
// interval, exercising the exact shutdown-drain path the binary uses — not an
// unexported method.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/P-kaizoku/small-light/internal/cache"
	"github.com/P-kaizoku/small-light/internal/repository"
	"github.com/P-kaizoku/small-light/internal/worker"
)

// TestClicksFlushPersistsClicks asserts clicks buffered in Redis end up in
// Postgres after the worker's tick, and that Redis is drained (not
// double-counted). This is the async-analytics promise end to end.
func TestClicksFlushPersistsClicks(t *testing.T) {
	env := setup(t)
	u := seedUser(t, env, "worker-clicks@test.com")
	link := seedLink(t, env, u.ID, "https://example.com", time.Now().Add(time.Hour))
	ctx := context.Background()

	counter := cache.NewClickCounter(env.rdb)
	counter.Increment(ctx, link.ID)
	counter.Increment(ctx, link.ID)

	linkRepo := repository.NewLinkRepository(env.pool)
	w := worker.NewClicks(counter, linkRepo, slog.New(slog.NewTextHandler(io.Discard, nil)), 50*time.Millisecond)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(runCtx)
	}()

	time.Sleep(300 * time.Millisecond) // let the ticker fire at least once
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("click worker did not stop after cancel")
	}

	got, err := linkRepo.GetByID(ctx, link.ID, u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ClickCount != 2 {
		t.Errorf("ClickCount = %d, want 2 (both increments flushed)", got.ClickCount)
	}

	// Postgres got the clicks, so Redis must be empty.
	deltas, err := counter.Drain(ctx)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(deltas) != 0 {
		t.Errorf("Redis still holds %+v, want drained", deltas)
	}
}

// TestCleanupPurgesExpired asserts the cleanup worker soft-deletes links past
// their expiry while leaving active links alone, and that the final purge on
// cancel does not resurrect or duplicate work.
func TestCleanupPurgesExpired(t *testing.T) {
	env := setup(t)
	u := seedUser(t, env, "worker-cleanup@test.com")
	expired := seedLink(t, env, u.ID, "https://expired.com", time.Now().Add(-time.Hour))
	active := seedLink(t, env, u.ID, "https://active.com", time.Now().Add(time.Hour))

	linkRepo := repository.NewLinkRepository(env.pool)
	w := worker.NewCleanup(linkRepo, slog.New(slog.NewTextHandler(io.Discard, nil)), 50*time.Millisecond)

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
		t.Fatal("cleanup worker did not stop after cancel")
	}

	ctx := context.Background()
	if _, err := linkRepo.GetByShortCode(ctx, expired.ShortCode); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("expired link still resolvable: err = %v, want ErrNotFound", err)
	}
	if _, err := linkRepo.GetByShortCode(ctx, active.ShortCode); err != nil {
		t.Errorf("active link was purged: %v", err)
	}
}
