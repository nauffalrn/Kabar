package httpapi

import (
	"time"
	"unicode/utf8"

	"article-service/internal/account"
	"article-service/internal/article"
)

// Request and response shapes are kept separate from domain types.

type createArticleRequest struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Category string `json:"category"`
}

type updateArticleRequest struct {
	Title    *string `json:"title"`
	Body     *string `json:"body"`
	Category *string `json:"category"`
}

type updateImageRequest struct {
	Caption string `json:"caption"`
}

type reorderImagesRequest struct {
	ImageIDs []string `json:"image_ids"`
}

type setCoverRequest struct {
	ImageID string `json:"image_id"`
}

type commentRequest struct {
	Body string `json:"body"`
}

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authorResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type categoryResponse struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type imageResponse struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url"`
	Caption  string `json:"caption"`
	IsCover  bool   `json:"is_cover"`
	Position int    `json:"position"`
}

type articleResponse struct {
	ID           string            `json:"id"`
	Author       authorResponse    `json:"author"`
	Category     *categoryResponse `json:"category"`
	Title        string            `json:"title"`
	Excerpt      string            `json:"excerpt"`
	Body         string            `json:"body"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	LikeCount    int64             `json:"like_count"`
	CommentCount int64             `json:"comment_count"`
	ImageCount   int               `json:"image_count"`
	Cover        *imageResponse    `json:"cover"`
	Images       []imageResponse   `json:"images,omitempty"`
	LikedByMe    *bool             `json:"liked_by_me,omitempty"`
}

type listResponse struct {
	Data       []articleResponse `json:"data"`
	NextCursor *string           `json:"next_cursor"`
}

type commentArticleResponse struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type commentResponse struct {
	ID        string                  `json:"id"`
	ArticleID string                  `json:"article_id"`
	Article   *commentArticleResponse `json:"article,omitempty"`
	UserID    string                  `json:"user_id"`
	Author    authorResponse          `json:"author"`
	Body      string                  `json:"body"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
	Edited    bool                    `json:"edited"`
	Deleted   bool                    `json:"deleted,omitempty"`
}

type commentListResponse struct {
	Data       []commentResponse `json:"data"`
	NextCursor *string           `json:"next_cursor"`
}

type userResponse struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	AuthorID     string    `json:"author_id"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	Blocked      bool      `json:"blocked"`
	ArticleCount *int64    `json:"article_count,omitempty"`
}

type profileResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	JoinedAt     time.Time `json:"joined_at"`
	ArticleCount int64     `json:"article_count"`
}

const excerptRunes = 220

func excerpt(body string) string {
	if utf8.RuneCountInString(body) <= excerptRunes {
		return body
	}
	r := []rune(body)[:excerptRunes]
	return string(r) + "…"
}

func toImageResponse(i article.Image) imageResponse {
	base := "/media/" + i.Key + "/"
	return imageResponse{ID: i.ID, URL: base + "large.jpg", ThumbURL: base + "thumb.jpg", Caption: i.Caption, IsCover: i.IsCover, Position: i.Position}
}

func toArticleResponse(a article.Article) articleResponse {
	out := articleResponse{
		ID:           a.ID,
		Author:       authorResponse{ID: a.AuthorID, Name: a.AuthorName},
		Title:        a.Title,
		Excerpt:      excerpt(a.Body),
		Body:         a.Body,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
		LikeCount:    a.LikeCount,
		CommentCount: a.CommentCount,
		ImageCount:   a.ImageCount,
	}
	if a.Category != nil {
		c := categoryResponse(*a.Category)
		out.Category = &c
	}
	if a.Cover != nil {
		c := toImageResponse(*a.Cover)
		out.Cover = &c
	}
	return out
}

// toArticleDetail adds the gallery, and liked_by_me only when a viewer is signed in.
func toArticleDetail(a article.Article, signedIn bool) articleResponse {
	out := toArticleResponse(a)
	out.Images = make([]imageResponse, len(a.Images))
	for i, img := range a.Images {
		out.Images[i] = toImageResponse(img)
	}
	if signedIn {
		liked := a.LikedByMe
		out.LikedByMe = &liked
	}
	return out
}

func toListResponse(p article.Page) listResponse {
	out := listResponse{Data: make([]articleResponse, len(p.Articles))}
	for i, a := range p.Articles {
		out.Data[i] = toArticleResponse(a)
	}
	if p.NextCursor != nil {
		s := p.NextCursor.Encode()
		out.NextCursor = &s
	}
	return out
}

func toCommentResponse(c article.Comment, withArticle bool) commentResponse {
	out := commentResponse{
		ID: c.ID, ArticleID: c.ArticleID, UserID: c.UserID,
		Author: authorResponse{ID: c.AuthorID, Name: c.AuthorName},
		Body:   c.Body, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		Edited: c.UpdatedAt.Sub(c.CreatedAt) > time.Second, Deleted: c.Deleted,
	}
	if withArticle {
		out.Article = &commentArticleResponse{ID: c.ArticleID, Title: c.ArticleTitle}
	}
	return out
}

func toCommentList(p article.CommentPage, withArticle bool) commentListResponse {
	out := commentListResponse{Data: make([]commentResponse, len(p.Comments))}
	for i, c := range p.Comments {
		out.Data[i] = toCommentResponse(c, withArticle)
	}
	if p.NextCursor != nil {
		s := p.NextCursor.Encode()
		out.NextCursor = &s
	}
	return out
}

func toUserResponse(u account.User) userResponse {
	return userResponse{
		ID: u.ID, Email: u.Email, Name: u.Name, AuthorID: u.AuthorID, Role: u.Role,
		CreatedAt: u.CreatedAt, Blocked: u.DisabledAt != nil,
	}
}
