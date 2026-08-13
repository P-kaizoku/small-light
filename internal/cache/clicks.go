package cache

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const clicksKeyPrefix = "link:clicks:"

// ClickCounter buffers click increments in Redis; a background worker drains
// them to Postgres. This trades durability for throughput: a crash between
// Increment and Drain loses analytics, never data integrity — a lost click
// counter is far cheaper than a failed redirect.
type ClickCounter struct {
	rdb *redis.Client
}

func NewClickCounter(rdb *redis.Client) *ClickCounter {
	return &ClickCounter{rdb: rdb}
}

// Increment records one click for a link. Errors are swallowed on purpose:
// if Redis is down, the redirect must still succeed.
func (c *ClickCounter) Increment(ctx context.Context, id uuid.UUID) {
	c.rdb.Incr(ctx, clicksKeyPrefix+id.String())
}

// Drain atomically reads and clears every counter, returning id -> delta.
// GetDel is atomic, so a drained click can never be double-counted.
func (c *ClickCounter) Drain(ctx context.Context) (map[uuid.UUID]int64, error) {
	deltas := make(map[uuid.UUID]int64)
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, clicksKeyPrefix+"*", 100).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			n, err := c.rdb.GetDel(ctx, key).Int64()
			if err != nil || n <= 0 {
				continue
			}
			id, err := uuid.Parse(strings.TrimPrefix(key, clicksKeyPrefix))
			if err != nil {
				continue
			}
			deltas[id] += n
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return deltas, nil
}
