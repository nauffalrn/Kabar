package postgres

import (
	"context"

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
