package postgres

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"article-service/internal/article"
)

type ImageRepo struct{ pool *pgxpool.Pool }

func NewImageRepo(pool *pgxpool.Pool) *ImageRepo { return &ImageRepo{pool: pool} }

const imageColumns = "id, article_id, position, is_cover, object_key, caption"

func scanImage(row pgx.CollectableRow) (article.Image, error) {
	var i article.Image
	err := row.Scan(&i.ID, &i.ArticleID, &i.Position, &i.IsCover, &i.Key, &i.Caption)
	return i, err
}

func insertImage(ctx context.Context, tx pgx.Tx, img article.Image) error {
	_, err := tx.Exec(ctx,
		"INSERT INTO article_images (id, article_id, position, is_cover, object_key, caption) VALUES ($1, $2, $3, $4, $5, $6)",
		img.ID, img.ArticleID, img.Position, img.IsCover, img.Key, img.Caption)
	return err
}

// lockArticle serialises image changes of one article, so two concurrent
// uploads cannot both pass the limit check.
func lockArticle(ctx context.Context, tx pgx.Tx, articleID string) error {
	var id string
	err := tx.QueryRow(ctx, "SELECT id FROM articles WHERE id = $1 AND deleted_at IS NULL FOR UPDATE", articleID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return article.ErrNotFound
	}
	return err
}

func (r *ImageRepo) AddImage(ctx context.Context, img article.Image, max int) (article.Image, error) {
	err := withTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockArticle(ctx, tx, img.ArticleID); err != nil {
			return err
		}
		var count, next int
		if err := tx.QueryRow(ctx,
			"SELECT count(*), COALESCE(max(position) + 1, 0) FROM article_images WHERE article_id = $1",
			img.ArticleID).Scan(&count, &next); err != nil {
			return err
		}
		if count >= max {
			return article.ErrTooManyImages
		}
		img.Position, img.IsCover = next, count == 0
		return insertImage(ctx, tx, img)
	})
	return img, err
}

func (r *ImageRepo) GetImage(ctx context.Context, id string) (article.Image, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+imageColumns+" FROM article_images WHERE id = $1", id)
	if err != nil {
		return article.Image{}, err
	}
	img, err := pgx.CollectExactlyOneRow(rows, scanImage)
	if errors.Is(err, pgx.ErrNoRows) {
		return img, article.ErrImageNotFound
	}
	return img, err
}

func (r *ImageRepo) UpdateCaption(ctx context.Context, id, caption string) (article.Image, error) {
	rows, err := r.pool.Query(ctx,
		"UPDATE article_images SET caption = $2 WHERE id = $1 RETURNING "+imageColumns, id, caption)
	if err != nil {
		return article.Image{}, err
	}
	img, err := pgx.CollectExactlyOneRow(rows, scanImage)
	if errors.Is(err, pgx.ErrNoRows) {
		return img, article.ErrImageNotFound
	}
	return img, err
}

func (r *ImageRepo) DeleteImage(ctx context.Context, id string) (article.Image, error) {
	var img article.Image
	err := withTx(ctx, r.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "DELETE FROM article_images WHERE id = $1 RETURNING "+imageColumns, id)
		if err != nil {
			return err
		}
		if img, err = pgx.CollectExactlyOneRow(rows, scanImage); errors.Is(err, pgx.ErrNoRows) {
			return article.ErrImageNotFound
		} else if err != nil {
			return err
		}
		if !img.IsCover {
			return nil
		}
		_, err = tx.Exec(ctx, `
			UPDATE article_images SET is_cover = true
			WHERE id = (SELECT id FROM article_images WHERE article_id = $1 ORDER BY position LIMIT 1)`, img.ArticleID)
		return err
	})
	return img, err
}

// ReorderImages takes the complete new order; the deferred unique
// constraint lets positions swap within the transaction.
func (r *ImageRepo) ReorderImages(ctx context.Context, articleID string, imageIDs []string) error {
	return withTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockArticle(ctx, tx, articleID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT id FROM article_images WHERE article_id = $1", articleID)
		if err != nil {
			return err
		}
		current, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if !sameSet(current, imageIDs) {
			return article.NewValidationError("image_ids", "must list every image of the article exactly once")
		}
		for pos, id := range imageIDs {
			if _, err := tx.Exec(ctx, "UPDATE article_images SET position = $2 WHERE id = $1", id, pos); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetCover clears the old cover first: the partial unique index allows only one.
func (r *ImageRepo) SetCover(ctx context.Context, articleID, imageID string) error {
	return withTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockArticle(ctx, tx, articleID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE article_images SET is_cover = false WHERE article_id = $1 AND is_cover", articleID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE article_images SET is_cover = true WHERE id = $1 AND article_id = $2", imageID, articleID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return article.ErrImageNotFound
		}
		return nil
	})
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b) && len(slices.Compact(b)) == len(a)
}
