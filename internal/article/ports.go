package article

import "context"

// Small consumer-owned interfaces, implemented in internal/postgres,
// internal/elasticsearch, internal/media and internal/storage.

// ArticleWriter must enqueue a search-index event in the same transaction.
type ArticleWriter interface {
	CreateArticle(ctx context.Context, a Article, images []Image) (Article, error)
	UpdateArticle(ctx context.Context, id string, ch ArticleChanges) (Article, error)
	DeleteArticle(ctx context.Context, id string) error
}

// ArticleStore reads single articles and adds the fields search results lack.
type ArticleStore interface {
	GetArticle(ctx context.Context, id, viewerUserID string) (Article, error)
	// Enrich fills category, counters and cover, and marks deleted articles.
	Enrich(ctx context.Context, rows []Article) ([]Article, error)
	// ListIllustrated returns the newest live articles that have a cover image.
	ListIllustrated(ctx context.Context, limit int) ([]Article, error)
}

// AuthorStore also resolves the (non-unique) name filter.
type AuthorStore interface {
	CreateAuthor(ctx context.Context, a Author) error
	FindAuthorIDsByName(ctx context.Context, name string) ([]string, error)
	GetProfile(ctx context.Context, id string) (AuthorProfile, error)
}

type CategoryStore interface {
	ListCategories(ctx context.Context) ([]Category, error)
	FindCategoryBySlug(ctx context.Context, slug string) (Category, error)
}

// Reader returns up to p.Limit articles, newest first, strictly after p.After.
type Reader interface {
	List(ctx context.Context, p ListParams) ([]Article, error)
}

type ImageStore interface {
	// AddImage refuses with ErrTooManyImages once the article holds max images.
	AddImage(ctx context.Context, img Image, max int) (Image, error)
	GetImage(ctx context.Context, id string) (Image, error)
	UpdateCaption(ctx context.Context, id, caption string) (Image, error)
	// DeleteImage moves the cover to the first remaining image when needed.
	DeleteImage(ctx context.Context, id string) (Image, error)
	ReorderImages(ctx context.Context, articleID string, imageIDs []string) error
	SetCover(ctx context.Context, articleID, imageID string) error
}

type LikeStore interface {
	Like(ctx context.Context, articleID, userID string) error
	Unlike(ctx context.Context, articleID, userID string) error
}

type CommentStore interface {
	ListComments(ctx context.Context, articleID string, after *Cursor, limit int) ([]Comment, error)
	ListRecentComments(ctx context.Context, after *Cursor, limit int) ([]Comment, error)
	CreateComment(ctx context.Context, c Comment) (Comment, error)
	GetComment(ctx context.Context, id string) (Comment, error)
	UpdateComment(ctx context.Context, id, body string) (Comment, error)
	SetCommentDeleted(ctx context.Context, id string, deleted bool) (Comment, error)
}

// ProcessedFile is one stored variant of an uploaded image.
type ProcessedFile struct {
	Name        string
	ContentType string
	Data        []byte
}

// ImageProcessor validates an upload and renders its stored variants.
type ImageProcessor interface {
	Process(data []byte) ([]ProcessedFile, error)
}

type ObjectStore interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	Delete(ctx context.Context, key string) error
}

// PageCache de-duplicates and briefly caches identical list requests.
type PageCache interface {
	GetOrLoad(ctx context.Context, key string, load func(context.Context) (Page, error)) (Page, error)
}
