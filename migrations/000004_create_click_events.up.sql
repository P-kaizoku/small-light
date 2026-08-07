CREATE TABLE click_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    link_id    UUID NOT NULL REFERENCES links(id) ON DELETE CASCADE,
    ip         TEXT,
    user_agent TEXT,
    referrer   TEXT,
    country    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_click_events_link_id ON click_events(link_id);
CREATE INDEX idx_click_events_created_at ON click_events(created_at);
