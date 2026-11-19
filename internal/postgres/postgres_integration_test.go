//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"article-service/internal/article"
	"article-service/migrations"
)

// newTestDB gives each test a freshly migrated, empty schema.
func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := Connect(ctx, PoolConfig{URL: url, MaxConns: 5})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	return pool
}

func id() string { return uuid.Must(uuid.NewV7()).String() }

func mustAuthor(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	a := article.Author{ID: id(), Name: name}
	if err := NewAuthorRepo(pool).CreateAuthor(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a.ID
}

// insertAt writes an article with an explicit created_at, which the public
// API never allows, so ordering and ties can be tested precisely.
func insertAt(t *testing.T, pool *pgxpool.Pool, authorID string, at time.Time) string {
	t.Helper()
	aid := id()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO articles (id, author_id, title, body, created_at) VALUES ($1, $2, 't', 'b', $3)",
		aid, authorID, at); err != nil {
		t.Fatal(err)
	}
	return aid
}

func outboxCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM article_outbox").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// readAll pages through a reader and returns ids in order.
func readAll(t *testing.T, r article.Reader, authorIDs []string, pageSize int) []string {
	t.Helper()
	var ids []string
	var after *article.Cursor
	for range 1000 {
		rows, err := r.List(context.Background(), article.ListParams{AuthorIDs: authorIDs, Limit: pageSize, After: after})
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range rows {
			ids = append(ids, a.ID)
		}
		if len(rows) < pageSize {
			return ids
		}
		c := article.CursorOf(rows[len(rows)-1])
		after = &c
	}
	t.Fatal("pagination did not terminate")
	return nil
}

