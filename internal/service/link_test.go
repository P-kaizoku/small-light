package service

// HOW TO TEST LinkService:
//
// It depends on three interfaces — LinkStore, LinkCache, ClickIncrementer —
// so three tiny fakes give full control over every code path with no database
// or Redis. The fakes below are completed.
//
// The three behaviors to verify in each test:
//   - what reaches the store (arguments passed to it),
//   - whether the cache was touched (Get/Set/Delete),
//   - whether a click was recorded (Increment).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/P-kaizoku/small-light/internal/model"
	"github.com/google/uuid"
)

// --- completed fakes -------------------------------------------------------

type fakeLinkStore struct {
	byCode  map[string]*model.Link
	byID    map[uuid.UUID]*model.Link
	getErr  error         // error to return from GetByShortCode
	listErr error         // error to return from ListByOwner
	created []*model.Link // every link passed to Create, in order
	updated []*model.Link // every link passed to Update
	deleted []uuid.UUID   // every id passed to SoftDelete
}

func (f *fakeLinkStore) Create(_ context.Context, link *model.Link) (*model.Link, error) {
	f.created = append(f.created, link)
	f.byCode[link.ShortCode] = link
	f.byID[link.ID] = link
	return link, nil
}

func (f *fakeLinkStore) GetByShortCode(_ context.Context, code string) (*model.Link, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	l, ok := f.byCode[code]
	if !ok {
		return nil, errors.New("not found")
	}
	return l, nil
}

func (f *fakeLinkStore) GetByID(_ context.Context, id, ownerID uuid.UUID) (*model.Link, error) {
	l, ok := f.byID[id]
	if !ok || l.OwnerID != ownerID {
		return nil, errors.New("not found")
	}
	return l, nil
}

func (f *fakeLinkStore) ListByOwner(_ context.Context, ownerID uuid.UUID) ([]model.Link, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []model.Link
	for _, l := range f.byID {
		if l.OwnerID == ownerID {
			out = append(out, *l)
		}
	}
	return out, nil
}

func (f *fakeLinkStore) Update(_ context.Context, id, ownerID uuid.UUID, originalURL string, expiresAt time.Time) (*model.Link, error) {
	l, ok := f.byID[id]
	if !ok || l.OwnerID != ownerID {
		return nil, errors.New("not found")
	}
	l.OriginalURL = originalURL
	l.ExpiresAt = expiresAt
	f.updated = append(f.updated, l)
	return l, nil
}

func (f *fakeLinkStore) SoftDelete(_ context.Context, id, ownerID uuid.UUID) error {
	if _, ok := f.byID[id]; !ok {
		return errors.New("not found")
	}
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeLinkCache struct {
	data        map[string]*model.Link
	getCalls    []string
	setCalls    []string
	deleteCalls []string
}

func (f *fakeLinkCache) Get(_ context.Context, code string) (*model.Link, bool) {
	f.getCalls = append(f.getCalls, code)
	l, ok := f.data[code]
	return l, ok
}

func (f *fakeLinkCache) Set(_ context.Context, code string, link *model.Link) {
	f.setCalls = append(f.setCalls, code)
	if f.data == nil {
		f.data = map[string]*model.Link{}
	}
	f.data[code] = link
}

func (f *fakeLinkCache) Delete(_ context.Context, code string) {
	f.deleteCalls = append(f.deleteCalls, code)
}

type fakeClickIncrementer struct {
	clicked []uuid.UUID
}

func (f *fakeClickIncrementer) Increment(_ context.Context, id uuid.UUID) {
	f.clicked = append(f.clicked, id)
}

// stubLink builds a stored link for seeding fakes.
func stubLink(id uuid.UUID, code, url string, expiresAt time.Time) *model.Link {
	return &model.Link{ID: id, ShortCode: code, OriginalURL: url, ExpiresAt: expiresAt}
}

// --- tests -----------------------------------------------------------------

// TestValidateURL is a table test over the pure function. Required — this is
// the gate that keeps garbage out of the database, and url.Parse's permissive
// behavior is easy to regress.
func TestValidateURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"https", "https://example.com", nil},
		{"http with path and query", "http://a.co/x?q=1", nil},
		{"no scheme", "notaurl", ErrInvalidURL},
		{"ftp scheme", "ftp://example.com", ErrInvalidURL},
		{"mailto scheme", "mailto:x@y.z", ErrInvalidURL},
		{"empty", "", ErrInvalidURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateURL(tt.in)
			if !errors.Is(err, tt.want) {
				t.Errorf("validateURL(%q) err = %v, want %v", tt.in, err, tt.want)
			}
		})
	}
}

