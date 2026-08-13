package service

import (
	"context"
	"net/url"
	"time"

	"github.com/P-kaizoku/small-light/internal/cache"
	"github.com/P-kaizoku/small-light/internal/model"
	"github.com/google/uuid"
)

// LinkStore is the persistence surface LinkService needs. Defined here so
// LinkService is testable against a fake. *repository.LinkRepository satisfies it.
type LinkStore interface {
	Create(ctx context.Context, link *model.Link) (*model.Link, error)
	GetByShortCode(ctx context.Context, code string) (*model.Link, error)
	GetByID(ctx context.Context, id, ownerID uuid.UUID) (*model.Link, error)
	ListByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Link, error)
	Update(ctx context.Context, id, ownerID uuid.UUID, originalURL string, expiresAt time.Time) (*model.Link, error)
	SoftDelete(ctx context.Context, id, ownerID uuid.UUID) error
}

type LinkService struct {
	links      LinkStore
	defaultTTL time.Duration
	cache      *cache.Link
	clicks     *cache.ClickCounter
}

func NewLinkService(links LinkStore, defaultTTL time.Duration, linkCache *cache.Link, clicks *cache.ClickCounter) *LinkService {
	return &LinkService{links: links, defaultTTL: defaultTTL, cache: linkCache, clicks: clicks}
}

// validateURL returns a normalized URL or ErrInvalidURL. url.Parse is
// surprisingly permissive — "notaurl" parses fine (empty Scheme and Host) and
// "ftp://x" parses with an unsupported scheme — so we require an http/https
// scheme and a non-empty host.
func validateURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", ErrInvalidURL
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", ErrInvalidURL
	}
	return u.String(), nil

}

// Create shortens a URL and returns the stored link.
func (s *LinkService) Create(ctx context.Context, ownerID uuid.UUID, raw string) (*model.Link, error) {
	u, err := validateURL(raw)
	if err != nil {
		return nil, err
	}

	link := &model.Link{OwnerID: ownerID, OriginalURL: u, ExpiresAt: time.Now().Add(s.defaultTTL)}
	return s.links.Create(ctx, link)
}

// List returns all non-deleted links owned by ownerID.
// Thin passthrough to s.links.ListByOwner. The repo already returns an empty
// (non-nil) slice and any error.
func (s *LinkService) List(ctx context.Context, ownerID uuid.UUID) ([]model.Link, error) {
	list, err := s.links.ListByOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	return list, nil
}

// Get returns one of the owner's links by id.
// Thin passthrough to s.links.GetByID. The repo returns ErrNotFound for
// missing, foreign or soft-deleted links — that's already the right answer.
func (s *LinkService) Get(ctx context.Context, id, ownerID uuid.UUID) (*model.Link, error) {
	link, err := s.links.GetByID(ctx, id, ownerID)
	if err != nil {
		return nil, err
	}
	return link, nil
}

// Update replaces the URL and expiry of a link owned by ownerID (full replace,
// no partials). The repo guards ownership and returns ErrNotFound when the
// link doesn't exist or isn't yours. The cached entry is dropped so the
// redirect picks up the new values.
func (s *LinkService) Update(ctx context.Context, id, ownerID uuid.UUID, raw string, expiresAt time.Time) (*model.Link, error) {
	u, err := validateURL(raw)
	if err != nil {
		return nil, err
	}

	updatedLink, err := s.links.Update(ctx, id, ownerID, u, expiresAt)
	if err != nil {
		return nil, err
	}

	s.cache.Delete(ctx, updatedLink.ShortCode)
	return updatedLink, nil
}

// Delete soft-deletes one of the owner's links by id, then drops its cached
// entry so the redirect stops resolving.
func (s *LinkService) Delete(ctx context.Context, id, ownerID uuid.UUID) error {
	link, err := s.links.GetByID(ctx, id, ownerID)
	if err != nil {
		return err
	}
	if err := s.links.SoftDelete(ctx, id, ownerID); err != nil {
		return err
	}

	s.cache.Delete(ctx, link.ShortCode)
	return nil
}

// Resolve looks up a short code for the redirect handler, caching the result
// on a miss. Both paths re-check expiry, then record the click in Redis.
func (s *LinkService) Resolve(ctx context.Context, code string) (*model.Link, error) {
	link, err := s.getCached(ctx, code)
	if err != nil {
		return nil, err
	}
	if time.Now().After(link.ExpiresAt) {
		return nil, ErrLinkExpired
	}

	s.clicks.Increment(ctx, link.ID)
	return link, nil
}

// getCached returns the link from Redis or, on miss, from Postgres and warms
// the cache. Redis failures degrade to a miss — never a failed request.
func (s *LinkService) getCached(ctx context.Context, code string) (*model.Link, error) {
	if link, ok := s.cache.Get(ctx, code); ok {
		return link, nil
	}
	link, err := s.links.GetByShortCode(ctx, code)
	if err != nil {
		return nil, err
	}
	s.cache.Set(ctx, code, link)
	return link, nil
}
