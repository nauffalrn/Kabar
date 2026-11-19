package article

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Deps struct {
	Writer     ArticleWriter
	Store      ArticleStore
	Authors    AuthorStore
	Categories CategoryStore
	Browser    Reader // used when there is no keyword (Postgres)
	Search     Reader // used when there is a keyword (Elasticsearch)
	Images     ImageStore
	Likes      LikeStore
	Comments   CommentStore
	Processor  ImageProcessor
	Objects    ObjectStore
	Cache      PageCache
	NewID      func() string
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
	if err := ValidateAuthorName(name); err != nil {
		return Author{}, err
	}
	a := Author{ID: s.d.NewID(), Name: name}
	if err := s.d.Authors.CreateAuthor(ctx, a); err != nil {
		return Author{}, fmt.Errorf("create author: %w", err)
	}
	return a, nil
}

func (s *Service) ListCategories(ctx context.Context) ([]Category, error) {
	return s.d.Categories.ListCategories(ctx)
}

func (s *Service) GetAuthorProfile(ctx context.Context, id string) (AuthorProfile, error) {
	return s.d.Authors.GetProfile(ctx, id)
}

func (s *Service) GetArticle(ctx context.Context, id string, viewer *Actor) (Article, error) {
	userID := ""
	if viewer != nil {
		userID = viewer.UserID
	}
	return s.d.Store.GetArticle(ctx, id, userID)
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

	switch {
	case q.AuthorID != "":
		params.AuthorIDs = []string{q.AuthorID}
	case q.AuthorName != "":
		ids, err := s.d.Authors.FindAuthorIDsByName(ctx, q.AuthorName)
		if err != nil {
			return Page{}, fmt.Errorf("resolve author name: %w", err)
		}
		if len(ids) == 0 {
			return Page{Articles: []Article{}}, nil // unknown name: no need to hit any reader
		}
		params.AuthorIDs = ids
	}

	if q.Category != "" {
		c, err := s.d.Categories.FindCategoryBySlug(ctx, q.Category)
		if errors.Is(err, ErrCategoryNotFound) {
			return Page{}, NewValidationError("category", "unknown category")
		}
		if err != nil {
			return Page{}, fmt.Errorf("resolve category: %w", err)
		}
		params.CategoryID = c.ID
	}

	rows, err := s.readerFor(q).List(ctx, params)
	if err != nil {
		return Page{}, fmt.Errorf("list articles: %w", err)
	}
	page := toPage(rows, q.Limit)
	if s.d.Store != nil && len(page.Articles) > 0 {
		enriched, err := s.d.Store.Enrich(ctx, page.Articles)
		if err != nil {
			return Page{}, fmt.Errorf("enrich articles: %w", err)
		}
		page.Articles = withoutDeleted(enriched)
	}
	return page, nil
}

// ListFeatured feeds the illustrated strip on the home page; it shares the
// page cache, so a burst on the home page still costs one query.
func (s *Service) ListFeatured(ctx context.Context, limit int) ([]Article, error) {
	limit = pageSize(limit)
	page, err := s.d.Cache.GetOrLoad(ctx, "featured|l="+strconv.Itoa(limit), func(ctx context.Context) (Page, error) {
		rows, err := s.d.Store.ListIllustrated(ctx, limit)
		if err != nil {
			return Page{}, fmt.Errorf("list featured: %w", err)
		}
		if len(rows) == 0 {
			return Page{Articles: []Article{}}, nil
		}
		rows, err = s.d.Store.Enrich(ctx, rows)
		if err != nil {
			return Page{}, fmt.Errorf("enrich featured: %w", err)
		}
		return Page{Articles: rows}, nil
	})
	return page.Articles, err
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

// Search may briefly return an article that was just deleted in Postgres.
func withoutDeleted(rows []Article) []Article {
	out := rows[:0]
	for _, a := range rows {
		if !a.Deleted {
			out = append(out, a)
		}
	}
	return out
}

// strconv.Quote keeps different inputs from colliding through the separator.
func (q ListQuery) cacheKey() string {
	cursor := ""
	if q.After != nil {
		cursor = q.After.Encode()
	}
	return "q=" + strconv.Quote(strings.ToLower(q.Query)) +
		"|a=" + strconv.Quote(strings.ToLower(q.AuthorName)) +
		"|ai=" + strconv.Quote(q.AuthorID) +
		"|c=" + strconv.Quote(q.Category) +
		"|l=" + strconv.Itoa(q.Limit) +
		"|cur=" + cursor
}

type noCache struct{}

func (noCache) GetOrLoad(ctx context.Context, _ string, load func(context.Context) (Page, error)) (Page, error) {
	return load(ctx)
}
