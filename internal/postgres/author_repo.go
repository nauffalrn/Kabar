package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"article-service/internal/article"
)

type AuthorRepo struct{ pool *pgxpool.Pool }

func NewAuthorRepo(pool *pgxpool.Pool) *AuthorRepo { return &AuthorRepo{pool: pool} }

func (r *AuthorRepo) CreateAuthor(ctx context.Context, a article.Author) error {
	_, err := r.pool.Exec(ctx, "INSERT INTO authors (id, name) VALUES ($1, $2)", a.ID, a.Name)
	return err
}

// FindAuthorIDsByName returns every id with this name (case-insensitive).
func (r *AuthorRepo) FindAuthorIDsByName(ctx context.Context, name string) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id FROM authors WHERE lower(name) = lower($1) ORDER BY id", name)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// GetProfile counts through articles_author_created_at_id_idx.
func (r *AuthorRepo) GetProfile(ctx context.Context, id string) (article.AuthorProfile, error) {
	var p article.AuthorProfile
	err := r.pool.QueryRow(ctx, `
		SELECT au.id, au.name, au.created_at,
		       (SELECT count(*) FROM articles a WHERE a.author_id = au.id AND a.deleted_at IS NULL)
		FROM authors au WHERE au.id = $1`, id,
	).Scan(&p.ID, &p.Name, &p.JoinedAt, &p.ArticleCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, article.ErrAuthorNotFound
	}
	return p, err
}
