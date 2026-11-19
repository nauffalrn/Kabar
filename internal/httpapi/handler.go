// Package httpapi exposes the article service as a REST API.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"article-service/internal/account"
	"article-service/internal/article"
	"article-service/internal/storage"
)

// ArticleService and AccountService are declared on the consumer side so tests can pass fakes.
type ArticleService interface {
	ListCategories(ctx context.Context) ([]article.Category, error)
	ListArticles(ctx context.Context, q article.ListQuery) (article.Page, error)
	ListFeatured(ctx context.Context, limit int) ([]article.Article, error)
	GetArticle(ctx context.Context, id string, viewer *article.Actor) (article.Article, error)
	GetAuthorProfile(ctx context.Context, id string) (article.AuthorProfile, error)
	CreateArticle(ctx context.Context, actor *article.Actor, in article.CreateArticleInput) (article.Article, error)
	UpdateArticle(ctx context.Context, actor *article.Actor, id string, in article.UpdateArticleInput) (article.Article, error)
	DeleteArticle(ctx context.Context, actor *article.Actor, id string) error
	AddImage(ctx context.Context, actor *article.Actor, articleID string, up article.NewImage) (article.Image, error)
	UpdateImageCaption(ctx context.Context, actor *article.Actor, imageID, caption string) (article.Image, error)
	DeleteImage(ctx context.Context, actor *article.Actor, imageID string) error
	ReorderImages(ctx context.Context, actor *article.Actor, articleID string, imageIDs []string) error
	SetCover(ctx context.Context, actor *article.Actor, articleID, imageID string) error
	Like(ctx context.Context, actor *article.Actor, articleID string) error
	Unlike(ctx context.Context, actor *article.Actor, articleID string) error
	ListComments(ctx context.Context, articleID string, after *article.Cursor, limit int) (article.CommentPage, error)
	ListRecentComments(ctx context.Context, actor *article.Actor, after *article.Cursor, limit int) (article.CommentPage, error)
	CreateComment(ctx context.Context, actor *article.Actor, articleID, body string) (article.Comment, error)
	UpdateComment(ctx context.Context, actor *article.Actor, id, body string) (article.Comment, error)
	DeleteComment(ctx context.Context, actor *article.Actor, id string) error
	RestoreComment(ctx context.Context, actor *article.Actor, id string) (article.Comment, error)
}

type AccountService interface {
	Register(ctx context.Context, name, email, password string) (account.User, string, error)
	Login(ctx context.Context, email, password string) (account.User, string, error)
	Authenticate(ctx context.Context, token string) (account.User, error)
	Logout(ctx context.Context, token string) error
	SearchUsers(ctx context.Context, actor *article.Actor, query string) ([]account.UserSummary, error)
	SetBlocked(ctx context.Context, actor *article.Actor, userID string, blocked bool) error
}

type MediaStore interface {
	Get(ctx context.Context, key string) (storage.Object, error)
}

type Deps struct {
	Articles      ArticleService
	Accounts      AccountService
	Media         MediaStore
	Log           *slog.Logger
	SecureCookies bool // true behind HTTPS
}

type Handler struct {
	Deps
	log *slog.Logger
}

func NewRouter(d Deps) http.Handler {
	h := &Handler{Deps: d, log: d.Log}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /auth/register", h.register)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("GET /auth/me", h.me)

	mux.HandleFunc("GET /categories", h.listCategories)
	mux.HandleFunc("GET /authors/{id}", h.getAuthor)

	mux.HandleFunc("GET /articles", h.listArticles)
	mux.HandleFunc("GET /articles/featured", h.listFeatured)
	mux.HandleFunc("POST /articles", h.createArticle)
	mux.HandleFunc("GET /articles/{id}", h.getArticle)
	mux.HandleFunc("PATCH /articles/{id}", h.updateArticle)
	mux.HandleFunc("DELETE /articles/{id}", h.deleteArticle)

	mux.HandleFunc("POST /articles/{id}/images", h.addImage)
	mux.HandleFunc("PUT /articles/{id}/images/order", h.reorderImages)
	mux.HandleFunc("PUT /articles/{id}/images/cover", h.setCover)
	mux.HandleFunc("PATCH /images/{id}", h.updateImage)
	mux.HandleFunc("DELETE /images/{id}", h.deleteImage)

	mux.HandleFunc("PUT /articles/{id}/like", h.like)
	mux.HandleFunc("DELETE /articles/{id}/like", h.unlike)

	mux.HandleFunc("GET /articles/{id}/comments", h.listComments)
	mux.HandleFunc("POST /articles/{id}/comments", h.createComment)
	mux.HandleFunc("PATCH /comments/{id}", h.updateComment)
	mux.HandleFunc("DELETE /comments/{id}", h.deleteComment)

	mux.HandleFunc("GET /admin/users", h.adminUsers)
	mux.HandleFunc("POST /admin/users/{id}/block", h.blockUser)
	mux.HandleFunc("DELETE /admin/users/{id}/block", h.unblockUser)
	mux.HandleFunc("GET /admin/comments", h.adminComments)
	mux.HandleFunc("POST /admin/comments/{id}/restore", h.restoreComment)

	mux.HandleFunc("GET /media/{key...}", h.media)

	return withRecover(d.Log, withRequestLog(d.Log, withOriginCheck(h.withSession(mux))))
}
