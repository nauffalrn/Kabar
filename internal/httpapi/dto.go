package httpapi

import (
	"time"

	"article-service/internal/article"
)

// Request/response shapes are kept separate from domain types.

type createAuthorRequest struct {
	Name string `json:"name"`
}

type createArticleRequest struct {
	AuthorID string `json:"author_id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
}

type updateArticleRequest struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
}

type authorResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type articleResponse struct {
	ID        string         `json:"id"`
	Author    authorResponse `json:"author"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type listResponse struct {
	Data       []articleResponse `json:"data"`
	NextCursor *string           `json:"next_cursor"`
}

func toAuthorResponse(a article.Author) authorResponse {
	return authorResponse{ID: a.ID, Name: a.Name}
}

func toArticleResponse(a article.Article) articleResponse {
	return articleResponse{
		ID:        a.ID,
		Author:    authorResponse{ID: a.AuthorID, Name: a.AuthorName},
		Title:     a.Title,
		Body:      a.Body,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

func toListResponse(p article.Page) listResponse {
	data := make([]articleResponse, len(p.Articles))
	for i, a := range p.Articles {
		data[i] = toArticleResponse(a)
	}
	resp := listResponse{Data: data}
	if p.NextCursor != nil {
		c := p.NextCursor.Encode()
		resp.NextCursor = &c
	}
	return resp
}
