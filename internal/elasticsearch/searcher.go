package elasticsearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"article-service/internal/article"
)

type Searcher struct {
	client *Client
	alias  string
}

func NewSearcher(c *Client, alias string) *Searcher { return &Searcher{client: c, alias: alias} }

type searchResponse struct {
	Hits struct {
		Hits []struct {
			Source document `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

func (s *Searcher) List(ctx context.Context, p article.ListParams) ([]article.Article, error) {
	var resp searchResponse
	if err := s.client.doJSON(ctx, http.MethodPost, "/"+s.alias+"/_search", buildSearchRequest(p), &resp); err != nil {
		if isTransient(err) {
			return nil, fmt.Errorf("search articles: %w: %w", article.ErrUnavailable, err)
		}
		return nil, fmt.Errorf("search articles: %w", err)
	}
	out := make([]article.Article, len(resp.Hits.Hits))
	for i, h := range resp.Hits.Hits {
		out[i] = h.Source.toArticle()
	}
	return out, nil
}

// isTransient reports whether the error is an outage worth retrying, not a bug.
func isTransient(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
		return false
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

// buildSearchRequest uses filter context only (results are sorted by date, so
// scoring is wasted) and search_after on (created_at, id) like the Postgres keyset.
func buildSearchRequest(p article.ListParams) map[string]any {
	filters := []any{keywordClause(p.Query)}
	if len(p.AuthorIDs) > 0 {
		filters = append(filters, map[string]any{"terms": map[string]any{"author_id": p.AuthorIDs}})
	}

	req := map[string]any{
		"size":             p.Limit,
		"track_total_hits": false,
		"query":            map[string]any{"bool": map[string]any{"filter": filters}},
		"sort": []any{
			map[string]any{"created_at": "desc"},
			map[string]any{"id": "desc"},
		},
	}
	if p.After != nil {
		// created_at is a millisecond "date" field, so its sort value is epoch millis.
		req["search_after"] = []any{p.After.CreatedAt.UnixMilli(), p.After.ID}
	}
	return req
}

func keywordClause(query string) map[string]any {
	analyses := [][]string{
		{"title", "body"},
		{"title.id", "body.id"},
		{"title.en", "body.en"},
	}
	should := make([]any, len(analyses))
	for i, fields := range analyses {
		should[i] = map[string]any{"multi_match": map[string]any{
			"query":    query,
			"fields":   fields,
			"type":     "cross_fields",
			"operator": "and",
		}}
	}
	return map[string]any{"bool": map[string]any{"should": should, "minimum_should_match": 1}}
}
