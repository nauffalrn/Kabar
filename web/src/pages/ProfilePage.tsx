import { useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router-dom'
import { api, ApiError, type Profile } from '../api'
import { ArticleRow, ErrorNotice, Pager, RowSkeleton } from '../components/ArticleBits'
import { initials, monthYear } from '../format'
import { usePagedArticles } from '../usePagedArticles'
import NotFound from './NotFound'

export default function ProfilePage() {
  const { id = '' } = useParams()
  const profile = useQuery({ queryKey: ['author', id], queryFn: () => api<Profile>(`/authors/${id}`) })
  const feed = usePagedArticles({ author_id: id })

  if (profile.error instanceof ApiError && profile.error.status === 404) return <NotFound />
  const p = profile.data
  const articles = feed.data?.data ?? []

  return (
    <>
      <section className="profile-band">
        <div className="profile-band__row">
          <span className="avatar" aria-hidden="true">
            {p ? initials(p.name) : ''}
          </span>
          <div style={{ flex: '1 1 360px' }}>
            <h1>{p?.name ?? ' '}</h1>
            {p && (
              <p>
                <span>Bergabung {monthYear(p.joined_at)}</span>
                <span>{p.article_count.toLocaleString('id-ID')} artikel</span>
              </p>
            )}
          </div>
        </div>
      </section>
      <main className="article" style={{ paddingTop: 36 }}>
        <h2 className="feed__title" style={{ fontSize: 30 }}>
          Artikel
        </h2>
        {feed.isError && <ErrorNotice error={feed.error} onRetry={() => feed.refetch()} />}
        {feed.isPending && [0, 1].map((i) => <RowSkeleton key={i} />)}
        {feed.isSuccess && articles.length === 0 && <p className="empty">Penulis ini belum menerbitkan artikel.</p>}
        {articles.map((a) => (
          <ArticleRow key={a.id} article={a} showAuthor={false} />
        ))}
        <Pager hasNewer={!feed.isFirstPage} hasOlder={feed.hasOlder} onNewer={feed.newer} onOlder={feed.older} />
      </main>
    </>
  )
}
