package article

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- fakes -----------------------------------------------------------------

type fakeReader struct {
	rows   []Article
	err    error
	calls  atomic.Int32
	params ListParams
}

func (f *fakeReader) List(_ context.Context, p ListParams) ([]Article, error) {
	f.calls.Add(1)
	f.params = p
	if f.err != nil {
		return nil, f.err
	}
	return f.rows[:min(p.Limit, len(f.rows))], nil
}

type fakeAuthors struct {
	idsByName map[string][]string
	created   []Author
}

func (f *fakeAuthors) CreateAuthor(_ context.Context, a Author) error {
	f.created = append(f.created, a)
	return nil
}

func (f *fakeAuthors) FindAuthorIDsByName(_ context.Context, name string) ([]string, error) {
	return f.idsByName[name], nil
}

type fakeWriter struct {
	created Article
	err     error
}

func (f *fakeWriter) CreateArticle(_ context.Context, a Article) (Article, error) {
	f.created = a
	return a, f.err
}

func (f *fakeWriter) UpdateArticle(_ context.Context, id string, _ UpdateArticleInput) (Article, error) {
	return Article{ID: id}, f.err
}

func articles(n int) []Article {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]Article, n)
	for i := range out {
		out[i] = Article{ID: "a" + strconv.Itoa(i), CreatedAt: base.Add(-time.Duration(i) * time.Minute)}
	}
	return out
}

type fixture struct {
	svc     *Service
	browser *fakeReader
	search  *fakeReader
	authors *fakeAuthors
	writer  *fakeWriter
}

func newFixture(rows []Article) fixture {
	f := fixture{
		browser: &fakeReader{rows: rows},
		search:  &fakeReader{rows: rows},
		authors: &fakeAuthors{idsByName: map[string][]string{"Budi": {"id-1", "id-2"}}},
		writer:  &fakeWriter{},
	}
	f.svc = NewService(Deps{
		Writer: f.writer, Authors: f.authors, Browser: f.browser, Search: f.search,
		NewID: func() string { return "new-id" },
	})
	return f
}

// ---- ListArticles ------------------------------------------------------------

func TestListArticles_RoutesByKeyword(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantSearch bool
	}{
		{"no keyword uses postgres", "", false},
		{"blank keyword uses postgres", "   ", false},
		{"keyword uses elasticsearch", "banjir", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(articles(3))
			if _, err := f.svc.ListArticles(context.Background(), ListQuery{Query: tt.query}); err != nil {
				t.Fatal(err)
			}
			if got := f.search.calls.Load() == 1; got != tt.wantSearch {
				t.Fatalf("search called = %v, want %v", got, tt.wantSearch)
			}
			if got := f.browser.calls.Load() == 1; got == tt.wantSearch {
				t.Fatalf("browser called = %v, want %v", got, !tt.wantSearch)
			}
		})
	}
}

