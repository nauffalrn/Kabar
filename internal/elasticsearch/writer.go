package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"article-service/internal/article"
)

// Writer implements indexer.Sink.
type Writer struct {
	client *Client
	alias  string
}

func NewWriter(c *Client, alias string) *Writer { return &Writer{client: c, alias: alias} }

type bulkResponse struct {
	Errors bool `json:"errors"`
	Items  []map[string]struct {
		ID     string          `json:"_id"`
		Status int             `json:"status"`
		Error  json.RawMessage `json:"error"`
	} `json:"items"`
}

// Upsert uses external versioning with the Postgres version, so a late or
// retried event never overwrites a newer edit; 409 means already up to date.
func (w *Writer) Upsert(ctx context.Context, articles []article.Article) error {
	if len(articles) == 0 {
		return nil
	}
	body, err := encodeBulk(w.alias, articles)
	if err != nil {
		return err
	}

	var resp bulkResponse
	if err := w.client.do(ctx, http.MethodPost, "/_bulk", "application/x-ndjson", body, &resp); err != nil {
		return fmt.Errorf("bulk index: %w", err)
	}
	return bulkFailures(resp)
}

func encodeBulk(alias string, articles []article.Article) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf) // Encode appends '\n', exactly what NDJSON needs
	for _, a := range articles {
		action := map[string]any{"index": map[string]any{
			"_index":       alias,
			"_id":          a.ID,
			"version":      a.Version,
			"version_type": "external",
		}}
		if err := enc.Encode(action); err != nil {
			return nil, err
		}
		if err := enc.Encode(toDocument(a)); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func bulkFailures(resp bulkResponse) error {
	if !resp.Errors {
		return nil
	}
	var failures []string
	for _, item := range resp.Items {
		for _, r := range item {
			if r.Status >= 300 && r.Status != http.StatusConflict {
				failures = append(failures, fmt.Sprintf("%s: %d %s", r.ID, r.Status, r.Error))
			}
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.New("bulk index failed for " + strings.Join(failures, "; "))
}
