package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/P-kaizoku/small-light/internal/config"
	"github.com/P-kaizoku/small-light/internal/handler"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func New(cfg config.Config, logger *slog.Logger, pool *pgxpool.Pool, rdb *redis.Client) *http.Server {

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	health := handler.NewHealth(pool, rdb, logger)

	r.Get("/health", health.Check)

	return &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
