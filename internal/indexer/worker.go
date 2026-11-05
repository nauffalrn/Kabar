// Package indexer syncs Elasticsearch with Postgres by draining the outbox.
package indexer

import (
	"context"
	"log/slog"
	"time"

	"article-service/internal/article"
)

// Source yields batches of changed articles (Postgres outbox).
type Source interface {
	ProcessBatch(ctx context.Context, limit int, handle func(context.Context, []article.Article) error) (int, error)
}

// Sink receives changed articles (Elasticsearch).
type Sink interface {
	Upsert(ctx context.Context, articles []article.Article) error
}

type Config struct {
	BatchSize    int
	IdleInterval time.Duration // wait when the outbox is drained
	MaxBackoff   time.Duration // cap for the wait after consecutive errors
}

type Worker struct {
	src  Source
	sink Sink
	cfg  Config
	log  *slog.Logger
}

func NewWorker(src Source, sink Sink, cfg Config, log *slog.Logger) *Worker {
	return &Worker{src: src, sink: sink, cfg: cfg, log: log}
}

// Run loops until ctx is cancelled: full batches run back to back, an empty
// outbox idles, and errors back off exponentially.
func (w *Worker) Run(ctx context.Context) error {
	backoff := w.cfg.IdleInterval
	for {
		n, err := w.RunOnce(ctx)

		var wait time.Duration
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			w.log.Error("index batch failed", "err", err, "retry_in", backoff)
			wait = backoff
			backoff = min(backoff*2, w.cfg.MaxBackoff)
		case n < w.cfg.BatchSize:
			wait = w.cfg.IdleInterval
			backoff = w.cfg.IdleInterval
		default:
			backoff = w.cfg.IdleInterval
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	return w.src.ProcessBatch(ctx, w.cfg.BatchSize, w.sink.Upsert)
}
