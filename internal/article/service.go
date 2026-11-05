package article

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type Deps struct {
	Writer  ArticleWriter
	Authors AuthorStore
	Browser Reader // used when there is no keyword (Postgres)
	Search  Reader // used when there is a keyword (Elasticsearch)
	Cache   PageCache
	NewID   func() string
}

type Service struct{ d Deps }

func NewService(d Deps) *Service {
	if d.Cache == nil {
		d.Cache = noCache{}
	}
	return &Service{d: d}
}

func (s *Service) CreateAuthor(ctx context.Context, name string) (Author, error) {
	name = NormalizeAuthorName(name)
	if err := validateAuthorName(name); err != nil {
		return Author{}, err
	}
	a := Author{ID: s.d.NewID(), Name: name}
	if err := s.d.Authors.CreateAuthor(ctx, a); err != nil {
		return Author{}, fmt.Errorf("create author: %w", err)
	}
	return a, nil
}

func (s *Service) CreateArticle(ctx context.Context, in CreateArticleInput) (Article, error) {
	in.normalize()
	if err := in.validate(); err != nil {
		return Article{}, err
	}
	a, err := s.d.Writer.CreateArticle(ctx, Article{
		ID:       s.d.NewID(),
		AuthorID: in.AuthorID,
		Title:    in.Title,
		Body:     in.Body,
	})
	if err != nil {
		return Article{}, fmt.Errorf("create article: %w", err)
	}
	return a, nil
}

func (s *Service) UpdateArticle(ctx context.Context, id string, in UpdateArticleInput) (Article, error) {
	in.normalize()
	if err := in.validate(); err != nil {
		return Article{}, err
	}
	a, err := s.d.Writer.UpdateArticle(ctx, id, in)
	if err != nil {
		return Article{}, fmt.Errorf("update article: %w", err)
	}
	return a, nil
}

// ListArticles returns one page, newest first; identical requests share one backend call.
func (s *Service) ListArticles(ctx context.Context, q ListQuery) (Page, error) {
	q.normalize()
	if err := q.validate(); err != nil {
		return Page{}, err
	}
	return s.d.Cache.GetOrLoad(ctx, q.cacheKey(), func(ctx context.Context) (Page, error) {
		return s.listArticles(ctx, q)
	})
}

func (s *Service) listArticles(ctx context.Context, q ListQuery) (Page, error) {
	params := ListParams{Query: q.Query, After: q.After, Limit: q.Limit + 1} // +1 tells us if a next page exists

	if q.AuthorName != "" {
		ids, err := s.d.Authors.FindAuthorIDsByName(ctx, q.AuthorName)
		if err != nil {
			return Page{}, fmt.Errorf("resolve author name: %w", err)
		}
		if len(ids) == 0 {
			return Page{Articles: []Article{}}, nil // unknown name: no need to hit any reader
		}
		params.AuthorIDs = ids
	}

	rows, err := s.readerFor(q).List(ctx, params)
	if err != nil {
		return Page{}, fmt.Errorf("list articles: %w", err)
	}
	return toPage(rows, q.Limit), nil
}

// readerFor: Postgres for plain browsing, Elasticsearch for keyword search.
func (s *Service) readerFor(q ListQuery) Reader {
	if q.Query != "" {
		return s.d.Search
	}
	return s.d.Browser
}

func toPage(rows []Article, limit int) Page {
	if rows == nil {
		rows = []Article{}
	}
	if len(rows) <= limit {
		return Page{Articles: rows}
	}
	rows = rows[:limit]
	next := CursorOf(rows[len(rows)-1])
	return Page{Articles: rows, NextCursor: &next}
}

// strconv.Quote keeps different inputs from colliding through the separator.
func (q ListQuery) cacheKey() string {
	cursor := ""
	if q.After != nil {
		cursor = q.After.Encode()
	}
	return "q=" + strconv.Quote(strings.ToLower(q.Query)) +
		"|a=" + strconv.Quote(strings.ToLower(q.AuthorName)) +
		"|l=" + strconv.Itoa(q.Limit) +
		"|c=" + cursor
}

type noCache struct{}

func (noCache) GetOrLoad(ctx context.Context, _ string, load func(context.Context) (Page, error)) (Page, error) {
	return load(ctx)
}
