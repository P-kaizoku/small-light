package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/P-kaizoku/small-light/internal/model"
	"github.com/redis/go-redis/v9"
)

const linkKeyPrefix = "link:code:"

// Link is the Redis read-path cache for resolved links, keyed by short code.
type Link struct {
	rdb *redis.Client
}

func NewLink(rdb *redis.Client) *Link {
	return &Link{rdb: rdb}
}

// Get returns the cached link for code, or false on miss or Redis error.
// A Redis outage must never fail a request, so errors degrade to a miss.
func (c *Link) Get(ctx context.Context, code string) (*model.Link, bool) {
	b, err := c.rdb.Get(ctx, linkKeyPrefix+code).Bytes()
	if err != nil {
		return nil, false
	}
	var l model.Link
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, false
	}
	return &l, true
}

// Set caches link for code. The cache entry expires at the same moment the
// link itself does, so a cached copy can never outlive its TTL.
func (c *Link) Set(ctx context.Context, code string, link *model.Link) {
	ttl := time.Until(link.ExpiresAt)
	if ttl <= 0 {
		return
	}
	b, err := json.Marshal(link)
	if err != nil {
		return
	}
	c.rdb.Set(ctx, linkKeyPrefix+code, b, ttl)
}

// Delete removes the cached entry for code (called on update/soft-delete).
func (c *Link) Delete(ctx context.Context, code string) {
	c.rdb.Del(ctx, linkKeyPrefix+code)
}
