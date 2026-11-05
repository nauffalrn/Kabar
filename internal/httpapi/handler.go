// Package httpapi exposes the article service as a REST API.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"article-service/internal/article"
)

// Service is declared on the consumer side so tests can pass a fake.
type Service interface {
	CreateAuthor(ctx context.Context, name string) (article.Author, error)
	CreateArticle(ctx context.Context, in article.CreateArticleInput) (article.Article, error)
	UpdateArticle(ctx context.Context, id string, in article.UpdateArticleInput) (article.Article, error)
	ListArticles(ctx context.Context, q article.ListQuery) (article.Page, error)
}

type Handler struct {
	svc Service
	log *slog.Logger
}

func NewRouter(svc Service, log *slog.Logger) http.Handler {
	h := &Handler{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /authors", h.createAuthor)
	mux.HandleFunc("POST /articles", h.createArticle)
	mux.HandleFunc("GET /articles", h.listArticles)
	mux.HandleFunc("PATCH /articles/{id}", h.updateArticle)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return withRecover(log, withRequestLog(log, mux))
}

func (h *Handler) createAuthor(w http.ResponseWriter, r *http.Request) {
	var req createAuthorRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	a, err := h.svc.CreateAuthor(r.Context(), req.Name)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAuthorResponse(a))
}

func (h *Handler) createArticle(w http.ResponseWriter, r *http.Request) {
	var req createArticleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	a, err := h.svc.CreateArticle(r.Context(), article.CreateArticleInput{
		AuthorID: req.AuthorID, Title: req.Title, Body: req.Body,
	})
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, toArticleResponse(a))
}

func (h *Handler) updateArticle(w http.ResponseWriter, r *http.Request) {
	var req updateArticleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	a, err := h.svc.UpdateArticle(r.Context(), r.PathValue("id"), article.UpdateArticleInput{
		Title: req.Title, Body: req.Body,
	})
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toArticleResponse(a))
}

// listArticles: GET /articles?query=&author=&limit=&cursor=
func (h *Handler) listArticles(w http.ResponseWriter, r *http.Request) {
	q, err := parseListQuery(r)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	page, err := h.svc.ListArticles(r.Context(), q)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toListResponse(page))
}

func parseListQuery(r *http.Request) (article.ListQuery, error) {
	v := r.URL.Query()
	q := article.ListQuery{Query: v.Get("query"), AuthorName: v.Get("author")}

	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return q, &article.ValidationError{Fields: []article.FieldError{
				{Field: "limit", Message: "must be between 1 and 100"},
			}}
		}
		q.Limit = n
	}
	if s := v.Get("cursor"); s != "" {
		c, err := article.DecodeCursor(s)
		if err != nil {
			return q, err
		}
		q.After = c
	}
	return q, nil
}
