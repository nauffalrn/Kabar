// Command api serves the REST API.
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

	"github.com/google/uuid"

	"article-service/internal/account"
	"article-service/internal/article"
	"article-service/internal/cache"
	"article-service/internal/config"
	"article-service/internal/elasticsearch"
	"article-service/internal/httpapi"
	"article-service/internal/media"
	"article-service/internal/postgres"
	"article-service/internal/storage"
	"article-service/migrations"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

// run is the composition root: the only place that knows concrete types.
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

	es := elasticsearch.NewClient(cfg.ESURL, &http.Client{Timeout: 5 * time.Second})
	if err := elasticsearch.EnsureIndex(ctx, es, cfg.ESIndex); err != nil {
		return err
	}

	objects, err := storage.Connect(ctx, cfg.Storage)
	if err != nil {
		return err
	}

	articles := postgres.NewArticleRepo(pool)
	engagement := postgres.NewEngagementRepo(pool)
	svc := article.NewService(article.Deps{
		Writer:     articles,
		Store:      articles,
		Authors:    postgres.NewAuthorRepo(pool),
		Categories: postgres.NewCategoryRepo(pool),
		Browser:    articles,
		Search:     elasticsearch.NewSearcher(es, cfg.ESIndex.Alias),
		Images:     postgres.NewImageRepo(pool),
		Likes:      engagement,
		Comments:   engagement,
		Processor:  media.Processor{},
		Objects:    objects,
		Cache:      cache.New[article.Page](cfg.Cache.Size, cfg.Cache.TTL, cfg.Cache.LoadTimeout),
		NewID:      newID,
	})

	accounts := account.NewService(postgres.NewAccountRepo(pool), newID)
	if cfg.Admin.Email != "" {
		if err := accounts.EnsureAdmin(ctx, cfg.Admin.Name, cfg.Admin.Email, cfg.Admin.Password); err != nil {
			return err
		}
	}

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewRouter(httpapi.Deps{
			Articles: svc, Accounts: accounts, Media: objects, Log: log, SecureCookies: cfg.SecureCookies,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second, // image uploads
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	// Graceful shutdown: finish in-flight requests before exiting.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// newID returns a UUIDv7: time-ordered, so inserts append to the B-tree.
func newID() string { return uuid.Must(uuid.NewV7()).String() }
