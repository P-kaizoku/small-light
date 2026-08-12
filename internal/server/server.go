package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/P-kaizoku/small-light/internal/config"
	"github.com/P-kaizoku/small-light/internal/handler"
	"github.com/P-kaizoku/small-light/internal/middleware"
	"github.com/go-chi/chi/v5"
	chimicro "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func New(cfg config.Config, logger *slog.Logger, pool *pgxpool.Pool, rdb *redis.Client, h *handler.Handler) *http.Server {

	r := chi.NewRouter()
	r.Use(chimicro.Recoverer)

	health := handler.NewHealth(pool, rdb, logger)

	r.Get("/health", health.Check)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", h.Register)
		r.Post("/auth/login", h.Login)

		r.Route("/links", func(r chi.Router) {
			r.Use(middleware.RequireAuth(h.AuthService()))

			r.Post("/", h.CreateLink)
			r.Get("/", h.ListLinks)
			r.Get("/{id}", h.GetLink)
			r.Patch("/{id}", h.UpdateLink)
			r.Delete("/{id}", h.DeleteLink)
			r.Get("/{id}/analytics", h.Analytics)
		})
	})

	r.Get("/{code}", h.Resolve)

	return &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
