package service

import (
	"context"
	"net/url"
	"time"

	"github.com/P-kaizoku/small-light/internal/model"
	"github.com/google/uuid"
)

// NOTE ON IMPORTS: when you fill in the bodies below, add this import to this
// file:
//
//	"net/url"
//
// It is intentionally absent right now so the scaffold compiles with empty
// bodies.

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
}

func NewLinkService(links LinkStore, defaultTTL time.Duration) *LinkService {
	return &LinkService{links: links, defaultTTL: defaultTTL}
}

// validateURL returns a normalized URL or ErrInvalidURL.
//
// url.Parse is surprisingly permissive: "notaurl" parses fine (empty Scheme and
// Host), and "ftp://x" parses with a scheme you must reject. Check all three:
//   - parse error -> ErrInvalidURL
//   - u.Scheme is neither "http" nor "https" -> ErrInvalidURL
//   - u.Host == "" -> ErrInvalidURL
//
// Return u.String() on success.
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
//
//  1. url, err := validateURL(raw).
//  2. Build &model.Link{OwnerID: ownerID, OriginalURL: url,
//     ExpiresAt: time.Now().Add(s.defaultTTL)}.
//  3. return s.links.Create(ctx, link). The repo assigns short_code,
//     click_count and timestamps, and returns the complete link.
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
// no partials).
//
// validateURL(raw), then s.links.Update(ctx, id, ownerID, url, expiresAt).
// The repo guards ownership and returns ErrNotFound when the link doesn't exist
// or isn't yours.
func (s *LinkService) Update(ctx context.Context, id, ownerID uuid.UUID, raw string, expiresAt time.Time) (*model.Link, error) {
	url, err := validateURL(raw)
	if err != nil {
		return nil, err
	}

	updatedLink, err := s.links.Update(ctx, id, ownerID, url, expiresAt)
	if err != nil {
		return nil, err
	}

	return updatedLink, nil
}

// Delete soft-deletes one of the owner's links by id.
// Thin passthrough to s.links.SoftDelete (ownership + ErrNotFound handled there).
func (s *LinkService) Delete(ctx context.Context, id, ownerID uuid.UUID) error {
	err := s.links.SoftDelete(ctx, id, ownerID)
	if err != nil {
		return err
	}

	return nil
}

// Resolve looks up a short code for the redirect handler.
//
//  1. link, err := s.links.GetByShortCode(ctx, code); propagate err.
//  2. if time.Now().After(link.ExpiresAt) return ErrLinkExpired.
//  3. Return the link; the handler will 302 to link.OriginalURL.
func (s *LinkService) Resolve(ctx context.Context, code string) (*model.Link, error) {
	link, err := s.links.GetByShortCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if time.Now().After(link.ExpiresAt) {
		return nil, ErrLinkExpired
	}
	return link, nil
}
