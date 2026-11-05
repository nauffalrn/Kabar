// Package article holds the domain model and use cases; it depends only on ports.go.
package article

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxAuthorNameLen = 200
	MaxTitleLen      = 300
	MaxBodyLen       = 100_000
	MaxQueryLen      = 200

	DefaultPageSize = 20
	MaxPageSize     = 100
)

type Author struct {
	ID   string
	Name string
}

type Article struct {
	ID         string
	AuthorID   string
	AuthorName string
	Title      string
	Body       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Version    int64
}

type CreateArticleInput struct {
	AuthorID string
	Title    string
	Body     string
}

// Pointers distinguish "not sent" (nil) from "set".
type UpdateArticleInput struct {
	Title *string
	Body  *string
}

type ListQuery struct {
	Query      string // keyword search over title + body
	AuthorName string // filter by author name (may match several author ids)
	Limit      int
	After      *Cursor
}

// ListParams is ListQuery with the author name resolved to ids.
type ListParams struct {
	Query     string
	AuthorIDs []string // nil = no author filter
	Limit     int
	After     *Cursor
}

type Page struct {
	Articles   []Article
	NextCursor *Cursor
}

func normalizeText(s string) string { return strings.TrimSpace(s) }

// Shared by writes and the name filter so both compare the same form.
func NormalizeAuthorName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

func validateAuthorName(name string) error {
	var v validationErrors
	v.text("name", name, MaxAuthorNameLen)
	return v.err()
}

func (in *CreateArticleInput) normalize() {
	in.AuthorID = normalizeText(in.AuthorID)
	in.Title = normalizeText(in.Title)
	in.Body = normalizeText(in.Body)
}

func (in CreateArticleInput) validate() error {
	var v validationErrors
	v.required("author_id", in.AuthorID)
	v.text("title", in.Title, MaxTitleLen)
	v.text("body", in.Body, MaxBodyLen)
	return v.err()
}

func (in *UpdateArticleInput) normalize() {
	if in.Title != nil {
		t := normalizeText(*in.Title)
		in.Title = &t
	}
	if in.Body != nil {
		b := normalizeText(*in.Body)
		in.Body = &b
	}
}

func (in UpdateArticleInput) validate() error {
	var v validationErrors
	if in.Title == nil && in.Body == nil {
		v.add("body", "at least one of title or body must be provided")
	}
	if in.Title != nil {
		v.text("title", *in.Title, MaxTitleLen)
	}
	if in.Body != nil {
		v.text("body", *in.Body, MaxBodyLen)
	}
	return v.err()
}

func (q *ListQuery) normalize() {
	q.Query = normalizeText(q.Query)
	q.AuthorName = NormalizeAuthorName(q.AuthorName)
	if q.Limit <= 0 {
		q.Limit = DefaultPageSize
	}
}

func (q ListQuery) validate() error {
	var v validationErrors
	if q.Limit > MaxPageSize {
		v.add("limit", "must be between 1 and 100")
	}
	if utf8.RuneCountInString(q.Query) > MaxQueryLen {
		v.add("query", "must be at most 200 characters")
	}
	return v.err()
}
