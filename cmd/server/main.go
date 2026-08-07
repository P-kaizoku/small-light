package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/P-kaizoku/small-light/internal/config"
	"github.com/P-kaizoku/small-light/internal/database"
	"github.com/P-kaizoku/small-light/internal/logger"
	"github.com/P-kaizoku/small-light/internal/server"
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
	//
	srv := server.New(cfg, log, pool, rdb)

	// make a context to chk for interuption nd syscall
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// clean up func
	defer stop()

	// error channel to pass error
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
		log.Info("server stopped cleanly")

	}

}
