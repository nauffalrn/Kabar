//go:build integration

package elasticsearch

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"article-service/internal/article"
)

// newTestIndex creates a throwaway index behind a unique alias.
func newTestIndex(t *testing.T) (*Client, string) {
	t.Helper()
	url := os.Getenv("TEST_ELASTICSEARCH_URL")
	if url == "" {
		t.Skip("TEST_ELASTICSEARCH_URL not set")
	}
	c := NewClient(url, &http.Client{Timeout: 10 * time.Second})
	suffix := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	cfg := IndexConfig{Alias: "test_articles_" + suffix, Name: "test_articles_" + suffix + "_v1", Shards: 1}

	ctx := context.Background()
	if err := EnsureIndex(ctx, c, cfg); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndex(ctx, c, cfg); err != nil {
		t.Fatalf("EnsureIndex must be idempotent: %v", err)
	}
	t.Cleanup(func() { _ = c.doJSON(context.Background(), http.MethodDelete, "/"+cfg.Name, nil, nil) })
	return c, cfg.Alias
}

func index(t *testing.T, c *Client, alias string, docs ...article.Article) {
	t.Helper()
	ctx := context.Background()
	if err := NewWriter(c, alias).Upsert(ctx, docs); err != nil {
		t.Fatal(err)
	}
	// Make the writes visible now; in production the default 1s refresh applies.
	if err := c.doJSON(ctx, http.MethodPost, "/"+alias+"/_refresh", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func ids(as []article.Article) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.ID
	}
	return out
}

func search(t *testing.T, s *Searcher, p article.ListParams) []string {
	t.Helper()
	if p.Limit == 0 {
		p.Limit = 10
	}
	got, err := s.List(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return ids(got)
}

func TestSearch_MixedLanguage(t *testing.T) {
	c, alias := newTestIndex(t)
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	index(t, c, alias,
		article.Article{ID: "id-doc", AuthorID: "a1", AuthorName: "Budi", Version: 1, CreatedAt: base,
			Title: "Warga membaca berita banjir", Body: "Banjir melanda Jakarta pagi ini."},
		article.Article{ID: "en-doc", AuthorID: "a2", AuthorName: "John", Version: 1, CreatedAt: base.Add(time.Hour),
			Title: "Residents reading flood reports", Body: "Flooding hit the city this morning."},
		article.Article{ID: "mix-doc", AuthorID: "a1", AuthorName: "Budi", Version: 1, CreatedAt: base.Add(2 * time.Hour),
			Title: "Startup fintech raih pendanaan", Body: "The startup raised a Series A round."},
	)
	s := NewSearcher(c, alias)

	tests := []struct {
		query string
		want  []string
	}{
		{"banjir", []string{"id-doc"}},            // exact Indonesian word
		{"baca", []string{"id-doc"}},              // Indonesian stem: membaca -> baca
		{"read", []string{"en-doc"}},              // English stem: reading -> read
		{"FLOODING", []string{"en-doc"}},          // case-insensitive
		{"pendanaan raised", []string{"mix-doc"}}, // both languages in one article
		{"startup", []string{"mix-doc"}},          // title or body
		{"banjir startup", nil},                   // every keyword must match
		{"jakarta", []string{"id-doc"}},           // body only
		{"flood", []string{"en-doc"}},             // English stem: flooding/flood
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := search(t, s, article.ListParams{Query: tt.query})
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("query %q: got %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestSearch_AuthorFilterOrderingAndPaging(t *testing.T) {
	c, alias := newTestIndex(t)
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	var docs []article.Article
	for i := range 7 {
		docs = append(docs, article.Article{
			ID: "doc-" + string(rune('a'+i)), AuthorID: []string{"a1", "a2"}[i%2], Version: 1,
			Title: "berita ekonomi", Body: "isi", CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	index(t, c, alias, docs...)
	s := NewSearcher(c, alias)

	if got := search(t, s, article.ListParams{Query: "ekonomi", AuthorIDs: []string{"a1"}}); strings.Join(got, ",") != "doc-g,doc-e,doc-c,doc-a" {
		t.Fatalf("author filter / newest-first wrong: %v", got)
	}

	// Page through with search_after and compare with one big page.
	var paged []string
	var after *article.Cursor
	for {
		rows, err := s.List(context.Background(), article.ListParams{Query: "ekonomi", Limit: 3, After: after})
		if err != nil {
			t.Fatal(err)
		}
		paged = append(paged, ids(rows)...)
		if len(rows) < 3 {
			break
		}
		cur := article.CursorOf(rows[len(rows)-1])
		after = &cur
	}
	if strings.Join(paged, ",") != "doc-g,doc-f,doc-e,doc-d,doc-c,doc-b,doc-a" {
		t.Fatalf("search_after paging wrong: %v", paged)
	}
}

func TestWriter_OlderVersionNeverOverwritesNewer(t *testing.T) {
	c, alias := newTestIndex(t)
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	index(t, c, alias, article.Article{ID: "x", Version: 2, Title: "versi baru", Body: "b", CreatedAt: at})
	index(t, c, alias, article.Article{ID: "x", Version: 1, Title: "versi lama", Body: "b", CreatedAt: at}) // late, stale event

	got, err := NewSearcher(c, alias).List(context.Background(), article.ListParams{Query: "versi", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "versi baru" {
		t.Fatalf("stale event overwrote newer document: %+v", got)
	}
}
