package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/P-kaizoku/small-light/internal/model"
	"github.com/P-kaizoku/small-light/internal/shortcode"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LinkRepository struct {
	pool *pgxpool.Pool
}

func NewLinkRepository(pool *pgxpool.Pool) *LinkRepository {
	return &LinkRepository{
		pool: pool,
	}
}

func (r *LinkRepository) NextShortCode(ctx context.Context) (string, error) {
	var n int64

	if err := r.pool.QueryRow(ctx, `SELECT nextval('link_seq')`).Scan(&n); err != nil {
		return "", fmt.Errorf("nextval: %w", err)
	}
	return shortcode.Encode(uint64(n)), nil

}

const linkColumns = `id, owner_id, original_url, short_code, click_count, expires_at, created_at, updated_at`

func scanLink(row pgx.Row) (model.Link, error) {
	var l model.Link
	err := row.Scan(&l.ID, &l.OwnerID, &l.OriginalURL, &l.ShortCode, &l.ClickCount, &l.ExpiresAt, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

func (r *LinkRepository) Create(ctx context.Context, link *model.Link) (*model.Link, error) {
	shortCode, err := r.NextShortCode(ctx)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`INSERT INTO links (owner_id, original_url, short_code, expires_at) VALUES($1, $2, $3, $4) RETURNING %s`, linkColumns)
	l, err := scanLink(r.pool.QueryRow(ctx, query, link.OwnerID, link.OriginalURL, shortCode, link.ExpiresAt))
	if err != nil {
		return nil, translateError(err)
	}
	return &l, nil
}

func (r *LinkRepository) GetByShortCode(ctx context.Context, code string) (*model.Link, error) {
	query := fmt.Sprintf(`SELECT %s FROM links WHERE short_code = $1 AND deleted_at IS NULL`, linkColumns)
	l, err := scanLink(r.pool.QueryRow(ctx, query, code))
	if err != nil {
		return nil, translateError(err)
	}
	return &l, nil
}

func (r *LinkRepository) GetByID(ctx context.Context, id, ownerID uuid.UUID) (*model.Link, error) {
	query := fmt.Sprintf(`SELECT %s FROM links WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL`, linkColumns)
	l, err := scanLink(r.pool.QueryRow(ctx, query, id, ownerID))
	if err != nil {
		return nil, translateError(err)
	}
	return &l, nil
}

func (r *LinkRepository) ListByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Link, error) {
	query := fmt.Sprintf(`SELECT %s FROM links WHERE owner_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC`, linkColumns)
	rows, err := r.pool.Query(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	defer rows.Close()

	links := make([]model.Link, 0)
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		links = append(links, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate links: %w", err)
	}
	return links, nil
}

func (r *LinkRepository) Update(ctx context.Context, id, ownerID uuid.UUID, originalURL string, expiresAt time.Time) (*model.Link, error) {
	query := fmt.Sprintf(`UPDATE links SET original_url = $3, expires_at = $4, updated_at = now() WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL RETURNING %s`, linkColumns)
	l, err := scanLink(r.pool.QueryRow(ctx, query, id, ownerID, originalURL, expiresAt))
	if err != nil {
		return nil, translateError(err)
	}
	return &l, nil
}

func (r *LinkRepository) SoftDelete(ctx context.Context, id, ownerID uuid.UUID) error {
	query := `UPDATE links SET deleted_at = now(), updated_at = now() WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, query, id, ownerID)
	if err != nil {
		return fmt.Errorf("soft delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ApplyClickDeltas applies worker-flushed click deltas. Rows already soft
// deleted are skipped by the WHERE clause, so a lingering counter for a dead
// link is a harmless no-op.
func (r *LinkRepository) ApplyClickDeltas(ctx context.Context, deltas map[uuid.UUID]int64) error {
	for id, delta := range deltas {
		query := `UPDATE links SET click_count = click_count + $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`
		if _, err := r.pool.Exec(ctx, query, id, delta); err != nil {
			return fmt.Errorf("apply click delta: %w", err)
		}
	}
	return nil
}
