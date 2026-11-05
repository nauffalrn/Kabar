package article

import "context"

// Small consumer-owned interfaces, implemented in internal/postgres and internal/elasticsearch.

// ArticleWriter must enqueue a search-index event in the same transaction.
type ArticleWriter interface {
	CreateArticle(ctx context.Context, a Article) (Article, error)
	UpdateArticle(ctx context.Context, id string, in UpdateArticleInput) (Article, error)
}

// AuthorStore also resolves the (non-unique) name filter.
type AuthorStore interface {
	CreateAuthor(ctx context.Context, a Author) error
	FindAuthorIDsByName(ctx context.Context, name string) ([]string, error)
}

// Reader returns up to p.Limit articles, newest first, strictly after p.After.
type Reader interface {
	List(ctx context.Context, p ListParams) ([]Article, error)
}

// PageCache de-duplicates and briefly caches identical list requests.
type PageCache interface {
	GetOrLoad(ctx context.Context, key string, load func(context.Context) (Page, error)) (Page, error)
}
