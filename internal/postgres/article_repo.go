package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"article-service/internal/article"
)

// ArticleRepo is the Postgres source of truth for articles.
type ArticleRepo struct{ pool *pgxpool.Pool }

func NewArticleRepo(pool *pgxpool.Pool) *ArticleRepo { return &ArticleRepo{pool: pool} }

// Queries alias articles as "a" and authors as "au".
const articleColumns = "a.id, a.author_id, au.name, COALESCE(a.category_id, ''), a.title, a.body, " +
	"a.created_at, a.updated_at, a.version, a.deleted_at IS NOT NULL"

func scanArticle(row pgx.CollectableRow) (article.Article, error) {
	var a article.Article
	err := row.Scan(&a.ID, &a.AuthorID, &a.AuthorName, &a.CategoryID, &a.Title, &a.Body,
		&a.CreatedAt, &a.UpdatedAt, &a.Version, &a.Deleted)
	return a, err
}

func (r *ArticleRepo) CreateArticle(ctx context.Context, in article.Article, images []article.Image) (article.Article, error) {
	err := withTx(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			"INSERT INTO articles (id, author_id, category_id, title, body) VALUES ($1, $2, NULLIF($3, ''), $4, $5)",
			in.ID, in.AuthorID, in.CategoryID, in.Title, in.Body); err != nil {
			return err
		}
		for _, img := range images {
			if err := insertImage(ctx, tx, img); err != nil {
				return err
			}
		}
		return enqueueIndex(ctx, tx, in.ID)
	})
	if isForeignKeyViolation(err) {
		return article.Article{}, article.ErrAuthorNotFound
	}
	if err != nil {
		return article.Article{}, err
	}
	return r.GetArticle(ctx, in.ID, "")
}

func (r *ArticleRepo) UpdateArticle(ctx context.Context, id string, ch article.ArticleChanges) (article.Article, error) {
	// version is bumped by the database, so it is safe to use as the ES external version.
	err := withTx(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE articles
			SET title       = COALESCE($2::text, title),
			    body        = COALESCE($3::text, body),
			    category_id = COALESCE($4::text, category_id),
			    updated_at  = now(),
			    version     = version + 1
			WHERE id = $1 AND deleted_at IS NULL`, id, ch.Title, ch.Body, ch.CategoryID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return article.ErrNotFound
		}
		return enqueueIndex(ctx, tx, id)
	})
	if err != nil {
		return article.Article{}, err
	}
	return r.GetArticle(ctx, id, "")
}

// DeleteArticle is a soft delete; the indexer removes the search document.
func (r *ArticleRepo) DeleteArticle(ctx context.Context, id string) error {
	return withTx(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE articles SET deleted_at = now(), updated_at = now(), version = version + 1
			WHERE id = $1 AND deleted_at IS NULL`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return article.ErrNotFound
		}
		return enqueueIndex(ctx, tx, id)
	})
}

func (r *ArticleRepo) GetArticle(ctx context.Context, id, viewerUserID string) (article.Article, error) {
	var a article.Article
	var catSlug, catName string
	err := r.pool.QueryRow(ctx, `
		SELECT `+articleColumns+`, COALESCE(c.slug, ''), COALESCE(c.name, ''),
		       COALESCE(s.like_count, 0), COALESCE(s.comment_count, 0),
		       $2 <> '' AND EXISTS (SELECT 1 FROM article_likes l WHERE l.article_id = a.id AND l.user_id = $2)
		FROM articles a
		JOIN authors au ON au.id = a.author_id
		LEFT JOIN categories c ON c.id = a.category_id
		LEFT JOIN article_stats s ON s.article_id = a.id
		WHERE a.id = $1 AND a.deleted_at IS NULL`, id, viewerUserID,
	).Scan(&a.ID, &a.AuthorID, &a.AuthorName, &a.CategoryID, &a.Title, &a.Body,
		&a.CreatedAt, &a.UpdatedAt, &a.Version, &a.Deleted,
		&catSlug, &catName, &a.LikeCount, &a.CommentCount, &a.LikedByMe)
	if errors.Is(err, pgx.ErrNoRows) {
		return article.Article{}, article.ErrNotFound
	}
	if err != nil {
		return article.Article{}, fmt.Errorf("get article: %w", err)
	}
	if a.CategoryID != "" {
		a.Category = &article.Category{ID: a.CategoryID, Slug: catSlug, Name: catName}
	}

	rows, err := r.pool.Query(ctx, "SELECT "+imageColumns+" FROM article_images WHERE article_id = $1 ORDER BY position", id)
	if err != nil {
		return article.Article{}, fmt.Errorf("get images: %w", err)
	}
	a.Images, err = pgx.CollectRows(rows, scanImage)
	if err != nil {
		return article.Article{}, err
	}
	a.ImageCount = len(a.Images)
	for i := range a.Images {
		if a.Images[i].IsCover {
			a.Cover = &a.Images[i]
		}
	}
	return a, nil
}