func TestListArticles_Pagination(t *testing.T) {
	f := newFixture(articles(5))

	page, err := f.svc.ListArticles(context.Background(), ListQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if f.browser.params.Limit != 3 {
		t.Fatalf("reader should be asked for limit+1 rows, got %d", f.browser.params.Limit)
	}
	if len(page.Articles) != 2 || page.NextCursor == nil || page.NextCursor.ID != "a1" {
		t.Fatalf("want 2 rows and cursor at a1, got %d rows, cursor %+v", len(page.Articles), page.NextCursor)
	}

	last, err := f.svc.ListArticles(context.Background(), ListQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if last.NextCursor != nil {
		t.Fatal("no next cursor expected when everything fits in one page")
	}
}

func TestListArticles_AuthorNameResolvesToAllIDs(t *testing.T) {
	f := newFixture(articles(1))

	if _, err := f.svc.ListArticles(context.Background(), ListQuery{AuthorName: "  Budi "}); err != nil {
		t.Fatal(err)
	}
	if got := f.browser.params.AuthorIDs; len(got) != 2 || got[0] != "id-1" || got[1] != "id-2" {
		t.Fatalf("want both ids for the shared name, got %v", got)
	}
}

func TestListArticles_UnknownAuthorShortCircuits(t *testing.T) {
	f := newFixture(articles(3))

	page, err := f.svc.ListArticles(context.Background(), ListQuery{AuthorName: "Nobody"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Articles) != 0 || page.Articles == nil {
		t.Fatalf("want empty non-nil page, got %#v", page.Articles)
	}
	if f.browser.calls.Load()+f.search.calls.Load() != 0 {
		t.Fatal("no reader should be called for an unknown author")
	}
}

func TestListArticles_ValidatesLimit(t *testing.T) {
	f := newFixture(nil)
	_, err := f.svc.ListArticles(context.Background(), ListQuery{Limit: MaxPageSize + 1})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want ValidationError, got %v", err)
	}
}

func TestListArticles_ReaderErrorIsWrapped(t *testing.T) {
	f := newFixture(nil)
	boom := errors.New("boom")
	f.browser.err = boom

	_, err := f.svc.ListArticles(context.Background(), ListQuery{})
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped reader error, got %v", err)
	}
}

// ---- writes -------------------------------------------------------------------

func TestCreateArticle(t *testing.T) {
	t.Run("normalizes and assigns id", func(t *testing.T) {
		f := newFixture(nil)
		a, err := f.svc.CreateArticle(context.Background(), CreateArticleInput{AuthorID: " au ", Title: " Judul ", Body: " Isi "})
		if err != nil {
			t.Fatal(err)
		}
		if a.ID != "new-id" || a.Title != "Judul" || a.Body != "Isi" || a.AuthorID != "au" {
			t.Fatalf("unexpected article %+v", a)
		}
	})

	t.Run("reports every invalid field", func(t *testing.T) {
		f := newFixture(nil)
		_, err := f.svc.CreateArticle(context.Background(), CreateArticleInput{})
		var verr *ValidationError
		if !errors.As(err, &verr) || len(verr.Fields) != 3 {
			t.Fatalf("want 3 field errors, got %v", err)
		}
	})

	t.Run("keeps writer sentinel errors", func(t *testing.T) {
		f := newFixture(nil)
		f.writer.err = ErrAuthorNotFound
		_, err := f.svc.CreateArticle(context.Background(), CreateArticleInput{AuthorID: "x", Title: "t", Body: "b"})
		if !errors.Is(err, ErrAuthorNotFound) {
			t.Fatalf("want ErrAuthorNotFound, got %v", err)
		}
	})
}

func TestUpdateArticle_RequiresAField(t *testing.T) {
	f := newFixture(nil)
	_, err := f.svc.UpdateArticle(context.Background(), "id", UpdateArticleInput{})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want ValidationError, got %v", err)
	}
}

func TestCreateAuthor_NormalizesWhitespace(t *testing.T) {
	f := newFixture(nil)
	a, err := f.svc.CreateAuthor(context.Background(), "  Budi   Santoso ")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Budi Santoso" {
		t.Fatalf("got %q", a.Name)
	}
}

// ---- cache wiring ---------------------------------------------------------------

type countingCache struct {
	mu   sync.Mutex
	keys []string
}

func (c *countingCache) GetOrLoad(ctx context.Context, key string, load func(context.Context) (Page, error)) (Page, error) {
	c.mu.Lock()
	c.keys = append(c.keys, key)
	c.mu.Unlock()
	return load(ctx)
}

func TestCacheKey_DistinguishesEveryParameter(t *testing.T) {
	c := Cursor{CreatedAt: time.Unix(1, 0).UTC(), ID: "x"}
	queries := []ListQuery{
		{Limit: 20},
		{Limit: 21},
		{Limit: 20, Query: "a"},
		{Limit: 20, AuthorName: "a"},
		{Limit: 20, After: &c},
		{Limit: 20, Query: `a"|a=`}, // separator injection must not collide
	}
	seen := map[string]bool{}
	for _, q := range queries {
		k := q.cacheKey()
		if seen[k] {
			t.Fatalf("cache key collision: %s", k)
		}
		seen[k] = true
	}
	if (ListQuery{Query: "Banjir"}).cacheKey() != (ListQuery{Query: "banjir"}).cacheKey() {
		t.Fatal("search is case-insensitive, so the key should be too")
	}
}

func TestListArticles_GoesThroughCache(t *testing.T) {
	cache := &countingCache{}
	f := newFixture(articles(1))
	f.svc.d.Cache = cache

	if _, err := f.svc.ListArticles(context.Background(), ListQuery{}); err != nil {
		t.Fatal(err)
	}
	if len(cache.keys) != 1 {
		t.Fatalf("want one cache lookup, got %d", len(cache.keys))
	}
}
