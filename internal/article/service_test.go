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

func (f *fakeAuthors) GetProfile(_ context.Context, id string) (AuthorProfile, error) {
	return AuthorProfile{Author: Author{ID: id}}, nil
}

type fakeWriter struct {
	created Article
	images  []Image
	changes ArticleChanges
	deleted string
	err     error
}

func (f *fakeWriter) CreateArticle(_ context.Context, a Article, imgs []Image) (Article, error) {
	f.created, f.images = a, imgs
	return a, f.err
}

func (f *fakeWriter) UpdateArticle(_ context.Context, id string, ch ArticleChanges) (Article, error) {
	f.changes = ch
	return Article{ID: id}, f.err
}

func (f *fakeWriter) DeleteArticle(_ context.Context, id string) error {
	f.deleted = id
	return f.err
}

// fakeStore owns one article, written by author "owner".
type fakeStore struct{ enrichCalls int }

func (f *fakeStore) GetArticle(_ context.Context, id, _ string) (Article, error) {
	if id != "art-1" {
		return Article{}, ErrNotFound
	}
	return Article{ID: id, AuthorID: "owner"}, nil
}

func (f *fakeStore) ListIllustrated(_ context.Context, limit int) ([]Article, error) {
	return articles(limit), nil
}

func (f *fakeStore) Enrich(_ context.Context, rows []Article) ([]Article, error) {
	f.enrichCalls++
	out := make([]Article, len(rows))
	for i, a := range rows {
		a.Deleted = a.ID == "gone"
		out[i] = a
	}
	return out, nil
}

type fakeCategories struct{}

func (fakeCategories) ListCategories(context.Context) ([]Category, error) { return nil, nil }

func (fakeCategories) FindCategoryBySlug(_ context.Context, slug string) (Category, error) {
	if slug == "ekonomi" {
		return Category{ID: "cat-ekonomi", Slug: slug}, nil
	}
	return Category{}, ErrCategoryNotFound
}

type fakeProcessor struct{}

func (fakeProcessor) Process(data []byte) ([]ProcessedFile, error) {
	if string(data) != "img" {
		return nil, ErrUnsupportedImage
	}
	return []ProcessedFile{{Name: "large.jpg"}, {Name: "thumb.jpg"}}, nil
}

type fakeObjects struct{ put, deleted []string }

func (f *fakeObjects) Put(_ context.Context, key, _ string, _ []byte) error {
	f.put = append(f.put, key)
	return nil
}

func (f *fakeObjects) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}

type fakeImages struct{ ImageStore }

func (fakeImages) AddImage(context.Context, Image, int) (Image, error) {
	return Image{}, ErrTooManyImages
}

type fakeComments struct{ CommentStore }

func (fakeComments) GetComment(_ context.Context, id string) (Comment, error) {
	return Comment{ID: id, UserID: "writer"}, nil
}

func (fakeComments) SetCommentDeleted(_ context.Context, id string, _ bool) (Comment, error) {
	return Comment{ID: id}, nil
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
	store   *fakeStore
	objects *fakeObjects
}