// TestCreateValidatesURL asserts an invalid URL never reaches the store.
func TestCreateValidatesURL(t *testing.T) {
	store := &fakeLinkStore{byCode: map[string]*model.Link{}, byID: map[uuid.UUID]*model.Link{}}
	svc := NewLinkService(store, time.Hour, &fakeLinkCache{}, &fakeClickIncrementer{})

	_, err := svc.Create(context.Background(), uuid.New(), "notaurl")
	if !errors.Is(err, ErrInvalidURL) {
		t.Errorf("err = %v, want ErrInvalidURL", err)
	}
	if len(store.created) != 0 {
		t.Errorf("store got %d Create calls, want 0", len(store.created))
	}
}

// TestCreateSetsDefaultTTL asserts the created link expires roughly
// now+defaultTTL. Required — a wrong TTL means links either expire instantly
// or never (the 7-day promise in the PRD breaks).
func TestCreateSetsDefaultTTL(t *testing.T) {
	store := &fakeLinkStore{byCode: map[string]*model.Link{}, byID: map[uuid.UUID]*model.Link{}}
	svc := NewLinkService(store, time.Hour, &fakeLinkCache{}, &fakeClickIncrementer{})

	link, err := svc.Create(context.Background(), uuid.New(), "https://example.com")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Allow generous slack — we only assert "about an hour out", not a fixed
	// timestamp, since time.Now() moves between calls.
	got := time.Until(link.ExpiresAt)
	if got < 59*time.Minute || got > time.Hour+time.Minute {
		t.Errorf("time.Until(ExpiresAt) = %v, want ~1h", got)
	}
}

// TestUpdateInvalidURL asserts an invalid URL fails before touching the cache.
// Required — a failed update must not evict a still-valid cached redirect.
func TestUpdateInvalidURL(t *testing.T) {
	owner := uuid.New()
	id := uuid.New()
	store := &fakeLinkStore{
		byCode: map[string]*model.Link{},
		byID: map[uuid.UUID]*model.Link{
			id: stubLink(id, "abc", "https://example.com", time.Now().Add(time.Hour)),
		},
	}
	cache := &fakeLinkCache{}
	svc := NewLinkService(store, time.Hour, cache, &fakeClickIncrementer{})

	_, err := svc.Update(context.Background(), id, owner, "notaurl", time.Now().Add(2*time.Hour))
	if !errors.Is(err, ErrInvalidURL) {
		t.Errorf("err = %v, want ErrInvalidURL", err)
	}
	if len(cache.deleteCalls) != 0 {
		t.Errorf("cache deleteCalls = %v, want none", cache.deleteCalls)
	}
}

