// Package worker runs background processes. The click worker drains Redis
// click counters into Postgres on a fixed interval.
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/P-kaizoku/small-light/internal/cache"
	"github.com/P-kaizoku/small-light/internal/repository"
)

// Clicks drains buffered click counters into the links table.
type Clicks struct {
	counter  *cache.ClickCounter
	links    *repository.LinkRepository
	logger   *slog.Logger
	interval time.Duration
}

func NewClicks(counter *cache.ClickCounter, links *repository.LinkRepository, logger *slog.Logger, interval time.Duration) *Clicks {
	return &Clicks{counter: counter, links: links, logger: logger, interval: interval}
}

// Run flushes every interval until ctx is cancelled, then performs a final
// flush so shutdown doesn't lose the clicks accumulated since the last tick.
func (w *Clicks) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.flush(ctx)
		case <-ctx.Done():
			w.flushFinal()
			return
		}
	}
}

func (w *Clicks) flush(ctx context.Context) {
	deltas, err := w.counter.Drain(ctx)
	if err != nil {
		w.logger.Warn("drain click counters", "error", err)
		return
	}
	if len(deltas) == 0 {
		return
	}
	if err := w.links.ApplyClickDeltas(ctx, deltas); err != nil {
		w.logger.Warn("apply click deltas", "error", err)
		return
	}
	w.logger.Info("click counters flushed", "links", len(deltas))
}

// flushFinal drains with an independent timeout so a final flush can't hang
// shutdown forever. Best-effort by design: clicks survive only if Postgres is
// reachable during the drain window.
func (w *Clicks) flushFinal() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w.flush(ctx)
}
