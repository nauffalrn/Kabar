package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"article-service/internal/article"
)

type OutboxRepo struct{ pool *pgxpool.Pool }

func NewOutboxRepo(pool *pgxpool.Pool) *OutboxRepo { return &OutboxRepo{pool: pool} }

type outboxEvent struct {
	ID        int64
	ArticleID string
}

// ProcessBatch claims events with FOR UPDATE SKIP LOCKED (safe for parallel
// indexers), loads the latest article state, and deletes the events only if
// handle succeeds.
func (r *OutboxRepo) ProcessBatch(ctx context.Context, limit int, handle func(context.Context, []article.Article) error) (int, error) {
	var claimed int
	err := withTx(ctx, r.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, article_id FROM article_outbox
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED`, limit)
		if err != nil {
			return fmt.Errorf("claim outbox: %w", err)
		}
		events, err := pgx.CollectRows(rows, pgx.RowToStructByPos[outboxEvent])
		if err != nil {
			return fmt.Errorf("read outbox: %w", err)
		}
		claimed = len(events)
		if claimed == 0 {
			return nil
		}

		eventIDs, articleIDs := splitEvents(events)
		articles, err := loadArticles(ctx, tx, articleIDs)
		if err != nil {
			return err
		}
		if err := handle(ctx, articles); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "DELETE FROM article_outbox WHERE id = ANY($1)", eventIDs)
		return err
	})
	return claimed, err
}

func splitEvents(events []outboxEvent) (eventIDs []int64, articleIDs []string) {
	seen := make(map[string]struct{}, len(events))
	for _, e := range events {
		eventIDs = append(eventIDs, e.ID)
		if _, dup := seen[e.ArticleID]; !dup {
			seen[e.ArticleID] = struct{}{}
			articleIDs = append(articleIDs, e.ArticleID)
		}
	}
	return eventIDs, articleIDs
}

func loadArticles(ctx context.Context, tx pgx.Tx, ids []string) ([]article.Article, error) {
	rows, err := tx.Query(ctx,
		"SELECT "+articleColumns+" FROM articles a JOIN authors au ON au.id = a.author_id WHERE a.id = ANY($1)", ids)
	if err != nil {
		return nil, fmt.Errorf("load articles: %w", err)
	}
	return pgx.CollectRows(rows, scanArticle)
}
