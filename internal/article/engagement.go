package article

import (
	"context"
	"fmt"
)

func (s *Service) Like(ctx context.Context, actor *Actor, articleID string) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	return s.d.Likes.Like(ctx, articleID, actor.UserID)
}

func (s *Service) Unlike(ctx context.Context, actor *Actor, articleID string) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	return s.d.Likes.Unlike(ctx, articleID, actor.UserID)
}

func (s *Service) ListComments(ctx context.Context, articleID string, after *Cursor, limit int) (CommentPage, error) {
	limit = pageSize(limit)
	rows, err := s.d.Comments.ListComments(ctx, articleID, after, limit+1)
	if err != nil {
		return CommentPage{}, fmt.Errorf("list comments: %w", err)
	}
	return toCommentPage(rows, limit), nil
}

// ListRecentComments feeds the moderation screen, deleted comments included.
func (s *Service) ListRecentComments(ctx context.Context, actor *Actor, after *Cursor, limit int) (CommentPage, error) {
	if err := requireAdmin(actor); err != nil {
		return CommentPage{}, err
	}
	limit = pageSize(limit)
	rows, err := s.d.Comments.ListRecentComments(ctx, after, limit+1)
	if err != nil {
		return CommentPage{}, fmt.Errorf("list recent comments: %w", err)
	}
	return toCommentPage(rows, limit), nil
}

func (s *Service) CreateComment(ctx context.Context, actor *Actor, articleID, body string) (Comment, error) {
	if err := requireActor(actor); err != nil {
		return Comment{}, err
	}
	body = normalizeText(body)
	if err := validateComment(body); err != nil {
		return Comment{}, err
	}
	return s.d.Comments.CreateComment(ctx, Comment{ID: s.d.NewID(), ArticleID: articleID, UserID: actor.UserID, Body: body})
}

func (s *Service) UpdateComment(ctx context.Context, actor *Actor, id, body string) (Comment, error) {
	if _, err := s.authorizeComment(ctx, actor, id); err != nil {
		return Comment{}, err
	}
	body = normalizeText(body)
	if err := validateComment(body); err != nil {
		return Comment{}, err
	}
	return s.d.Comments.UpdateComment(ctx, id, body)
}

func (s *Service) DeleteComment(ctx context.Context, actor *Actor, id string) error {
	if _, err := s.authorizeComment(ctx, actor, id); err != nil {
		return err
	}
	_, err := s.d.Comments.SetCommentDeleted(ctx, id, true)
	return err
}

func (s *Service) RestoreComment(ctx context.Context, actor *Actor, id string) (Comment, error) {
	if err := requireAdmin(actor); err != nil {
		return Comment{}, err
	}
	return s.d.Comments.SetCommentDeleted(ctx, id, false)
}

// authorizeComment lets the comment's writer or an admin change it.
func (s *Service) authorizeComment(ctx context.Context, actor *Actor, id string) (Comment, error) {
	if err := requireActor(actor); err != nil {
		return Comment{}, err
	}
	c, err := s.d.Comments.GetComment(ctx, id)
	if err != nil {
		return Comment{}, err
	}
	if !actor.Admin && c.UserID != actor.UserID {
		return Comment{}, ErrForbidden
	}
	return c, nil
}

func pageSize(limit int) int {
	if limit <= 0 || limit > MaxPageSize {
		return DefaultPageSize
	}
	return limit
}

func toCommentPage(rows []Comment, limit int) CommentPage {
	if rows == nil {
		rows = []Comment{}
	}
	if len(rows) <= limit {
		return CommentPage{Comments: rows}
	}
	rows = rows[:limit]
	last := rows[len(rows)-1]
	return CommentPage{Comments: rows, NextCursor: &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}}
}