// Enrich fills what feed cards need beyond the search document, in one query.
// Counters and covers change too often to be copied into Elasticsearch.
func (r *ArticleRepo) Enrich(ctx context.Context, list []article.Article) ([]article.Article, error) {
	ids := make([]string, len(list))
	for i, a := range list {
		ids[i] = a.ID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT a.id, a.deleted_at IS NOT NULL, COALESCE(c.id, ''), COALESCE(c.slug, ''), COALESCE(c.name, ''),
		       COALESCE(s.like_count, 0), COALESCE(s.comment_count, 0),
		       (SELECT count(*) FROM article_images i WHERE i.article_id = a.id),
		       COALESCE(cv.id, ''), COALESCE(cv.object_key, ''), COALESCE(cv.caption, '')
		FROM articles a
		LEFT JOIN categories c ON c.id = a.category_id
		LEFT JOIN article_stats s ON s.article_id = a.id
		LEFT JOIN article_images cv ON cv.article_id = a.id AND cv.is_cover
		WHERE a.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("enrich: %w", err)
	}
	defer rows.Close()

	type extra struct {
		deleted                bool
		cat                    article.Category
		likes, comments        int64
		images                 int
		coverID, coverKey, cap string
	}
	byID := make(map[string]extra, len(ids))
	for rows.Next() {
		var id string
		var e extra
		if err := rows.Scan(&id, &e.deleted, &e.cat.ID, &e.cat.Slug, &e.cat.Name,
			&e.likes, &e.comments, &e.images, &e.coverID, &e.coverKey, &e.cap); err != nil {
			return nil, err
		}
		byID[id] = e
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]article.Article, len(list))
	for i, a := range list {
		e, ok := byID[a.ID]
		a.Deleted = !ok || e.deleted
		if e.cat.ID != "" {
			cat := e.cat
			a.Category, a.CategoryID = &cat, cat.ID
		}
		a.LikeCount, a.CommentCount, a.ImageCount = e.likes, e.comments, e.images
		if e.coverID != "" {
			a.Cover = &article.Image{ID: e.coverID, ArticleID: a.ID, IsCover: true, Key: e.coverKey, Caption: e.cap}
		}
		out[i] = a
	}
	return out, nil
}

// ListIllustrated starts from the partial cover index, so its cost depends on
// limit, not on how many text-only articles were published since.
func (r *ArticleRepo) ListIllustrated(ctx context.Context, limit int) ([]article.Article, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+articleColumns+`
		FROM article_images i
		JOIN articles a ON a.id = i.article_id
		JOIN authors au ON au.id = a.author_id
		WHERE i.is_cover AND a.deleted_at IS NULL
		ORDER BY i.created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list illustrated: %w", err)
	}
	list, err := pgx.CollectRows(rows, scanArticle)
	if err != nil {
		return nil, err
	}
	// A cover chosen later than its article was written must not jump the queue.
	slices.SortFunc(list, func(a, b article.Article) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return list, nil
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
// Optional predicates are only added when present so the planner can use them.
func buildListQuery(p article.ListParams) (string, []any) {
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if len(p.AuthorIDs) == 0 {
		where := []string{"a.deleted_at IS NULL"}
		if p.CategoryID != "" {
			where = append(where, "a.category_id = "+arg(p.CategoryID))
		}
		if p.After != nil {
			where = append(where, "(a.created_at, a.id) < ("+arg(p.After.CreatedAt)+", "+arg(p.After.ID)+")")
		}
		var b strings.Builder
		b.WriteString("SELECT " + articleColumns + " FROM articles a JOIN authors au ON au.id = a.author_id")
		b.WriteString(" WHERE " + strings.Join(where, " AND "))
		b.WriteString(" ORDER BY a.created_at DESC, a.id DESC LIMIT " + arg(p.Limit))
		return b.String(), args
	}

	ids := arg(p.AuthorIDs)
	inner := " AND x.deleted_at IS NULL"
	if p.CategoryID != "" {
		inner += " AND x.category_id = " + arg(p.CategoryID)
	}
	if p.After != nil {
		inner += " AND (x.created_at, x.id) < (" + arg(p.After.CreatedAt) + ", " + arg(p.After.ID) + ")"
	}
	limit := arg(p.Limit)

	sql := `SELECT ` + articleColumns + `
		FROM authors au
		CROSS JOIN LATERAL (
			SELECT x.* FROM articles x
			WHERE x.author_id = au.id` + inner + `
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
