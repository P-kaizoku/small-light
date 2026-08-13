package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/P-kaizoku/small-light/internal/cache"
	"github.com/P-kaizoku/small-light/internal/config"
	"github.com/P-kaizoku/small-light/internal/database"
	"github.com/P-kaizoku/small-light/internal/handler"
	"github.com/P-kaizoku/small-light/internal/logger"
	"github.com/P-kaizoku/small-light/internal/repository"
	"github.com/P-kaizoku/small-light/internal/server"
	"github.com/P-kaizoku/small-light/internal/service"
	"github.com/P-kaizoku/small-light/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	log := logger.New(cfg.AppEnv, cfg.LogLevel)
	if err := cfg.Validate(); err != nil {
		log.Error("invalid config", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	//connect postgre here
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("invalid db config", "error", err)
		os.Exit(1)
	}

	// closes the pool
	defer pool.Close()

	// apply migrations before serving
	migrateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := database.RunMigrations(migrateCtx, pool); err != nil {
		log.Error("migrations failed", "error", err)
		os.Exit(1)
	}

	//creating the redis client by passing the creds using cfg
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	//close the rdb - clean up func

	defer rdb.Close()

	// make the server
	userRepo := repository.NewUserRepository(pool)
	linkRepo := repository.NewLinkRepository(pool)

	linkCache := cache.NewLink(rdb)
	clickCounter := cache.NewClickCounter(rdb)

	authSvc := service.NewAuthService(userRepo, []byte(cfg.JWTSecret), cfg.JWTTTL)
	linkSvc := service.NewLinkService(linkRepo, cfg.DefaultLinkTTL, linkCache, clickCounter)

	h := handler.New(authSvc, linkSvc, cfg.DefaultLinkTTL, log)

	srv := server.New(cfg, log, pool, rdb, h)

	// make a context to chk for interuption nd syscall
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// clean up func
	defer stop()

	// click flush worker — drains Redis counters into Postgres every
	// cfg.ClickFlushInterval and once more on shutdown; the done channel lets
	// shutdown wait for that final flush so no clicks are lost.
	clickWorker := worker.NewClicks(clickCounter, linkRepo, log, cfg.ClickFlushInterval)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		clickWorker.Run(ctx)
	}()

	// cleanup worker — soft-deletes links past their expires_at every
	// cfg.LinkCleanupInterval and once more on shutdown; same done-channel
	// wait pattern so the final purge completes before we exit.
	cleanupWorker := worker.NewCleanup(linkRepo, log, cfg.LinkCleanupInterval)
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		cleanupWorker.Run(ctx)
	}()

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		log.Error("server error", "error", err)
		os.Exit(1)
	case <-ctx.Done():
		log.Info("shutdown signal recieved, draining...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("error in shutting down gracefully", "error", err)
			os.Exit(1)
		}
		<-workerDone
		<-cleanupDone
		log.Info("server stopped cleanly")

	}

}
