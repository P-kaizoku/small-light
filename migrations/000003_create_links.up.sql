CREATE TABLE links (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    original_url TEXT NOT NULL,
    short_code   VARCHAR(16) NOT NULL UNIQUE,
    click_count  INTEGER NOT NULL DEFAULT 0,
    expires_at   TIMESTAMPTZ NOT NULL DEFAULT now() + interval '7 days',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);

CREATE INDEX idx_links_owner_id ON links(owner_id);
CREATE INDEX idx_links_short_code ON links(short_code);
