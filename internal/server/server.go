package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/P-kaizoku/small-light/internal/config"
	"github.com/P-kaizoku/small-light/internal/handler"
	"github.com/P-kaizoku/small-light/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func New(cfg config.Config, logger *slog.Logger, pool *pgxpool.Pool, rdb *redis.Client, h *handler.Handler) *http.Server {

	r := chi.NewRouter()

	// Global middleware chain. Order matters — each r.Use wraps everything
	// mounted after it (first call = outermost), so:
	//  1. RequestID:      generates/echoes the request id so every layer below
	//                     (loggers, recover, handlers) can tag its log lines.
	//  2. RequestLogger:  wraps the writer in its statusRecorder so it can log
	//                     the final status/bytes after the handler returns.
	//  3. Recover:        innermost, so the writer it sees IS the
	//                     statusRecorder — that is how it knows whether headers
	//                     were already sent before a panic. It replaces chi's
	//                     Recoverer, which logs to the default logger and
	//                     writes plain text; ours logs through slog and returns
	//                     JSON like the rest of the API.
	r.Use(middleware.RequestID)
	r.Use(middleware.RequestLogger(logger))
	r.Use(middleware.Recover(logger))

	health := handler.NewHealth(pool, rdb, logger)

	r.Get("/health", health.Check)

	r.Route("/api/v1", func(r chi.Router) {
		// Public endpoints (register/login) plus the whole API subtree:
		// 100 requests/min per IP. Brute-forcing login is the main threat, so
		// the whole subtree is protected rather than just the auth routes.
		r.Use(middleware.RateLimit(rdb, 100, time.Minute))
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

	// The redirect hotspot gets a tighter budget: 20/min per IP. Resolve is
	// the cheapest route to abuse (an attacker can hammer it with arbitrary
	// codes to burn CPU/Redis), so it deserves a stricter cap than the API.
	r.With(middleware.RateLimit(rdb, 20, time.Minute)).Get("/{code}", h.Resolve)

	return &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
