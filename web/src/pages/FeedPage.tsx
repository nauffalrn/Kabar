import { useSearchParams } from 'react-router-dom'
import { ArticleRow, ErrorNotice, Pager, RowSkeleton } from '../components/ArticleBits'
import CategoryNav, { useCategories } from '../components/CategoryNav'
import Terkini from '../components/Terkini'
import { usePagedArticles } from '../usePagedArticles'

export default function FeedPage() {
  const [params] = useSearchParams()
  const search = params.get('query') ?? ''
  const author = params.get('author') ?? ''
  const category = params.get('category') ?? ''
  const { data: cats = [] } = useCategories()
  const feed = usePagedArticles({ query: search, author, category })

  const catName = cats.find((c) => c.slug === category)?.name
  const filtered = !!(search || author || category)
  let heading = 'Semua artikel'
  if (search) heading = `Hasil pencarian “${search}”`
  else if (author) heading = `Artikel oleh “${author}”`
  else if (catName) heading = `Kategori ${catName}`

  const articles = feed.data?.data ?? []

  return (
    <>
      <CategoryNav active={category} search={search} author={author} />
      {!filtered && feed.isFirstPage && <Terkini />}
      <main className="wrap" style={{ paddingTop: 44, paddingBottom: 72 }}>
        <div className="feed">
          <h1 className="feed__title">{heading}</h1>
          {(search || author) && (catName || author) && (
            <p className="field__hint" style={{ margin: '-8px 0 18px', fontFamily: 'var(--display)' }}>
              {[catName && `Kategori ${catName}`, search && author && `Penulis “${author}”`].filter(Boolean).join(' · ')}
            </p>
          )}

          {feed.isError && <ErrorNotice error={feed.error} onRetry={() => feed.refetch()} />}
          {feed.isPending && [0, 1, 2].map((i) => <RowSkeleton key={i} />)}
          {feed.isSuccess && articles.length === 0 && (
            <p className="empty">{filtered ? 'Tidak ada artikel yang cocok. Coba kata kunci atau kategori lain.' : 'Belum ada artikel.'}</p>
          )}
          <div style={{ opacity: feed.isPlaceholderData ? 0.6 : 1, transition: 'opacity 150ms' }}>
            {articles.map((a) => (
              <ArticleRow key={a.id} article={a} />
            ))}
          </div>

          <Pager hasNewer={!feed.isFirstPage} hasOlder={feed.hasOlder} onNewer={feed.newer} onOlder={feed.older} />
        </div>
      </main>
    </>
  )
}
