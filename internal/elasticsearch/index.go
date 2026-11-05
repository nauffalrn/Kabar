package elasticsearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"article-service/internal/article"
)

type IndexConfig struct {
	Alias    string // what the app reads and writes, e.g. "articles"
	Name     string // physical index behind the alias, e.g. "articles_v1"
	Shards   int
	Replicas int
}

// document is the stored shape; author_name is denormalised to avoid a Postgres lookup.
type document struct {
	ID         string    `json:"id"`
	AuthorID   string    `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func toDocument(a article.Article) document {
	return document{
		ID: a.ID, AuthorID: a.AuthorID, AuthorName: a.AuthorName,
		Title: a.Title, Body: a.Body, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

func (d document) toArticle() article.Article {
	return article.Article{
		ID: d.ID, AuthorID: d.AuthorID, AuthorName: d.AuthorName,
		Title: d.Title, Body: d.Body, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

// textField indexes text three ways (plain, Indonesian, English) because the
// language is unknown and may be mixed; a match on any of them counts.
func textField() map[string]any {
	return map[string]any{
		"type":     "text",
		"analyzer": "folded",
		"fields": map[string]any{
			"id": map[string]any{"type": "text", "analyzer": "indonesian"},
			"en": map[string]any{"type": "text", "analyzer": "english"},
		},
	}
}

func indexDefinition(cfg IndexConfig) map[string]any {
	return map[string]any{
		"aliases": map[string]any{cfg.Alias: map[string]any{"is_write_index": true}},
		"settings": map[string]any{
			"number_of_shards":   cfg.Shards,
			"number_of_replicas": cfg.Replicas,
			// Newest-first index sort enables early termination.
			"index.sort.field": []string{"created_at", "id"},
			"index.sort.order": []string{"desc", "desc"},
			"analysis": map[string]any{
				"analyzer": map[string]any{
					"folded": map[string]any{
						"type":      "custom",
						"tokenizer": "standard",
						"filter":    []string{"lowercase", "asciifolding"},
					},
				},
			},
		},
		"mappings": map[string]any{
			"dynamic": "strict", // reject unexpected fields instead of guessing types
			"properties": map[string]any{
				"id":          map[string]any{"type": "keyword"},
				"author_id":   map[string]any{"type": "keyword"},
				"author_name": map[string]any{"type": "keyword", "index": false},
				"title":       textField(),
				"body":        textField(),
				"created_at":  map[string]any{"type": "date"},
				"updated_at":  map[string]any{"type": "date", "index": false},
			},
		},
	}
}

// EnsureIndex creates the index and its alias if missing. The app only uses
// the alias, so a future reindex can swap it with no downtime.
func EnsureIndex(ctx context.Context, c *Client, cfg IndexConfig) error {
	err := c.doJSON(ctx, http.MethodHead, "/_alias/"+cfg.Alias, nil, nil)
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		return fmt.Errorf("check alias: %w", err)
	}

	err = c.doJSON(ctx, http.MethodPut, "/"+cfg.Name, indexDefinition(cfg), nil)
	if errors.As(err, &apiErr) && apiErr.hasType("resource_already_exists_exception") {
		return nil // another process won the race
	}
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	return nil
}
