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
	MaxCommentLen    = 2_000
	MaxCaptionLen    = 300

	MaxImagesPerArticle = 5
	MaxImageBytes       = 5 << 20

	DefaultPageSize = 20
	MaxPageSize     = 100
)

type Author struct {
	ID   string
	Name string
}

type AuthorProfile struct {
	Author
	JoinedAt     time.Time
	ArticleCount int64
}

type Category struct {
	ID   string
	Slug string
	Name string
}

type Image struct {
	ID        string
	ArticleID string
	Position  int
	IsCover   bool
	Key       string
	Caption   string
}

type Article struct {
	ID         string
	AuthorID   string
	AuthorName string
	CategoryID string
	Category   *Category
	Title      string
	Body       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Version    int64
	Deleted    bool

	LikeCount    int64
	CommentCount int64
	ImageCount   int
	Cover        *Image
	Images       []Image // only filled on the detail view
	LikedByMe    bool
}

type Comment struct {
	ID           string
	ArticleID    string
	ArticleTitle string // filled for the moderation list
	UserID       string
	AuthorID     string
	AuthorName   string
	Body         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Deleted      bool
}

type CommentPage struct {
	Comments   []Comment
	NextCursor *Cursor
}

// NewImage is an uploaded file before processing.
type NewImage struct {
	Data    []byte
	Caption string
}

type CreateArticleInput struct {
	Title      string
	Body       string
	Category   string // slug
	Images     []NewImage
	CoverIndex int
}

// Pointers distinguish "not sent" (nil) from "set".
type UpdateArticleInput struct {
	Title    *string
	Body     *string
	Category *string // slug
}

// ArticleChanges is an UpdateArticleInput with the category resolved to an id.
type ArticleChanges struct {
	Title      *string
	Body       *string
	CategoryID *string
}

type ListQuery struct {
	Query      string // keyword search over title + body
	AuthorName string // filter by author name (may match several author ids)
	AuthorID   string // filter by one exact author, used by the profile page
	Category   string // slug
	Limit      int
	After      *Cursor
}

// ListParams is ListQuery with names and slugs resolved to ids.
type ListParams struct {
	Query      string
	AuthorIDs  []string // nil = no author filter
	CategoryID string
	Limit      int
	After      *Cursor
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

func ValidateAuthorName(name string) error {
	var v validationErrors
	v.text("name", name, MaxAuthorNameLen)
	return v.err()
}

func (in *CreateArticleInput) normalize() {
	in.Title = normalizeText(in.Title)
	in.Body = normalizeText(in.Body)
	in.Category = normalizeText(in.Category)
	for i := range in.Images {
		in.Images[i].Caption = normalizeText(in.Images[i].Caption)
	}
}

func (in CreateArticleInput) validate() error {
	var v validationErrors
	v.text("title", in.Title, MaxTitleLen)
	v.text("body", in.Body, MaxBodyLen)
	v.required("category", in.Category)
	if len(in.Images) > MaxImagesPerArticle {
		v.add("images", "at most 5 images")
	}
	if len(in.Images) > 0 && (in.CoverIndex < 0 || in.CoverIndex >= len(in.Images)) {
		v.add("cover_index", "must point to one of the images")
	}
	for _, img := range in.Images {
		if utf8.RuneCountInString(img.Caption) > MaxCaptionLen {
			v.add("captions", "must be at most 300 characters")
			break
		}
	}
	return v.err()
}

func (in *UpdateArticleInput) normalize() {
	for _, p := range []*string{in.Title, in.Body, in.Category} {
		if p != nil {
			*p = normalizeText(*p)
		}
	}
}

func (in UpdateArticleInput) validate() error {
	var v validationErrors
	if in.Title == nil && in.Body == nil && in.Category == nil {
		v.add("body", "at least one of title, body or category must be provided")
	}
	if in.Title != nil {
		v.text("title", *in.Title, MaxTitleLen)
	}
	if in.Body != nil {
		v.text("body", *in.Body, MaxBodyLen)
	}
	if in.Category != nil {
		v.required("category", *in.Category)
	}
	return v.err()
}

func (q *ListQuery) normalize() {
	q.Query = normalizeText(q.Query)
	q.AuthorName = NormalizeAuthorName(q.AuthorName)
	q.AuthorID = normalizeText(q.AuthorID)
	q.Category = normalizeText(q.Category)
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

func validateComment(body string) error {
	var v validationErrors
	v.text("body", body, MaxCommentLen)
	return v.err()
}

func validateCaption(caption string) error {
	var v validationErrors
	if utf8.RuneCountInString(caption) > MaxCaptionLen {
		v.add("caption", "must be at most 300 characters")
	}
	return v.err()
}
