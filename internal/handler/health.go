package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Health struct {
	pool   *pgxpool.Pool
	rdb    *redis.Client
	logger *slog.Logger
	start  time.Time
}

func NewHealth(pool *pgxpool.Pool, rdb *redis.Client, logger *slog.Logger) *Health {
	return &Health{pool: pool, rdb: rdb, logger: logger, start: time.Now()}
}

func (h *Health) Check(w http.ResponseWriter, r *http.Request) {
	components := map[string]string{"postgres": "UP", "redis": "UP"}
	status := "healthy"
	code := http.StatusOK

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.pool.Ping(ctx); err != nil {
		h.logger.Warn("postgress health chk failed", "error", err)
		components["postgres"] = "DOWN"
		status = "degraded"
		code = http.StatusServiceUnavailable
	}

	if err := h.rdb.Ping(ctx).Err(); err != nil {
		h.logger.Warn("redis health chk failed", "error", err)
		components["redis"] = "DOWN"
		status = "degraded"
		code = http.StatusServiceUnavailable
	}

	WriteJSON(w, code, map[string]any{
		"status":     status,
		"components": components,
		"uptime_sec": time.Since(h.start).Seconds(),
	})
}
