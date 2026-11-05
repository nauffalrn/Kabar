// Command indexer drains the Postgres outbox into Elasticsearch. A separate
// process, so slow search never slows API writes.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"article-service/internal/config"
	"article-service/internal/elasticsearch"
	"article-service/internal/indexer"
	"article-service/internal/postgres"
	"article-service/migrations"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("indexer stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Connect(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}

	es := elasticsearch.NewClient(cfg.ESURL, &http.Client{Timeout: 30 * time.Second})
	if err := elasticsearch.EnsureIndex(ctx, es, cfg.ESIndex); err != nil {
		return err
	}

	w := indexer.NewWorker(
		postgres.NewOutboxRepo(pool),
		elasticsearch.NewWriter(es, cfg.ESIndex.Alias),
		cfg.Indexer,
		log,
	)
	log.Info("indexer started", "batch_size", cfg.Indexer.BatchSize)
	return w.Run(ctx)
}