func newFixture(rows []Article) fixture {
	f := fixture{
		browser: &fakeReader{rows: rows},
		search:  &fakeReader{rows: rows},
		authors: &fakeAuthors{idsByName: map[string][]string{"Budi": {"id-1", "id-2"}}},
		writer:  &fakeWriter{},
		store:   &fakeStore{},
		objects: &fakeObjects{},
	}
	f.svc = NewService(Deps{
		Writer: f.writer, Store: f.store, Authors: f.authors, Categories: fakeCategories{},
		Browser: f.browser, Search: f.search, Images: fakeImages{}, Comments: fakeComments{},
		Processor: fakeProcessor{}, Objects: f.objects,
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

func TestListArticles_CategoryAndAuthorID(t *testing.T) {
	f := newFixture(articles(1))
	if _, err := f.svc.ListArticles(context.Background(), ListQuery{Category: "ekonomi", AuthorID: "au-9", AuthorName: "Budi"}); err != nil {
		t.Fatal(err)
	}
	if f.browser.params.CategoryID != "cat-ekonomi" {
		t.Fatalf("category slug should resolve to its id, got %q", f.browser.params.CategoryID)
	}
	if got := f.browser.params.AuthorIDs; len(got) != 1 || got[0] != "au-9" {
		t.Fatalf("an exact author id wins over the name filter, got %v", got)
	}

	var verr *ValidationError
	if _, err := f.svc.ListArticles(context.Background(), ListQuery{Category: "gosip"}); !errors.As(err, &verr) {
		t.Fatalf("unknown category should be a validation error, got %v", err)
	}
}

func TestListArticles_DropsArticlesDeletedSinceIndexing(t *testing.T) {
	f := newFixture([]Article{{ID: "a1"}, {ID: "gone"}, {ID: "a3"}})
	page, err := f.svc.ListArticles(context.Background(), ListQuery{Query: "banjir"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Articles) != 2 || page.Articles[1].ID != "a3" || f.store.enrichCalls != 1 {
		t.Fatalf("deleted article should be filtered after enrichment, got %+v", page.Articles)
	}
}

func TestListFeatured_EnrichesAndCaches(t *testing.T) {
	f := newFixture(nil)
	cache := &countingCache{}
	f.svc.d.Cache = cache
	rows, err := f.svc.ListFeatured(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || f.store.enrichCalls != 1 || len(cache.keys) != 1 || cache.keys[0] != "featured|l=3" {
		t.Fatalf("want 3 enriched rows through the cache, got %d rows, %d enrich calls, keys %v", len(rows), f.store.enrichCalls, cache.keys)
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

var (
	owner    = &Actor{UserID: "u-owner", AuthorID: "owner"}
	stranger = &Actor{UserID: "u-other", AuthorID: "other"}
	admin    = &Actor{UserID: "u-admin", AuthorID: "admin-author", Admin: true}
)

func TestCreateArticle(t *testing.T) {
	t.Run("uses the signed-in author and resolves the category", func(t *testing.T) {
		f := newFixture(nil)
		a, err := f.svc.CreateArticle(context.Background(), owner, CreateArticleInput{Title: " Judul ", Body: " Isi ", Category: "ekonomi"})
		if err != nil {
			t.Fatal(err)
		}
		if a.ID != "new-id" || a.Title != "Judul" || a.Body != "Isi" || a.AuthorID != "owner" || a.CategoryID != "cat-ekonomi" {
			t.Fatalf("unexpected article %+v", a)
		}
	})

	t.Run("requires a signed-in user", func(t *testing.T) {
		f := newFixture(nil)
		_, err := f.svc.CreateArticle(context.Background(), nil, CreateArticleInput{Title: "t", Body: "b", Category: "ekonomi"})
		if !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("want ErrUnauthenticated, got %v", err)
		}
	})

	t.Run("reports every invalid field", func(t *testing.T) {
		f := newFixture(nil)
		_, err := f.svc.CreateArticle(context.Background(), owner, CreateArticleInput{})
		var verr *ValidationError
		if !errors.As(err, &verr) || len(verr.Fields) != 3 {
			t.Fatalf("want 3 field errors, got %v", err)
		}
	})

	t.Run("unknown category is a validation error", func(t *testing.T) {
		f := newFixture(nil)
		_, err := f.svc.CreateArticle(context.Background(), owner, CreateArticleInput{Title: "t", Body: "b", Category: "gosip"})
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Fields[0].Field != "category" {
			t.Fatalf("want category validation error, got %v", err)
		}
	})

	t.Run("stores every image variant and marks the chosen cover", func(t *testing.T) {
		f := newFixture(nil)
		_, err := f.svc.CreateArticle(context.Background(), owner, CreateArticleInput{
			Title: "t", Body: "b", Category: "ekonomi", CoverIndex: 1,
			Images: []NewImage{{Data: []byte("img"), Caption: " satu "}, {Data: []byte("img")}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(f.writer.images) != 2 || f.writer.images[0].IsCover || !f.writer.images[1].IsCover {
			t.Fatalf("cover should be image 2, got %+v", f.writer.images)
		}
		if f.writer.images[0].Caption != "satu" || f.writer.images[1].Position != 1 {
			t.Fatalf("captions and positions should be kept, got %+v", f.writer.images)
		}
		if len(f.objects.put) != 4 {
			t.Fatalf("want 2 variants per image stored, got %v", f.objects.put)
		}
	})

	t.Run("rejects more than five images and non-images", func(t *testing.T) {
		f := newFixture(nil)
		six := make([]NewImage, 6)
		for i := range six {
			six[i] = NewImage{Data: []byte("img")}
		}
		var verr *ValidationError
		if _, err := f.svc.CreateArticle(context.Background(), owner, CreateArticleInput{Title: "t", Body: "b", Category: "ekonomi", Images: six}); !errors.As(err, &verr) {
			t.Fatalf("want validation error for 6 images, got %v", err)
		}
		_, err := f.svc.CreateArticle(context.Background(), owner, CreateArticleInput{Title: "t", Body: "b", Category: "ekonomi", Images: []NewImage{{Data: []byte("<html>")}}})
		if !errors.As(err, &verr) || verr.Fields[0].Field != "images" {
			t.Fatalf("want images validation error, got %v", err)
		}
	})
}

func TestUpdateArticle(t *testing.T) {
	t.Run("requires a field", func(t *testing.T) {
		f := newFixture(nil)
		_, err := f.svc.UpdateArticle(context.Background(), owner, "art-1", UpdateArticleInput{})
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("want ValidationError, got %v", err)
		}
	})

	t.Run("only the author or an admin may edit", func(t *testing.T) {
		title := "Baru"
		for name, tc := range map[string]struct {
			actor *Actor
			want  error
		}{
			"author":   {owner, nil},
			"admin":    {admin, nil},
			"stranger": {stranger, ErrForbidden},
			"guest":    {nil, ErrUnauthenticated},
		} {
			f := newFixture(nil)
			_, err := f.svc.UpdateArticle(context.Background(), tc.actor, "art-1", UpdateArticleInput{Title: &title})
			if !errors.Is(err, tc.want) {
				t.Errorf("%s: want %v, got %v", name, tc.want, err)
			}
		}
	})

	t.Run("missing article", func(t *testing.T) {
		f := newFixture(nil)
		title := "Baru"
		if _, err := f.svc.UpdateArticle(context.Background(), owner, "nope", UpdateArticleInput{Title: &title}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}

func TestDeleteArticle_ChecksOwnership(t *testing.T) {
	f := newFixture(nil)
	if err := f.svc.DeleteArticle(context.Background(), stranger, "art-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
	if err := f.svc.DeleteArticle(context.Background(), owner, "art-1"); err != nil || f.writer.deleted != "art-1" {
		t.Fatalf("author should delete, got %v", err)
	}
}

func TestAddImage_CleansUpWhenLimitReached(t *testing.T) {
	f := newFixture(nil)
	_, err := f.svc.AddImage(context.Background(), owner, "art-1", NewImage{Data: []byte("img")})
	if !errors.Is(err, ErrTooManyImages) {
		t.Fatalf("want ErrTooManyImages, got %v", err)
	}
	if len(f.objects.deleted) != len(f.objects.put) || len(f.objects.put) == 0 {
		t.Fatalf("stored files must be removed again, put %v deleted %v", f.objects.put, f.objects.deleted)
	}
}

func TestDeleteComment_OnlyWriterOrAdmin(t *testing.T) {
	f := newFixture(nil)
	if err := f.svc.DeleteComment(context.Background(), &Actor{UserID: "someone"}, "c1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
	if err := f.svc.DeleteComment(context.Background(), &Actor{UserID: "writer"}, "c1"); err != nil {
		t.Fatalf("writer may delete: %v", err)
	}
	if err := f.svc.DeleteComment(context.Background(), admin, "c1"); err != nil {
		t.Fatalf("admin may delete: %v", err)
	}
	if _, err := f.svc.RestoreComment(context.Background(), &Actor{UserID: "writer"}, "c1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("only admins restore, got %v", err)
	}
}

func TestCreateComment_Validates(t *testing.T) {
	f := newFixture(nil)
	var verr *ValidationError
	if _, err := f.svc.CreateComment(context.Background(), owner, "art-1", "   "); !errors.As(err, &verr) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	if _, err := f.svc.CreateComment(context.Background(), nil, "art-1", "halo"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("want ErrUnauthenticated, got %v", err)
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
		{Limit: 20, AuthorID: "a"},
		{Limit: 20, Category: "a"},
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
