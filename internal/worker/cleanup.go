package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/P-kaizoku/small-light/internal/repository"
)

// Cleanup periodically soft-deletes links that have passed their expiry. It
// mirrors the Clicks worker's shape: run on an interval, then run once more on
// shutdown so no expired link survives a restart.
type Cleanup struct {
	links    *repository.LinkRepository
	logger   *slog.Logger
	interval time.Duration
}

func NewCleanup(links *repository.LinkRepository, logger *slog.Logger, interval time.Duration) *Cleanup {
	return &Cleanup{links: links, logger: logger, interval: interval}
}

// Run purges expired links every interval until ctx is cancelled, then performs
// a final purge before returning so shutdown leaves nothing expired behind.
func (w *Cleanup) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.purge(ctx)
		case <-ctx.Done():
			w.purgeFinal()
			return
		}
	}
}

func (w *Cleanup) purge(ctx context.Context) {
	n, err := w.links.SoftDeleteExpired(ctx, time.Now())
	if err != nil {
		w.logger.Warn("purge expired links", "error", err)
		return
	}
	if n > 0 {
		// Only log when something was purged; a quiet maintenance job is a
		// healthy one.
		w.logger.Info("expired links purged", "count", n)
	}
}

// purgeFinal runs with an independent timeout so the final purge can't hang
// shutdown forever.
func (w *Cleanup) purgeFinal() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w.purge(ctx)
}
