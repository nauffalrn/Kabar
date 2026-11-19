package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"article-service/internal/article"
)

type EngagementRepo struct{ pool *pgxpool.Pool }

func NewEngagementRepo(pool *pgxpool.Pool) *EngagementRepo { return &EngagementRepo{pool: pool} }

// bumpStats changes a counter atomically, so concurrent likes never lose an update.
func bumpStats(ctx context.Context, tx pgx.Tx, articleID, column string, delta int) error {
	_, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO article_stats (article_id, %[1]s) VALUES ($1, GREATEST($2, 0))
		ON CONFLICT (article_id) DO UPDATE SET %[1]s = GREATEST(article_stats.%[1]s + $2, 0)`, column),
		articleID, delta)
	return err
}

func requireLiveArticle(ctx context.Context, tx pgx.Tx, articleID string) error {
	var ok bool
	if err := tx.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM articles WHERE id = $1 AND deleted_at IS NULL)", articleID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return article.ErrNotFound
	}
	return nil
}

// Like is idempotent: the primary key turns a repeated like into a no-op.
func (r *EngagementRepo) Like(ctx context.Context, articleID, userID string) error {
	return withTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := requireLiveArticle(ctx, tx, articleID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			"INSERT INTO article_likes (article_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", articleID, userID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return bumpStats(ctx, tx, articleID, "like_count", 1)
	})
}

func (r *EngagementRepo) Unlike(ctx context.Context, articleID, userID string) error {
	return withTx(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "DELETE FROM article_likes WHERE article_id = $1 AND user_id = $2", articleID, userID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return bumpStats(ctx, tx, articleID, "like_count", -1)
	})
}

const commentColumns = "c.id, c.article_id, a.title, c.user_id, u.author_id, au.name, c.body, " +
	"c.created_at, c.updated_at, c.deleted_at IS NOT NULL"

const commentJoins = ` FROM comments c
	JOIN articles a ON a.id = c.article_id
	JOIN users u ON u.id = c.user_id
	JOIN authors au ON au.id = u.author_id`

func scanComment(row pgx.CollectableRow) (article.Comment, error) {
	var c article.Comment
	err := row.Scan(&c.ID, &c.ArticleID, &c.ArticleTitle, &c.UserID, &c.AuthorID, &c.AuthorName, &c.Body,
		&c.CreatedAt, &c.UpdatedAt, &c.Deleted)
	return c, err
}

func (r *EngagementRepo) ListComments(ctx context.Context, articleID string, after *article.Cursor, limit int) ([]article.Comment, error) {
	sql := "SELECT " + commentColumns + commentJoins + " WHERE c.article_id = $1 AND c.deleted_at IS NULL"
	args := []any{articleID, limit}
	if after != nil {
		sql += " AND (c.created_at, c.id) < ($3, $4)"
		args = append(args, after.CreatedAt, after.ID)
	}
	rows, err := r.pool.Query(ctx, sql+" ORDER BY c.created_at DESC, c.id DESC LIMIT $2", args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanComment)
}

func (r *EngagementRepo) ListRecentComments(ctx context.Context, after *article.Cursor, limit int) ([]article.Comment, error) {
	sql := "SELECT " + commentColumns + commentJoins
	args := []any{limit}
	if after != nil {
		sql += " WHERE (c.created_at, c.id) < ($2, $3)"
		args = append(args, after.CreatedAt, after.ID)
	}
	rows, err := r.pool.Query(ctx, sql+" ORDER BY c.created_at DESC, c.id DESC LIMIT $1", args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanComment)
}

func (r *EngagementRepo) CreateComment(ctx context.Context, c article.Comment) (article.Comment, error) {
	err := withTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := requireLiveArticle(ctx, tx, c.ArticleID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO comments (id, article_id, user_id, body) VALUES ($1, $2, $3, $4)",
			c.ID, c.ArticleID, c.UserID, c.Body); err != nil {
			return err
		}
		return bumpStats(ctx, tx, c.ArticleID, "comment_count", 1)
	})
	if err != nil {
		return article.Comment{}, err
	}
	return r.GetComment(ctx, c.ID)
}

func (r *EngagementRepo) GetComment(ctx context.Context, id string) (article.Comment, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+commentColumns+commentJoins+" WHERE c.id = $1", id)
	if err != nil {
		return article.Comment{}, err
	}
	c, err := pgx.CollectExactlyOneRow(rows, scanComment)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, article.ErrCommentNotFound
	}
	return c, err
}

func (r *EngagementRepo) UpdateComment(ctx context.Context, id, body string) (article.Comment, error) {
	tag, err := r.pool.Exec(ctx,
		"UPDATE comments SET body = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL", id, body)
	if err != nil {
		return article.Comment{}, err
	}
	if tag.RowsAffected() == 0 {
		return article.Comment{}, article.ErrCommentNotFound
	}
	return r.GetComment(ctx, id)
}

// SetCommentDeleted soft-deletes or restores; the counter only moves when the state changes.
func (r *EngagementRepo) SetCommentDeleted(ctx context.Context, id string, deleted bool) (article.Comment, error) {
	err := withTx(ctx, r.pool, func(tx pgx.Tx) error {
		var articleID string
		err := tx.QueryRow(ctx, `
			UPDATE comments SET deleted_at = CASE WHEN $2 THEN now() END
			WHERE id = $1 AND (deleted_at IS NULL) = $2
			RETURNING article_id`, id, deleted).Scan(&articleID)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := r.GetComment(ctx, id); err != nil {
				return err
			}
			return nil // already in the requested state
		}
		if err != nil {
			return err
		}
		delta := 1
		if deleted {
			delta = -1
		}
		return bumpStats(ctx, tx, articleID, "comment_count", delta)
	})
	if err != nil {
		return article.Comment{}, err
	}
	return r.GetComment(ctx, id)
}
