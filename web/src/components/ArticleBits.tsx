import { Link } from 'react-router-dom'
import { ApiError, mediaUrl, type Article, type Category } from '../api'
import { timeAgo, wasEdited } from '../format'
import { Bubble, Camera, Heart } from './Icons'

export function CategoryBadge({ category, link = false }: { category: Category | null; link?: boolean }) {
  if (!category) return null
  if (link) {
    return (
      <Link to={`/?category=${category.slug}`} className="badge" data-cat={category.slug}>
        {category.name}
      </Link>
    )
  }
  return (
    <span className="badge" data-cat={category.slug}>
      {category.name}
    </span>
  )
}

export function Stats({ likes, comments }: { likes: number; comments: number }) {
  return (
    <>
      <span className="stat">
        <Heart />
        {likes}
        <span className="sr-only">suka</span>
      </span>
      <span className="stat">
        <Bubble />
        {comments}
        <span className="sr-only">komentar</span>
      </span>
    </>
  )
}

export function ArticleRow({ article, showAuthor = true }: { article: Article; showAuthor?: boolean }) {
  const href = `/artikel/${article.id}`
  return (
    <article className="row">
      <div className="row__text">
        <CategoryBadge category={article.category} />
        <h3 className="row__title">
          <Link to={href}>{article.title}</Link>
        </h3>
        <p className="row__excerpt">{article.excerpt}</p>
        <div className="meta">
          {showAuthor && <Link to={`/penulis/${article.author.id}`}>{article.author.name}</Link>}
          <span>{timeAgo(article.created_at)}</span>
          {wasEdited(article.created_at, article.updated_at) && <span>diperbarui</span>}
          <Stats likes={article.like_count} comments={article.comment_count} />
        </div>
      </div>
      {article.cover && (
        <Link to={href} className="row__thumb" tabIndex={-1} aria-hidden="true">
          <img src={mediaUrl(article.cover.thumb_url)} alt="" loading="lazy" width={260} height={170} />
          {article.image_count > 1 && (
            <span className="photo-count">
              <Camera />
              {article.image_count}
            </span>
          )}
        </Link>
      )}
    </article>
  )
}

export function Pager({ hasNewer, hasOlder, onNewer, onOlder }: { hasNewer: boolean; hasOlder: boolean; onNewer: () => void; onOlder: () => void }) {
  if (!hasNewer && !hasOlder) return null
  return (
    <nav aria-label="Halaman" className="pager">
      <button type="button" className="btn" disabled={!hasNewer} onClick={onNewer}>
        ← Lebih baru
      </button>
      <button type="button" className="btn btn--blue" disabled={!hasOlder} onClick={onOlder}>
        Lebih lama →
      </button>
    </nav>
  )
}

export function RowSkeleton() {
  return (
    <div className="row" aria-hidden="true">
      <div className="row__text">
        <div className="skeleton" style={{ height: 18, width: 90 }} />
        <div className="skeleton" style={{ height: 28, marginTop: 12, width: '80%' }} />
        <div className="skeleton" style={{ height: 16, marginTop: 12 }} />
        <div className="skeleton" style={{ height: 16, marginTop: 8, width: '60%' }} />
      </div>
      <div className="row__thumb skeleton" style={{ height: 170 }} />
    </div>
  )
}

export function ErrorNotice({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const busy = error instanceof ApiError && error.status === 503
  return (
    <div role="alert" className="notice notice--error" style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'center', justifyContent: 'space-between' }}>
      <span>{busy ? 'Layanan sedang sibuk. Coba lagi sebentar lagi.' : 'Gagal memuat data. Periksa koneksi Anda lalu coba lagi.'}</span>
      {onRetry && (
        <button type="button" className="btn btn--small" onClick={onRetry}>
          Coba lagi
        </button>
      )}
    </div>
  )
}
