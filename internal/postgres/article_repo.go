package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"article-service/internal/article"
)

// ArticleRepo is the Postgres source of truth for articles.
type ArticleRepo struct{ pool *pgxpool.Pool }

func NewArticleRepo(pool *pgxpool.Pool) *ArticleRepo { return &ArticleRepo{pool: pool} }

// Queries alias articles as "a" and authors as "au".
const articleColumns = "a.id, a.author_id, au.name, a.title, a.body, a.created_at, a.updated_at, a.version"

func scanArticle(row pgx.CollectableRow) (article.Article, error) {
	var a article.Article
	err := row.Scan(&a.ID, &a.AuthorID, &a.AuthorName, &a.Title, &a.Body, &a.CreatedAt, &a.UpdatedAt, &a.Version)
	return a, err
}

func (r *ArticleRepo) CreateArticle(ctx context.Context, in article.Article) (article.Article, error) {
	const q = `
		WITH a AS (
			INSERT INTO articles (id, author_id, title, body)
			VALUES ($1, $2, $3, $4)
			RETURNING *
		)
		SELECT ` + articleColumns + `
		FROM a JOIN authors au ON au.id = a.author_id`

	var out article.Article
	err := withTx(ctx, r.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, q, in.ID, in.AuthorID, in.Title, in.Body)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectExactlyOneRow(rows, scanArticle); err != nil {
			return err
		}
		return enqueueIndex(ctx, tx, out.ID)
	})
	if isForeignKeyViolation(err) {
		return article.Article{}, article.ErrAuthorNotFound
	}
	return out, err
}

func (r *ArticleRepo) UpdateArticle(ctx context.Context, id string, in article.UpdateArticleInput) (article.Article, error) {
	// Bumped by the database, so it is safe to use as the ES external version.
	const q = `
		WITH a AS (
			UPDATE articles
			SET title      = COALESCE($2::text, title),
			    body       = COALESCE($3::text, body),
			    updated_at = now(),
			    version    = version + 1
			WHERE id = $1
			RETURNING *
		)
		SELECT ` + articleColumns + `
		FROM a JOIN authors au ON au.id = a.author_id`

	var out article.Article
	err := withTx(ctx, r.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, q, id, in.Title, in.Body)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectExactlyOneRow(rows, scanArticle); err != nil {
			return err
		}
		return enqueueIndex(ctx, tx, out.ID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return article.Article{}, article.ErrNotFound
	}
	return out, err
}

func (r *ArticleRepo) List(ctx context.Context, p article.ListParams) ([]article.Article, error) {
	sql, args := buildListQuery(p)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query articles: %w", err)
	}
	return pgx.CollectRows(rows, scanArticle)
}

// buildListQuery returns a keyset-paginated query. With an author filter it
// takes the top LIMIT rows per author id via LATERAL, so the cost stays
// bounded even for the syndication account (README section 7).
// The cursor predicate is only added when present so the planner can use it.
func buildListQuery(p article.ListParams) (string, []any) {
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if len(p.AuthorIDs) == 0 {
		var b strings.Builder
		b.WriteString("SELECT " + articleColumns + " FROM articles a JOIN authors au ON au.id = a.author_id")
		if p.After != nil {
			b.WriteString(" WHERE (a.created_at, a.id) < (" + arg(p.After.CreatedAt) + ", " + arg(p.After.ID) + ")")
		}
		b.WriteString(" ORDER BY a.created_at DESC, a.id DESC LIMIT " + arg(p.Limit))
		return b.String(), args
	}

	ids := arg(p.AuthorIDs)
	cursor := ""
	if p.After != nil {
		cursor = " AND (x.created_at, x.id) < (" + arg(p.After.CreatedAt) + ", " + arg(p.After.ID) + ")"
	}
	limit := arg(p.Limit)

	sql := `SELECT ` + articleColumns + `
		FROM authors au
		CROSS JOIN LATERAL (
			SELECT x.* FROM articles x
			WHERE x.author_id = au.id` + cursor + `
			ORDER BY x.created_at DESC, x.id DESC
			LIMIT ` + limit + `
		) a
		WHERE au.id = ANY(` + ids + `)
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT ` + limit
	return sql, args
}

func enqueueIndex(ctx context.Context, tx pgx.Tx, articleID string) error {
	_, err := tx.Exec(ctx, "INSERT INTO article_outbox (article_id) VALUES ($1)", articleID)
	return err
}
