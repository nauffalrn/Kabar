package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"article-service/internal/account"
	"article-service/internal/article"
	"article-service/internal/storage"
)

// fakeArticles implements only what the tests call; the embedded nil
// interface panics on anything else, which the recover middleware reports.
type fakeArticles struct {
	ArticleService
	listQuery article.ListQuery
	page      article.Page
	err       error
	update    article.UpdateArticleInput
	created   article.CreateArticleInput
	actor     *article.Actor
}

func (f *fakeArticles) ListArticles(_ context.Context, q article.ListQuery) (article.Page, error) {
	f.listQuery = q
	return f.page, f.err
}

func (f *fakeArticles) ListFeatured(_ context.Context, limit int) ([]article.Article, error) {
	return []article.Article{{ID: fmt.Sprint("featured-", limit)}}, f.err
}

func (f *fakeArticles) CreateArticle(_ context.Context, a *article.Actor, in article.CreateArticleInput) (article.Article, error) {
	f.actor, f.created = a, in
	if f.err != nil {
		return article.Article{}, f.err
	}
	if a == nil {
		return article.Article{}, article.ErrUnauthenticated
	}
	return article.Article{ID: "ar-1", AuthorID: a.AuthorID, AuthorName: "Budi", Title: in.Title, Body: in.Body}, nil
}

func (f *fakeArticles) UpdateArticle(_ context.Context, a *article.Actor, id string, in article.UpdateArticleInput) (article.Article, error) {
	f.actor, f.update = a, in
	return article.Article{ID: id}, f.err
}

func (f *fakeArticles) GetArticle(_ context.Context, id string, viewer *article.Actor) (article.Article, error) {
	f.actor = viewer
	return article.Article{ID: id, LikedByMe: viewer != nil}, f.err
}

type fakeAccounts struct {
	AccountService
	user account.User
	err  error
}

func (f *fakeAccounts) Authenticate(_ context.Context, token string) (account.User, error) {
	if token != "good-token" {
		return account.User{}, account.ErrSessionInvalid
	}
	return f.user, nil
}

func (f *fakeAccounts) Login(context.Context, string, string) (account.User, string, error) {
	return f.user, "good-token", f.err
}

type fakeMedia struct{}

func (fakeMedia) Get(_ context.Context, key string) (storage.Object, error) {
	if key != "articles/a/i/thumb.jpg" {
		return storage.Object{}, storage.ErrNotFound
	}
	return storage.Object{Body: io.NopCloser(strings.NewReader("jpg")), ContentType: "image/jpeg", Size: 3}, nil
}

var budi = account.User{ID: "u-1", AuthorID: "au-1", Name: "Budi", Role: account.RoleUser}

