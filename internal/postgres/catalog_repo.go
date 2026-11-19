package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"article-service/internal/article"
)

type CategoryRepo struct{ pool *pgxpool.Pool }

func NewCategoryRepo(pool *pgxpool.Pool) *CategoryRepo { return &CategoryRepo{pool: pool} }

func (r *CategoryRepo) ListCategories(ctx context.Context) ([]article.Category, error) {
	rows, err := r.pool.Query(ctx, "SELECT id, slug, name FROM categories ORDER BY position")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[article.Category])
}

func (r *CategoryRepo) FindCategoryBySlug(ctx context.Context, slug string) (article.Category, error) {
	var c article.Category
	err := r.pool.QueryRow(ctx, "SELECT id, slug, name FROM categories WHERE slug = $1", slug).Scan(&c.ID, &c.Slug, &c.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, article.ErrCategoryNotFound
	}
	return c, err
}
