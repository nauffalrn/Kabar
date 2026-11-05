package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"article-service/internal/article"
)

type fakeService struct {
	listQuery article.ListQuery
	page      article.Page
	err       error
	update    article.UpdateArticleInput
}

func (f *fakeService) CreateAuthor(_ context.Context, name string) (article.Author, error) {
	return article.Author{ID: "au-1", Name: name}, f.err
}

func (f *fakeService) CreateArticle(_ context.Context, in article.CreateArticleInput) (article.Article, error) {
	return article.Article{ID: "ar-1", AuthorID: in.AuthorID, AuthorName: "Budi", Title: in.Title, Body: in.Body}, f.err
}

func (f *fakeService) UpdateArticle(_ context.Context, id string, in article.UpdateArticleInput) (article.Article, error) {
	f.update = in
	return article.Article{ID: id}, f.err
}

func (f *fakeService) ListArticles(_ context.Context, q article.ListQuery) (article.Page, error) {
	f.listQuery = q
	return f.page, f.err
}

func serve(t *testing.T, svc Service, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := httptest.NewRecorder()
	NewRouter(svc, log).ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
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

func TestCreateArticle(t *testing.T) {
	rec := serve(t, &fakeService{}, http.MethodPost, "/articles", `{"author_id":"au-1","title":"Judul","body":"Isi"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	got := decode[articleResponse](t, rec)
	if got.ID != "ar-1" || got.Author.ID != "au-1" || got.Author.Name != "Budi" {
		t.Fatalf("unexpected body %+v", got)
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
		{"unknown author", article.ErrAuthorNotFound, http.MethodPost, "/articles", `{}`, 422, "author_not_found"},
		{"not found", article.ErrNotFound, http.MethodPatch, "/articles/x", `{"title":"t"}`, 404, "not_found"},
		{"backend unavailable", fmt.Errorf("list: %w", article.ErrUnavailable), http.MethodGet, "/articles?query=x", ``, 503, "unavailable"},
		{"timeout", context.DeadlineExceeded, http.MethodGet, "/articles", ``, 503, "unavailable"},
		{"unexpected", errors.New("boom"), http.MethodGet, "/articles", ``, 500, "internal_error"},
		{"bad json", nil, http.MethodPost, "/articles", `{`, 400, "invalid_json"},
		{"unknown field", nil, http.MethodPost, "/articles", `{"titel":"typo"}`, 400, "invalid_json"},
		{"body too large", nil, http.MethodPost, "/articles", `{"body":"` + strings.Repeat("a", maxBodyBytes) + `"}`, 413, "body_too_large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, &fakeService{err: tt.err}, tt.method, tt.target, tt.body)
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

func TestListArticles_ParsesQueryParameters(t *testing.T) {
	cur := article.Cursor{CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ID: "x"}
	svc := &fakeService{page: article.Page{
		Articles:   []article.Article{{ID: "a1"}},
		NextCursor: &cur,
	}}

	rec := serve(t, svc, http.MethodGet, "/articles?query=banjir+jakarta&author=Budi&limit=5&cursor="+cur.Encode(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	q := svc.listQuery
	if q.Query != "banjir jakarta" || q.AuthorName != "Budi" || q.Limit != 5 || q.After == nil || q.After.ID != "x" {
		t.Fatalf("query not parsed correctly: %+v", q)
	}

	body := decode[listResponse](t, rec)
	if len(body.Data) != 1 || body.NextCursor == nil || *body.NextCursor != cur.Encode() {
		t.Fatalf("unexpected list body: %+v", body)
	}
}

func TestListArticles_EmptyPageIsAnArrayNotNull(t *testing.T) {
	rec := serve(t, &fakeService{page: article.Page{}}, http.MethodGet, "/articles", "")
	if !strings.Contains(rec.Body.String(), `"data":[]`) || !strings.Contains(rec.Body.String(), `"next_cursor":null`) {
		t.Fatalf("unexpected body: %s", rec.Body)
	}
}

func TestListArticles_RejectsBadParams(t *testing.T) {
	for _, target := range []string{"/articles?limit=abc", "/articles?limit=0", "/articles?cursor=@@@"} {
		if rec := serve(t, &fakeService{}, http.MethodGet, target, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", target, rec.Code)
		}
	}
}

func TestUpdateArticle_PartialFields(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, http.MethodPatch, "/articles/ar-9", `{"title":"Baru"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if svc.update.Title == nil || *svc.update.Title != "Baru" || svc.update.Body != nil {
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
