//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"article-service/internal/account"
	"article-service/internal/article"
)

func mustUser(t *testing.T, pool *pgxpool.Pool, email string) account.User {
	t.Helper()
	u := account.User{ID: id(), Email: email, Name: "Budi Santoso", AuthorID: id(), Role: account.RoleUser, CreatedAt: time.Now()}
	if err := NewAccountRepo(pool).CreateUser(context.Background(), u, "hash"); err != nil {
		t.Fatal(err)
	}
	return u
}

func mustArticle(t *testing.T, pool *pgxpool.Pool, authorID, categoryID string) string {
	t.Helper()
	a, err := NewArticleRepo(pool).CreateArticle(context.Background(),
		article.Article{ID: id(), AuthorID: authorID, CategoryID: categoryID, Title: "Judul", Body: "Isi"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a.ID
}

func stats(t *testing.T, pool *pgxpool.Pool, articleID string) (likes, comments int64) {
	t.Helper()
	a, err := NewArticleRepo(pool).GetArticle(context.Background(), articleID, "")
	if err != nil {
		t.Fatal(err)
	}
	return a.LikeCount, a.CommentCount
}

func TestAccounts(t *testing.T) {
	pool := newTestDB(t)
	repo := NewAccountRepo(pool)
	ctx := context.Background()
	u := mustUser(t, pool, "Budi@Contoh.id")

	dup := account.User{ID: id(), Email: "budi@contoh.id", Name: "X", AuthorID: id(), Role: account.RoleUser, CreatedAt: time.Now()}
	if err := repo.CreateUser(ctx, dup, "h"); !errors.Is(err, account.ErrEmailTaken) {
		t.Fatalf("email must be unique case-insensitively, got %v", err)
	}

	if err := repo.CreateSession(ctx, account.Session{ID: id(), UserID: u.ID, TokenHash: "hash-1", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindSessionUser(ctx, "hash-1", time.Now())
	if err != nil || got.ID != u.ID || got.Name != "Budi Santoso" {
		t.Fatalf("session should resolve to the user with its author name: %+v %v", got, err)
	}
	if _, err := repo.FindSessionUser(ctx, "hash-1", time.Now().Add(2*time.Hour)); !errors.Is(err, account.ErrSessionInvalid) {
		t.Fatalf("expired session must be rejected, got %v", err)
	}

	if err := repo.SetDisabled(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindSessionUser(ctx, "hash-1", time.Now()); !errors.Is(err, account.ErrSessionInvalid) {
		t.Fatalf("blocking must end existing sessions, got %v", err)
	}

	found, err := repo.SearchUsers(ctx, "budi_", 10)
	if err != nil || len(found) != 0 {
		t.Fatalf("LIKE wildcards in the query must be escaped, got %v %v", found, err)
	}
	found, _ = repo.SearchUsers(ctx, "santoso", 10)
	if len(found) != 1 || found[0].DisabledAt == nil {
		t.Fatalf("want the blocked user, got %+v", found)
	}
}

func TestLikes_AreIdempotentAndCounted(t *testing.T) {
	pool := newTestDB(t)
	repo := NewEngagementRepo(pool)
	ctx := context.Background()
	u1, u2 := mustUser(t, pool, "a@contoh.id"), mustUser(t, pool, "b@contoh.id")
	art := mustArticle(t, pool, u1.AuthorID, "ekonomi")

	for _, uid := range []string{u1.ID, u1.ID, u2.ID} { // u1 likes twice
		if err := repo.Like(ctx, art, uid); err != nil {
			t.Fatal(err)
		}
	}
	if likes, _ := stats(t, pool, art); likes != 2 {
		t.Fatalf("a repeated like must not count twice, got %d", likes)
	}
	for range 2 {
		if err := repo.Unlike(ctx, art, u2.ID); err != nil {
			t.Fatal(err)
		}
	}
	if likes, _ := stats(t, pool, art); likes != 1 {
		t.Fatalf("want 1 like after unlike, got %d", likes)
	}
	if a, _ := NewArticleRepo(pool).GetArticle(ctx, art, u1.ID); !a.LikedByMe {
		t.Fatal("liked_by_me should be true for u1")
	}
	if err := repo.Like(ctx, "missing", u1.ID); !errors.Is(err, article.ErrNotFound) {
		t.Fatalf("want ErrNotFound for a missing article, got %v", err)
	}
}

func TestComments_CountFollowsDeleteAndRestore(t *testing.T) {
	pool := newTestDB(t)
	repo := NewEngagementRepo(pool)
	ctx := context.Background()
	u := mustUser(t, pool, "a@contoh.id")
	art := mustArticle(t, pool, u.AuthorID, "")

	c1, err := repo.CreateComment(ctx, article.Comment{ID: id(), ArticleID: art, UserID: u.ID, Body: "satu"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateComment(ctx, article.Comment{ID: id(), ArticleID: art, UserID: u.ID, Body: "dua"}); err != nil {
		t.Fatal(err)
	}
	if c1.AuthorName != "Budi Santoso" || c1.ArticleTitle != "Judul" {
		t.Fatalf("comment should carry writer and article: %+v", c1)
	}

	for range 2 { // deleting twice must only count once
		if _, err := repo.SetCommentDeleted(ctx, c1.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, comments := stats(t, pool, art); comments != 1 {
		t.Fatalf("want 1 visible comment, got %d", comments)
	}
	list, _ := repo.ListComments(ctx, art, nil, 10)
	if len(list) != 1 || list[0].Body != "dua" {
		t.Fatalf("deleted comments are hidden from readers, got %+v", list)
	}
	recent, _ := repo.ListRecentComments(ctx, nil, 10)
	if len(recent) != 2 {
		t.Fatalf("moderators see deleted comments too, got %d", len(recent))
	}

	if _, err := repo.SetCommentDeleted(ctx, c1.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, comments := stats(t, pool, art); comments != 2 {
		t.Fatalf("restore should count again, got %d", comments)
	}
	edited, err := repo.UpdateComment(ctx, c1.ID, "satu (revisi)")
	if err != nil || edited.Body != "satu (revisi)" || !edited.UpdatedAt.After(edited.CreatedAt) {
		t.Fatalf("edit failed: %+v %v", edited, err)
	}
}

func TestImages_LimitCoverAndOrder(t *testing.T) {
	pool := newTestDB(t)
	repo := NewImageRepo(pool)
	ctx := context.Background()
	u := mustUser(t, pool, "a@contoh.id")
	art := mustArticle(t, pool, u.AuthorID, "")

	var ids []string
	for range 5 {
		img, err := repo.AddImage(ctx, article.Image{ID: id(), ArticleID: art, Key: "k"}, 5)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, img.ID)
	}
	if _, err := repo.AddImage(ctx, article.Image{ID: id(), ArticleID: art, Key: "k"}, 5); !errors.Is(err, article.ErrTooManyImages) {
		t.Fatalf("the sixth image must be refused, got %v", err)
	}

	// Full reversal swaps every position; only a deferred unique constraint allows it.
	reversed := []string{ids[4], ids[3], ids[2], ids[1], ids[0]}
	if err := repo.ReorderImages(ctx, art, reversed); err != nil {
		t.Fatal(err)
	}
	var verr *article.ValidationError
	if err := repo.ReorderImages(ctx, art, ids[:4]); !errors.As(err, &verr) {
		t.Fatalf("an incomplete order must be refused, got %v", err)
	}

	if err := repo.SetCover(ctx, art, ids[2]); err != nil {
		t.Fatal(err)
	}
	a, _ := NewArticleRepo(pool).GetArticle(ctx, art, "")
	if a.Images[0].ID != ids[4] || a.Cover == nil || a.Cover.ID != ids[2] || a.ImageCount != 5 {
		t.Fatalf("order or cover wrong: first=%s cover=%+v", a.Images[0].ID, a.Cover)
	}

	if _, err := repo.DeleteImage(ctx, ids[2]); err != nil {
		t.Fatal(err)
	}
	a, _ = NewArticleRepo(pool).GetArticle(ctx, art, "")
	if a.Cover == nil || a.Cover.ID != ids[4] {
		t.Fatalf("deleting the cover should promote the first image, got %+v", a.Cover)
	}
}

func TestSoftDelete_HidesEverywhereAndEnqueues(t *testing.T) {
	pool := newTestDB(t)
	repo := NewArticleRepo(pool)
	ctx := context.Background()
	u := mustUser(t, pool, "a@contoh.id")
	keep := mustArticle(t, pool, u.AuthorID, "ekonomi")
	gone := mustArticle(t, pool, u.AuthorID, "olahraga")

	if err := repo.DeleteArticle(ctx, gone); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteArticle(ctx, gone); !errors.Is(err, article.ErrNotFound) {
		t.Fatalf("deleting twice should report not found, got %v", err)
	}
	if _, err := repo.GetArticle(ctx, gone, ""); !errors.Is(err, article.ErrNotFound) {
		t.Fatalf("deleted article must not be readable, got %v", err)
	}
	if n := outboxCount(t, pool); n != 3 {
		t.Fatalf("want 2 creates + 1 delete in the outbox, got %d", n)
	}

	feed, _ := repo.List(ctx, article.ListParams{Limit: 10})
	byAuthor, _ := repo.List(ctx, article.ListParams{AuthorIDs: []string{u.AuthorID}, Limit: 10})
	if len(feed) != 1 || feed[0].ID != keep || len(byAuthor) != 1 {
		t.Fatalf("deleted article must leave both feed shapes: %d %d", len(feed), len(byAuthor))
	}

	enriched, _ := repo.Enrich(ctx, []article.Article{{ID: keep}, {ID: gone}})
	if enriched[0].Deleted || enriched[0].Category == nil || enriched[0].Category.Name != "Ekonomi" || !enriched[1].Deleted {
		t.Fatalf("enrich should add category and flag deleted rows: %+v", enriched)
	}

	byCategory, _ := repo.List(ctx, article.ListParams{CategoryID: "ekonomi", AuthorIDs: []string{u.AuthorID}, Limit: 10})
	if len(byCategory) != 1 || byCategory[0].ID != keep {
		t.Fatalf("category filter combined with author failed: %+v", byCategory)
	}
}

func TestListIllustrated_OnlyLiveArticlesWithACover(t *testing.T) {
	pool := newTestDB(t)
	ctx := context.Background()
	u := mustUser(t, pool, "a@contoh.id")
	images := NewImageRepo(pool)
	repo := NewArticleRepo(pool)

	older := mustArticle(t, pool, u.AuthorID, "")
	_ = mustArticle(t, pool, u.AuthorID, "") // text only
	newer := mustArticle(t, pool, u.AuthorID, "")
	gone := mustArticle(t, pool, u.AuthorID, "")
	for _, id := range []string{newer, older, gone} { // older gets its cover last
		if _, err := images.AddImage(ctx, article.Image{ID: id + "-img", ArticleID: id, Key: "k"}, 5); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.DeleteArticle(ctx, gone); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListIllustrated(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != newer || got[1].ID != older {
		t.Fatalf("want [newer older] newest-article-first, got %v", got)
	}
}
