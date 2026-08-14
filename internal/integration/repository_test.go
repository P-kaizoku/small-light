package integration

// Repository tests run against the real Postgres. They exercise the SQL
// directly (not through services), which is where ownership guards, soft
// deletes, and click deltas actually live.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/P-kaizoku/small-light/internal/model"
	"github.com/P-kaizoku/small-light/internal/repository"
	"github.com/google/uuid"
)

// seedUser creates a throwaway user row so links have a valid owner.
func seedUser(t *testing.T, env testEnv, email string) *model.User {
	t.Helper()
	u, err := repository.NewUserRepository(env.pool).Create(context.Background(), email, "hash")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func seedLink(t *testing.T, env testEnv, owner uuid.UUID, url string, expiresAt time.Time) *model.Link {
	t.Helper()
	l, err := repository.NewLinkRepository(env.pool).Create(context.Background(), &model.Link{
		OwnerID:     owner,
		OriginalURL: url,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		t.Fatalf("seed link: %v", err)
	}
	return l
}

// TestCreateAndGetByShortCode asserts a created link round-trips through the
// DB with a real (base62) short code and zero click count.
func TestCreateAndGetByShortCode(t *testing.T) {
	env := setup(t)
	u := seedUser(t, env, "repo@test.com")
	created := seedLink(t, env, u.ID, "https://example.com", time.Now().Add(time.Hour))

	got, err := repository.NewLinkRepository(env.pool).GetByShortCode(context.Background(), created.ShortCode)
	if err != nil {
		t.Fatalf("GetByShortCode: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %v, want %v", got.ID, created.ID)
	}
	if got.OriginalURL != "https://example.com" {
		t.Errorf("OriginalURL = %q, want %q", got.OriginalURL, "https://example.com")
	}
	if got.ShortCode == "" {
		t.Error("ShortCode is empty, want a base62 code")
	}
	if got.ClickCount != 0 {
		t.Errorf("ClickCount = %d, want 0", got.ClickCount)
	}
}

// TestGetByShortCodeNotFound asserts an unknown code maps to ErrNotFound, not
// a raw pgx no-rows error.
func TestGetByShortCodeNotFound(t *testing.T) {
	env := setup(t)
	repo := repository.NewLinkRepository(env.pool)

	if _, err := repo.GetByShortCode(context.Background(), "zzzzzz"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestSoftDeleteHidesLink asserts a soft-deleted link is invisible to every
// read path (by code, by id, and in the owner's list) while the row remains.
func TestSoftDeleteHidesLink(t *testing.T) {
	env := setup(t)
	u := seedUser(t, env, "soft@test.com")
	link := seedLink(t, env, u.ID, "https://example.com", time.Now().Add(time.Hour))
	repo := repository.NewLinkRepository(env.pool)
	ctx := context.Background()

	if err := repo.SoftDelete(ctx, link.ID, u.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	if _, err := repo.GetByShortCode(ctx, link.ShortCode); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("GetByShortCode after delete: err = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetByID(ctx, link.ID, u.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("GetByID after delete: err = %v, want ErrNotFound", err)
	}
	links, err := repo.ListByOwner(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("ListByOwner returned %d links, want 0", len(links))
	}
}

// TestOwnershipGuard asserts every owner-scoped operation refuses a foreign
// owner with ErrNotFound. Required — without the AND owner_id = $2 in the SQL,
// any authenticated user could mutate someone else's links.
func TestOwnershipGuard(t *testing.T) {
	env := setup(t)
	owner := seedUser(t, env, "owner@test.com")
	other := seedUser(t, env, "other@test.com")
	link := seedLink(t, env, owner.ID, "https://example.com", time.Now().Add(time.Hour))
	repo := repository.NewLinkRepository(env.pool)
	ctx := context.Background()

	if _, err := repo.GetByID(ctx, link.ID, other.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("GetByID foreign: err = %v, want ErrNotFound", err)
	}
	if _, err := repo.Update(ctx, link.ID, other.ID, "https://evil.com", time.Now().Add(time.Hour)); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("Update foreign: err = %v, want ErrNotFound", err)
	}
	if err := repo.SoftDelete(ctx, link.ID, other.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("SoftDelete foreign: err = %v, want ErrNotFound", err)
	}

	// The owner's link must be untouched after all the foreign attempts.
	if _, err := repo.GetByID(ctx, link.ID, owner.ID); err != nil {
		t.Errorf("owner link vanished after foreign attempts: %v", err)
	}
}

// TestApplyClickDeltas asserts worker-flushed deltas accumulate in Postgres.
func TestApplyClickDeltas(t *testing.T) {
	env := setup(t)
	u := seedUser(t, env, "clicks@test.com")
	link := seedLink(t, env, u.ID, "https://example.com", time.Now().Add(time.Hour))
	repo := repository.NewLinkRepository(env.pool)
	ctx := context.Background()

	if err := repo.ApplyClickDeltas(ctx, map[uuid.UUID]int64{link.ID: 5}); err != nil {
		t.Fatalf("ApplyClickDeltas: %v", err)
	}
	got, err := repo.GetByID(ctx, link.ID, u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ClickCount != 5 {
		t.Errorf("ClickCount = %d, want 5", got.ClickCount)
	}

	// A second flush must ADD, not overwrite.
	if err := repo.ApplyClickDeltas(ctx, map[uuid.UUID]int64{link.ID: 3}); err != nil {
		t.Fatalf("ApplyClickDeltas 2: %v", err)
	}
	got, err = repo.GetByID(ctx, link.ID, u.ID)
	if err != nil {
		t.Fatalf("GetByID 2: %v", err)
	}
	if got.ClickCount != 8 {
		t.Errorf("ClickCount = %d, want 8 (3 + 5)", got.ClickCount)
	}
}

// TestSoftDeleteExpired asserts the cleanup worker's query purges only links
// past their expiry, leaves active links alone, and is idempotent (a second
// run finds nothing).
func TestSoftDeleteExpired(t *testing.T) {
	env := setup(t)
	u := seedUser(t, env, "expiry@test.com")
	repo := repository.NewLinkRepository(env.pool)
	ctx := context.Background()

	expired := seedLink(t, env, u.ID, "https://expired.com", time.Now().Add(-time.Hour))
	active := seedLink(t, env, u.ID, "https://active.com", time.Now().Add(time.Hour))

	n, err := repo.SoftDeleteExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("SoftDeleteExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("purged %d rows, want 1", n)
	}

	if _, err := repo.GetByShortCode(ctx, expired.ShortCode); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("expired link still resolvable: err = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetByShortCode(ctx, active.ShortCode); err != nil {
		t.Errorf("active link was purged: %v", err)
	}

	n, err = repo.SoftDeleteExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("SoftDeleteExpired 2: %v", err)
	}
	if n != 0 {
		t.Errorf("second purge returned %d, want 0 (idempotent)", n)
	}
}
