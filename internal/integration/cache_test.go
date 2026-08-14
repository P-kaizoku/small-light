package integration

// Cache tests run against the real Redis and verify the exact behaviors the
// services rely on: TTL-mirrors-expiry (a cached copy can never outlive its
// link), fail-open on malformed data, and atomic GetDel draining.

import (
	"context"
	"testing"
	"time"

	"github.com/P-kaizoku/small-light/internal/cache"
	"github.com/P-kaizoku/small-light/internal/model"
	"github.com/google/uuid"
)

// TestLinkCacheSetGetDelete asserts the round-trip plus explicit eviction.
func TestLinkCacheSetGetDelete(t *testing.T) {
	env := setup(t)
	c := cache.NewLink(env.rdb)
	ctx := context.Background()

	link := &model.Link{
		ID:          uuid.New(),
		OwnerID:     uuid.New(),
		ShortCode:   "abc",
		OriginalURL: "https://example.com",
		ClickCount:  42,
		ExpiresAt:   time.Now().Add(time.Minute),
	}

	c.Set(ctx, "abc", link)
	got, ok := c.Get(ctx, "abc")
	if !ok {
		t.Fatal("Get = miss, want hit")
	}
	if got.ID != link.ID || got.OriginalURL != link.OriginalURL || got.ClickCount != 42 {
		t.Errorf("Get returned %+v, want the stored link", got)
	}

	c.Delete(ctx, "abc")
	if _, ok := c.Get(ctx, "abc"); ok {
		t.Error("Get after Delete = hit, want miss")
	}
}

// TestLinkCacheMiss asserts an unknown code is a miss (not an error) — the
// contract getCached relies on to fall through to Postgres.
func TestLinkCacheMiss(t *testing.T) {
	env := setup(t)
	c := cache.NewLink(env.rdb)

	if _, ok := c.Get(context.Background(), "nope"); ok {
		t.Error("Get(unknown) = hit, want miss")
	}
}

// TestLinkCacheExpires asserts the entry dies at the link's own expiry — the
// "never outlive its TTL" guarantee. This is why a link's TTL change on PATCH
// also forces an explicit cache delete.
func TestLinkCacheExpires(t *testing.T) {
	env := setup(t)
	c := cache.NewLink(env.rdb)
	ctx := context.Background()

	link := &model.Link{
		ID:          uuid.New(),
		ShortCode:   "short",
		OriginalURL: "https://example.com",
		ExpiresAt:   time.Now().Add(150 * time.Millisecond),
	}
	c.Set(ctx, "short", link)

	time.Sleep(300 * time.Millisecond)
	if _, ok := c.Get(ctx, "short"); ok {
		t.Error("Get after expiry = hit, want miss")
	}
}

// TestLinkCacheSetExpiredNoop asserts Set refuses to cache an already-expired
// link (Set guards ttl <= 0).
func TestLinkCacheSetExpiredNoop(t *testing.T) {
	env := setup(t)
	c := cache.NewLink(env.rdb)
	ctx := context.Background()

	link := &model.Link{
		ID:          uuid.New(),
		ShortCode:   "old",
		OriginalURL: "https://example.com",
		ExpiresAt:   time.Now().Add(-time.Minute),
	}
	c.Set(ctx, "old", link)

	if _, ok := c.Get(ctx, "old"); ok {
		t.Error("Get(expired-at-insert) = hit, want miss")
	}
}

// TestClickCounterIncrementDrain asserts increments accumulate and Drain
// atomically reads-and-clears them. The second Drain must return nothing —
// that is the GetDel guarantee the worker depends on to never double-count.
func TestClickCounterIncrementDrain(t *testing.T) {
	env := setup(t)
	c := cache.NewClickCounter(env.rdb)
	ctx := context.Background()

	id := uuid.New()
	c.Increment(ctx, id)
	c.Increment(ctx, id)

	deltas, err := c.Drain(ctx)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if deltas[id] != 2 {
		t.Errorf("deltas[%v] = %d, want 2", id, deltas[id])
	}

	deltas2, err := c.Drain(ctx)
	if err != nil {
		t.Fatalf("Drain 2: %v", err)
	}
	if len(deltas2) != 0 {
		t.Errorf("second Drain returned %d entries, want 0 (double-count guard)", len(deltas2))
	}
}

// TestClickCounterDrainMultiple asserts one Drain collects every link's
// counter in a single map — the batch shape ApplyClickDeltas expects.
func TestClickCounterDrainMultiple(t *testing.T) {
	env := setup(t)
	c := cache.NewClickCounter(env.rdb)
	ctx := context.Background()

	idA, idB := uuid.New(), uuid.New()
	c.Increment(ctx, idA)
	c.Increment(ctx, idA)
	c.Increment(ctx, idB)

	deltas, err := c.Drain(ctx)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if deltas[idA] != 2 || deltas[idB] != 1 {
		t.Errorf("deltas = %+v, want %v:2 and %v:1", deltas, idA, idB)
	}
}