// TestUpdateDeletesCache asserts a successful update evicts the cached entry
// so the redirect picks up the new URL immediately.
func TestUpdateDeletesCache(t *testing.T) {
	owner := uuid.New()
	id := uuid.New()
	link := stubLink(id, "abc", "https://example.com", time.Now().Add(time.Hour))
	link.OwnerID = owner
	store := &fakeLinkStore{
		byCode: map[string]*model.Link{"abc": link},
		byID:   map[uuid.UUID]*model.Link{id: link},
	}
	cache := &fakeLinkCache{}
	svc := NewLinkService(store, time.Hour, cache, &fakeClickIncrementer{})

	updated, err := svc.Update(context.Background(), id, owner, "https://new.example.com", time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.OriginalURL != "https://new.example.com" {
		t.Errorf("OriginalURL = %q, want %q", updated.OriginalURL, "https://new.example.com")
	}
	if len(cache.deleteCalls) != 1 || cache.deleteCalls[0] != "abc" {
		t.Errorf("cache deleteCalls = %v, want [abc]", cache.deleteCalls)
	}
}

// TestDeleteDeletesCache asserts a soft delete also evicts the cache — a
// deleted link must stop resolving even though its Redis entry still has TTL.
func TestDeleteDeletesCache(t *testing.T) {
	owner := uuid.New()
	id := uuid.New()
	link := stubLink(id, "abc", "https://example.com", time.Now().Add(time.Hour))
	link.OwnerID = owner
	store := &fakeLinkStore{
		byCode: map[string]*model.Link{"abc": link},
		byID:   map[uuid.UUID]*model.Link{id: link},
	}
	cache := &fakeLinkCache{}
	svc := NewLinkService(store, time.Hour, cache, &fakeClickIncrementer{})

	if err := svc.Delete(context.Background(), id, owner); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != id {
		t.Errorf("store deleted = %v, want [%v]", store.deleted, id)
	}
	if len(cache.deleteCalls) != 1 || cache.deleteCalls[0] != "abc" {
		t.Errorf("cache deleteCalls = %v, want [abc]", cache.deleteCalls)
	}
}

// TestResolveCacheHit asserts a cached link is served WITHOUT hitting the
// store, and the click is still recorded. Required — this is the hot path the
// read cache exists for; skipping the store is the whole point.
func TestResolveCacheHit(t *testing.T) {
	id := uuid.New()
	link := stubLink(id, "abc", "https://example.com", time.Now().Add(time.Hour))
	store := &fakeLinkStore{} // empty byCode: a store read would fail the test
	cache := &fakeLinkCache{data: map[string]*model.Link{"abc": link}}
	clicks := &fakeClickIncrementer{}
	svc := NewLinkService(store, time.Hour, cache, clicks)

	resolved, err := svc.Resolve(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved != link {
		t.Error("Resolve returned a different link than the cached one")
	}
	if len(clicks.clicked) != 1 || clicks.clicked[0] != id {
		t.Errorf("clicks = %v, want [%v]", clicks.clicked, id)
	}
}

// TestResolveMissFetchesStore asserts a cache miss fetches from the store,
// warms the cache, and records the click. Required — the miss path must not
// forget to populate the cache, or every request would hit Postgres.
func TestResolveMissFetchesStore(t *testing.T) {
	id := uuid.New()
	link := stubLink(id, "abc", "https://example.com", time.Now().Add(time.Hour))
	store := &fakeLinkStore{
		byCode: map[string]*model.Link{"abc": link},
		byID:   map[uuid.UUID]*model.Link{id: link},
	}
	cache := &fakeLinkCache{}
	clicks := &fakeClickIncrementer{}
	svc := NewLinkService(store, time.Hour, cache, clicks)

	resolved, err := svc.Resolve(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved != link {
		t.Error("Resolve returned a different link than the store's")
	}
	if len(cache.setCalls) != 1 || cache.setCalls[0] != "abc" {
		t.Errorf("cache setCalls = %v, want [abc]", cache.setCalls)
	}
	if len(clicks.clicked) != 1 || clicks.clicked[0] != id {
		t.Errorf("clicks = %v, want [%v]", clicks.clicked, id)
	}
}

// TestResolveExpired asserts an expired link returns ErrLinkExpired AND no
// click is recorded. Required — clicks on dead links would corrupt analytics.
func TestResolveExpired(t *testing.T) {
	expired := stubLink(uuid.New(), "abc", "https://example.com", time.Now().Add(-time.Minute))
	cache := &fakeLinkCache{data: map[string]*model.Link{"abc": expired}}
	clicks := &fakeClickIncrementer{}
	svc := NewLinkService(&fakeLinkStore{}, time.Hour, cache, clicks)

	_, err := svc.Resolve(context.Background(), "abc")
	if !errors.Is(err, ErrLinkExpired) {
		t.Errorf("err = %v, want ErrLinkExpired", err)
	}
	if len(clicks.clicked) != 0 {
		t.Errorf("clicks = %v, want none for an expired link", clicks.clicked)
	}
}

// TestResolveStoreError asserts a store failure propagates and no click is
// recorded. Required — a DB failure must fail the redirect (it cannot be
// served) without polluting click counts.
func TestResolveStoreError(t *testing.T) {
	storeErr := errors.New("db down")
	store := &fakeLinkStore{getErr: storeErr}
	clicks := &fakeClickIncrementer{}
	svc := NewLinkService(store, time.Hour, &fakeLinkCache{}, clicks)

	_, err := svc.Resolve(context.Background(), "abc")
	if !errors.Is(err, storeErr) {
		t.Errorf("err = %v, want %v", err, storeErr)
	}
	if len(clicks.clicked) != 0 {
		t.Errorf("clicks = %v, want none on store error", clicks.clicked)
	}
}