func TestCreateArticle_ReturnsAuthorAndEnqueues(t *testing.T) {
	pool := newTestDB(t)
	repo := NewArticleRepo(pool)
	authorID := mustAuthor(t, pool, "Budi")

	got, err := repo.CreateArticle(context.Background(), article.Article{ID: id(), AuthorID: authorID, Title: "Judul", Body: "Isi"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthorName != "Budi" || got.Version != 1 || got.CreatedAt.IsZero() {
		t.Fatalf("unexpected %+v", got)
	}
	if n := outboxCount(t, pool); n != 1 {
		t.Fatalf("write must enqueue exactly one outbox event, got %d", n)
	}
}

func TestCreateArticle_UnknownAuthor(t *testing.T) {
	pool := newTestDB(t)
	_, err := NewArticleRepo(pool).CreateArticle(context.Background(), article.Article{ID: id(), AuthorID: "missing", Title: "t", Body: "b"}, nil)
	if !errors.Is(err, article.ErrAuthorNotFound) {
		t.Fatalf("want ErrAuthorNotFound, got %v", err)
	}
	if n := outboxCount(t, pool); n != 0 {
		t.Fatal("failed write must not leave an outbox event (same transaction)")
	}
}

func TestUpdateArticle(t *testing.T) {
	pool := newTestDB(t)
	repo := NewArticleRepo(pool)
	created, _ := repo.CreateArticle(context.Background(), article.Article{ID: id(), AuthorID: mustAuthor(t, pool, "A"), Title: "old", Body: "body"}, nil)

	title := "new"
	got, err := repo.UpdateArticle(context.Background(), created.ID, article.ArticleChanges{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "new" || got.Body != "body" || got.Version != 2 {
		t.Fatalf("partial update wrong: %+v", got)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Fatal("created_at must never change on edit")
	}
	if n := outboxCount(t, pool); n != 2 {
		t.Fatalf("want 2 outbox events (create + update), got %d", n)
	}

	if _, err := repo.UpdateArticle(context.Background(), "missing", article.ArticleChanges{Title: &title}); !errors.Is(err, article.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestList_NewestFirstWithoutGapsOrDuplicates(t *testing.T) {
	pool := newTestDB(t)
	authorID := mustAuthor(t, pool, "A")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// 25 articles; groups of 3 share the exact same created_at to exercise the id tie-breaker.
	for i := range 25 {
		insertAt(t, pool, authorID, base.Add(time.Duration(i/3)*time.Second))
	}

	repo := NewArticleRepo(pool)
	all := readAll(t, repo, nil, 25)
	paged := readAll(t, repo, nil, 4)

	if len(paged) != 25 || fmt.Sprint(paged) != fmt.Sprint(all) {
		t.Fatalf("paging must return the same 25 rows in the same order:\nall:   %v\npaged: %v", all, paged)
	}

	rows, _ := repo.List(context.Background(), article.ListParams{Limit: 25})
	for i := 1; i < len(rows); i++ {
		prev, cur := rows[i-1], rows[i]
		if cur.CreatedAt.After(prev.CreatedAt) || (cur.CreatedAt.Equal(prev.CreatedAt) && cur.ID > prev.ID) {
			t.Fatalf("not ordered by (created_at, id) desc at %d", i)
		}
	}
}

func TestList_AuthorNameMapsToSeveralIDs(t *testing.T) {
	pool := newTestDB(t)
	big := mustAuthor(t, pool, "Syndication Wire")
	small := mustAuthor(t, pool, "syndication wire") // same name, different case
	other := mustAuthor(t, pool, "Someone Else")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	want := map[string]bool{}
	for i := range 30 {
		want[insertAt(t, pool, big, base.Add(time.Duration(i)*time.Minute))] = true
	}
	for i := range 5 {
		want[insertAt(t, pool, small, base.Add(time.Duration(i)*7*time.Minute+time.Second))] = true
	}
	for i := range 10 {
		insertAt(t, pool, other, base.Add(time.Duration(i)*time.Minute))
	}

	ids, err := NewAuthorRepo(pool).FindAuthorIDsByName(context.Background(), "SYNDICATION WIRE")
	if err != nil || len(ids) != 2 {
		t.Fatalf("name lookup must be case-insensitive and return both ids: %v %v", ids, err)
	}

	repo := NewArticleRepo(pool)
	got := readAll(t, repo, ids, 4)
	if len(got) != len(want) {
		t.Fatalf("want %d articles from both same-name authors, got %d", len(want), len(got))
	}
	for _, g := range got {
		if !want[g] {
			t.Fatalf("article %s belongs to another author", g)
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(readAll(t, repo, ids, 100)) {
		t.Fatal("LATERAL merge must give the same order regardless of page size")
	}
}

func TestOutbox_ProcessBatch(t *testing.T) {
	pool := newTestDB(t)
	repo := NewArticleRepo(pool)
	a, _ := repo.CreateArticle(context.Background(), article.Article{ID: id(), AuthorID: mustAuthor(t, pool, "A"), Title: "v1", Body: "b"}, nil)
	v2 := "v2"
	_, _ = repo.UpdateArticle(context.Background(), a.ID, article.ArticleChanges{Title: &v2})

	outbox := NewOutboxRepo(pool)

	t.Run("failure keeps events for retry", func(t *testing.T) {
		_, err := outbox.ProcessBatch(context.Background(), 10, func(context.Context, []article.Article) error {
			return errors.New("es down")
		})
		if err == nil || outboxCount(t, pool) != 2 {
			t.Fatal("events must survive a failed handler")
		}
	})

	t.Run("success collapses duplicates to latest state and deletes events", func(t *testing.T) {
		var got []article.Article
		n, err := outbox.ProcessBatch(context.Background(), 10, func(_ context.Context, as []article.Article) error {
			got = as
			return nil
		})
		if err != nil || n != 2 {
			t.Fatalf("n=%d err=%v", n, err)
		}
		if len(got) != 1 || got[0].Title != "v2" || got[0].Version != 2 {
			t.Fatalf("want one doc with the latest version, got %+v", got)
		}
		if outboxCount(t, pool) != 0 {
			t.Fatal("processed events must be deleted")
		}
	})
}

// TestService_EndToEndBrowse wires the real repositories into the service.
func TestService_EndToEndBrowse(t *testing.T) {
	pool := newTestDB(t)
	repo := NewArticleRepo(pool)
	svc := article.NewService(article.Deps{
		Writer: repo, Store: repo, Authors: NewAuthorRepo(pool), Categories: NewCategoryRepo(pool), Browser: repo,
		NewID: func() string { return id() },
	})
	ctx := context.Background()

	au, err := svc.CreateAuthor(ctx, "Budi")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if _, err := svc.CreateArticle(ctx, &article.Actor{AuthorID: au.ID}, article.CreateArticleInput{Title: fmt.Sprint("t", i), Body: "b", Category: "ekonomi"}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := svc.ListArticles(ctx, article.ListQuery{AuthorName: "budi", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Articles) != 2 || page.NextCursor == nil || page.Articles[0].Title != "t2" {
		t.Fatalf("want newest 2 + cursor, got %+v", page)
	}
	rest, err := svc.ListArticles(ctx, article.ListQuery{AuthorName: "budi", Limit: 2, After: page.NextCursor})
	if err != nil || len(rest.Articles) != 1 || rest.NextCursor != nil || rest.Articles[0].Title != "t0" {
		t.Fatalf("second page wrong: %+v %v", rest, err)
	}
}