func router(svc ArticleService) http.Handler {
	return NewRouter(Deps{
		Articles: svc, Accounts: &fakeAccounts{user: budi}, Media: fakeMedia{},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

type reqOpt func(*http.Request)

func signedIn(r *http.Request) { r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good-token"}) }

func serve(t *testing.T, svc ArticleService, method, target, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	router(svc).ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestCreateArticle_UsesTheSessionAuthor(t *testing.T) {
	svc := &fakeArticles{}
	rec := serve(t, svc, http.MethodPost, "/articles", `{"title":"Judul","body":"Isi","category":"ekonomi"}`, signedIn)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if svc.actor == nil || svc.actor.AuthorID != "au-1" || svc.created.Category != "ekonomi" {
		t.Fatalf("actor and category should come through, got %+v %+v", svc.actor, svc.created)
	}
	if got := decode[articleResponse](t, rec); got.ID != "ar-1" || got.Author.ID != "au-1" {
		t.Fatalf("unexpected body %+v", got)
	}
}

func TestCreateArticle_GuestGets401(t *testing.T) {
	rec := serve(t, &fakeArticles{}, http.MethodPost, "/articles", `{"title":"t","body":"b","category":"ekonomi"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}

func TestCreateArticle_Multipart(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "Judul")
	_ = mw.WriteField("body", "Isi")
	_ = mw.WriteField("category", "ekonomi")
	_ = mw.WriteField("cover_index", "1")
	for i, caption := range []string{"satu", "dua"} {
		fw, _ := mw.CreateFormFile("images", fmt.Sprintf("foto%d.jpg", i))
		_, _ = fw.Write([]byte("image-bytes"))
		_ = mw.WriteField("captions", caption)
	}
	_ = mw.Close()

	svc := &fakeArticles{}
	req := httptest.NewRequest(http.MethodPost, "/articles", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	signedIn(req)
	rec := httptest.NewRecorder()
	router(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	in := svc.created
	if len(in.Images) != 2 || in.CoverIndex != 1 || in.Images[1].Caption != "dua" || string(in.Images[0].Data) != "image-bytes" {
		t.Fatalf("multipart fields not parsed: %+v", in)
	}
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		method   string
		target   string
		body     string
		wantCode int
		wantErr  string
	}{
		{"validation", &article.ValidationError{Fields: []article.FieldError{{Field: "title", Message: "is required"}}},
			http.MethodPost, "/articles", `{}`, 400, "validation_error"},
		{"not found", article.ErrNotFound, http.MethodPatch, "/articles/x", `{"title":"t"}`, 404, "not_found"},
		{"forbidden", article.ErrForbidden, http.MethodPatch, "/articles/x", `{"title":"t"}`, 403, "forbidden"},
		{"too many images", article.ErrTooManyImages, http.MethodPost, "/articles", `{}`, 422, "too_many_images"},
		{"email taken", account.ErrEmailTaken, http.MethodPost, "/articles", `{}`, 409, "email_taken"},
		{"backend unavailable", fmt.Errorf("list: %w", article.ErrUnavailable), http.MethodGet, "/articles?query=x", ``, 503, "unavailable"},
		{"timeout", context.DeadlineExceeded, http.MethodGet, "/articles", ``, 503, "unavailable"},
		{"unexpected", errors.New("boom"), http.MethodGet, "/articles", ``, 500, "internal_error"},
		{"bad json", nil, http.MethodPost, "/articles", `{`, 400, "invalid_json"},
		{"unknown field", nil, http.MethodPost, "/articles", `{"titel":"typo"}`, 400, "invalid_json"},
		{"body too large", nil, http.MethodPost, "/articles", `{"body":"` + strings.Repeat("a", maxBodyBytes) + `"}`, 413, "body_too_large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, &fakeArticles{err: tt.err}, tt.method, tt.target, tt.body, signedIn)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.wantCode, rec.Body)
			}
			if got := decode[errorBody](t, rec).Error.Code; got != tt.wantErr {
				t.Fatalf("error code = %q, want %q", got, tt.wantErr)
			}
			if wantRetry := tt.wantCode == http.StatusServiceUnavailable; (rec.Header().Get("Retry-After") != "") != wantRetry {
				t.Fatalf("Retry-After = %q, want present=%v", rec.Header().Get("Retry-After"), wantRetry)
			}
		})
	}
}

func TestWrites_RequireJSONContentType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/articles/x", strings.NewReader(`{"title":"t"}`))
	req.Header.Set("Content-Type", "text/plain")
	signedIn(req)
	rec := httptest.NewRecorder()
	router(&fakeArticles{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status %d, want 415", rec.Code)
	}
}

func TestOriginCheck_BlocksCrossSiteWrites(t *testing.T) {
	cross := func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }
	if rec := serve(t, &fakeArticles{}, http.MethodPatch, "/articles/x", `{"title":"t"}`, signedIn, cross); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site write: status %d, want 403", rec.Code)
	}
	same := func(r *http.Request) { r.Header.Set("Origin", "http://"+r.Host) }
	if rec := serve(t, &fakeArticles{}, http.MethodPatch, "/articles/x", `{"title":"t"}`, signedIn, same); rec.Code != http.StatusOK {
		t.Fatalf("same-site write: status %d, want 200", rec.Code)
	}
	if rec := serve(t, &fakeArticles{}, http.MethodGet, "/articles", "", cross); rec.Code != http.StatusOK {
		t.Fatalf("cross-site read should pass, got %d", rec.Code)
	}
}

func TestLogin_SetsHardenedSessionCookie(t *testing.T) {
	rec := serve(t, &fakeArticles{}, http.MethodPost, "/auth/login", `{"email":"budi@contoh.id","password":"rahasia123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookie || cookies[0].Value != "good-token" ||
		!cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected cookie %+v", cookies)
	}
}

func TestGetArticle_LikedByMeOnlyForSignedInViewers(t *testing.T) {
	guest := serve(t, &fakeArticles{}, http.MethodGet, "/articles/ar-1", "")
	if strings.Contains(guest.Body.String(), "liked_by_me") {
		t.Fatalf("guests should not get liked_by_me: %s", guest.Body)
	}
	user := serve(t, &fakeArticles{}, http.MethodGet, "/articles/ar-1", "", signedIn)
	if !strings.Contains(user.Body.String(), `"liked_by_me":true`) {
		t.Fatalf("signed-in viewer should get liked_by_me: %s", user.Body)
	}
}

func TestMe(t *testing.T) {
	if rec := serve(t, &fakeArticles{}, http.MethodGet, "/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("guest: status %d, want 401", rec.Code)
	}
	expired := func(r *http.Request) { r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "old"}) }
	if rec := serve(t, &fakeArticles{}, http.MethodGet, "/auth/me", "", expired); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired session: status %d, want 401", rec.Code)
	}
	rec := serve(t, &fakeArticles{}, http.MethodGet, "/auth/me", "", signedIn)
	if got := decode[userResponse](t, rec); rec.Code != http.StatusOK || got.AuthorID != "au-1" {
		t.Fatalf("status %d, body %+v", rec.Code, got)
	}
}

func TestMedia(t *testing.T) {
	rec := serve(t, &fakeArticles{}, http.MethodGet, "/media/articles/a/i/thumb.jpg", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" ||
		!strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}
	for _, target := range []string{"/media/articles/missing.jpg", "/media/secrets/env"} {
		if rec := serve(t, &fakeArticles{}, http.MethodGet, target, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", target, rec.Code)
		}
	}
	// The router cleans "..", so a traversal never reaches the handler with a file.
	if rec := serve(t, &fakeArticles{}, http.MethodGet, "/media/articles/../x", ""); rec.Code == http.StatusOK {
		t.Fatal("path traversal must not serve a file")
	}
}

func TestListArticles_ParsesQueryParameters(t *testing.T) {
	cur := article.Cursor{CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ID: "x"}
	svc := &fakeArticles{page: article.Page{
		Articles:   []article.Article{{ID: "a1", Body: strings.Repeat("kata ", 100)}},
		NextCursor: &cur,
	}}

	rec := serve(t, svc, http.MethodGet, "/articles?query=banjir+jakarta&author=Budi&author_id=au-9&category=ekonomi&limit=5&cursor="+cur.Encode(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	q := svc.listQuery
	if q.Query != "banjir jakarta" || q.AuthorName != "Budi" || q.AuthorID != "au-9" || q.Category != "ekonomi" ||
		q.Limit != 5 || q.After == nil || q.After.ID != "x" {
		t.Fatalf("query not parsed correctly: %+v", q)
	}

	body := decode[listResponse](t, rec)
	if len(body.Data) != 1 || body.NextCursor == nil || *body.NextCursor != cur.Encode() {
		t.Fatalf("unexpected list body: %+v", body)
	}
	if n := len([]rune(body.Data[0].Excerpt)); n > excerptRunes+1 {
		t.Fatalf("excerpt should be cut to %d runes, got %d", excerptRunes, n)
	}
}

func TestListFeatured_IsNotMistakenForAnArticleID(t *testing.T) {
	rec := serve(t, &fakeArticles{}, http.MethodGet, "/articles/featured?limit=7", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if body := decode[listResponse](t, rec); len(body.Data) != 1 || body.Data[0].ID != "featured-7" {
		t.Fatalf("the literal route must win over /articles/{id}: %+v", body)
	}
}

func TestListArticles_EmptyPageIsAnArrayNotNull(t *testing.T) {
	rec := serve(t, &fakeArticles{page: article.Page{}}, http.MethodGet, "/articles", "")
	if !strings.Contains(rec.Body.String(), `"data":[]`) || !strings.Contains(rec.Body.String(), `"next_cursor":null`) {
		t.Fatalf("unexpected body: %s", rec.Body)
	}
}

func TestListArticles_RejectsBadParams(t *testing.T) {
	for _, target := range []string{"/articles?limit=abc", "/articles?limit=0", "/articles?cursor=@@@"} {
		if rec := serve(t, &fakeArticles{}, http.MethodGet, target, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", target, rec.Code)
		}
	}
}

func TestUpdateArticle_PartialFields(t *testing.T) {
	svc := &fakeArticles{}
	rec := serve(t, svc, http.MethodPatch, "/articles/ar-9", `{"title":"Baru"}`, signedIn)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if svc.update.Title == nil || *svc.update.Title != "Baru" || svc.update.Body != nil || svc.update.Category != nil {
		t.Fatalf("only title should be set: %+v", svc.update)
	}
}

func TestRecoverMiddleware(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := withRecover(log, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("oops") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
}
