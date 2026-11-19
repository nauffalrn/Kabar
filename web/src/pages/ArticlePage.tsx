import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { FormEvent, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { api, ApiError, mediaUrl, query, type Article, type Comment, type Page } from '../api'
import { canEdit, useMe } from '../auth'
import { CategoryBadge, ErrorNotice } from '../components/ArticleBits'
import { Bubble, Heart } from '../components/Icons'
import { fullDate, timeAgo, wasEdited } from '../format'
import NotFound from './NotFound'

export default function ArticlePage() {
  const { id = '' } = useParams()
  const { data: me } = useMe()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const location = useLocation()
  const [photo, setPhoto] = useState(0)
  const loginUrl = `/masuk?next=${encodeURIComponent(location.pathname)}`

  const article = useQuery({ queryKey: ['article', id, me?.id ?? ''], queryFn: () => api<Article>(`/articles/${id}`) })

  // Optimistic like: the counter moves at once and rolls back on failure.
  const like = useMutation({
    mutationFn: (liked: boolean) => api(`/articles/${id}/like`, { method: liked ? 'PUT' : 'DELETE' }),
    onMutate: (liked) => {
      const key = ['article', id, me?.id ?? '']
      const prev = qc.getQueryData<Article>(key)
      if (prev) qc.setQueryData<Article>(key, { ...prev, liked_by_me: liked, like_count: prev.like_count + (liked ? 1 : -1) })
      return { prev, key }
    },
    onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(ctx.key, ctx.prev),
    onSettled: () => qc.invalidateQueries({ queryKey: ['articles'] }),
  })

  const remove = useMutation({
    mutationFn: () => api(`/articles/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['articles'] })
      navigate('/', { replace: true })
    },
  })

  if (article.error instanceof ApiError && article.error.status === 404) return <NotFound />
  if (article.isError) {
    return (
      <main className="article">
        <ErrorNotice error={article.error} onRetry={() => article.refetch()} />
      </main>
    )
  }
  const a = article.data
  if (!a) {
    return (
      <main className="article" aria-busy="true">
        <div className="skeleton" style={{ height: 56, width: '80%' }} />
        <div className="skeleton" style={{ height: 420, marginTop: 28 }} />
      </main>
    )
  }

  const images = a.images ?? []
  const current = images[Math.min(photo, images.length - 1)]
  const liked = !!a.liked_by_me

  return (
    <main className="article">
      <Link to="/" style={{ fontFamily: 'var(--display)', fontSize: 14, fontWeight: 600 }}>
        ← Kembali ke beranda
      </Link>
      <article style={{ marginTop: 24 }}>
        <CategoryBadge category={a.category} link />
        <h1 className="article__title">{a.title}</h1>
        <p className="article__meta">
          <span>
            Oleh{' '}
            <Link to={`/penulis/${a.author.id}`} style={{ color: 'var(--ink)', fontWeight: 700 }}>
              {a.author.name}
            </Link>
          </span>
          <span>{fullDate(a.created_at)}</span>
          {wasEdited(a.created_at, a.updated_at) && <span>diperbarui {timeAgo(a.updated_at)}</span>}
        </p>

        {current && (
          <figure className="gallery">
            <div className="gallery__main">
              <img src={mediaUrl(current.url)} alt={current.caption || a.title} />
              {images.length > 1 && (
                <span className="gallery__counter">
                  {images.indexOf(current) + 1} / {images.length}
                </span>
              )}
            </div>
            {current.caption && <figcaption>{current.caption}</figcaption>}
            {images.length > 1 && (
              <div className="gallery__thumbs" role="group" aria-label="Pilih foto">
                {images.map((img, i) => (
                  <button key={img.id} type="button" aria-label={`Foto ${i + 1}`} aria-pressed={img === current} onClick={() => setPhoto(i)}>
                    <img src={mediaUrl(img.thumb_url)} alt="" />
                  </button>
                ))}
              </div>
            )}
          </figure>
        )}

        <div className="article__body">{a.body}</div>
      </article>

      <div className="actions">
        <button
          type="button"
          className="btn like"
          aria-pressed={liked}
          disabled={like.isPending}
          onClick={() => (me ? like.mutate(!liked) : navigate(loginUrl))}
        >
          <Heart size={20} filled={liked} />
          {a.like_count}
          <span className="sr-only">{liked ? 'suka, batalkan' : 'suka'}</span>
        </button>
        <a href="#komentar" className="btn" style={{ borderColor: 'var(--tarp)', color: 'var(--tarp)' }}>
          <Bubble size={20} />
          {a.comment_count}
          <span className="sr-only">komentar</span>
        </a>
        {canEdit(me, a.author.id) && (
          <div className="owner-tools">
            <Link to={`/artikel/${a.id}/edit`} className="btn btn--small">
              Edit artikel
            </Link>
            <button
              type="button"
              className="btn btn--small btn--danger"
              disabled={remove.isPending}
              onClick={() => window.confirm('Hapus artikel ini? Artikel akan hilang dari beranda dan pencarian.') && remove.mutate()}
            >
              Hapus
            </button>
          </div>
        )}
      </div>

      <Comments articleId={a.id} loginUrl={loginUrl} />
    </main>
  )
}

function Comments({ articleId, loginUrl }: { articleId: string; loginUrl: string }) {
  const { data: me } = useMe()
  const qc = useQueryClient()
  const key = ['comments', articleId]

  const list = useInfiniteQuery({
    queryKey: key,
    queryFn: ({ pageParam }) => api<Page<Comment>>(`/articles/${articleId}/comments` + query({ cursor: pageParam, limit: '10' })),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })
  const refresh = () => {
    qc.invalidateQueries({ queryKey: key })
    qc.invalidateQueries({ queryKey: ['article', articleId] })
  }

  const create = useMutation({
    mutationFn: (body: string) => api<Comment>(`/articles/${articleId}/comments`, { method: 'POST', json: { body } }),
    onSuccess: refresh,
  })

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = e.currentTarget
    const body = String(new FormData(form).get('body') ?? '').trim()
    if (!body) return
    create.mutate(body, { onSuccess: () => form.reset() })
  }

  const comments = list.data?.pages.flatMap((p) => p.data) ?? []

  return (
    <section className="comments" aria-labelledby="komentar">
      <h2 id="komentar">Komentar</h2>
      {me ? (
        <form className="comment-form" onSubmit={onSubmit}>
          <label htmlFor="komentar-baru" style={{ fontFamily: 'var(--display)', fontSize: 14, fontWeight: 700 }}>
            Tulis komentar sebagai {me.name}
          </label>
          <textarea id="komentar-baru" name="body" rows={3} maxLength={2000} placeholder="Tanggapan Anda…" className="textarea" />
          {create.isError && <span className="field__error">Komentar gagal dikirim. Coba lagi.</span>}
          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button type="submit" className="btn btn--primary" disabled={create.isPending}>
              {create.isPending ? 'Mengirim…' : 'Kirim'}
            </button>
          </div>
        </form>
      ) : (
        <p className="notice">
          <Link to={loginUrl} style={{ fontWeight: 700 }}>
            Masuk
          </Link>{' '}
          untuk ikut berkomentar.
        </p>
      )}

      {list.isError && <ErrorNotice error={list.error} onRetry={() => list.refetch()} />}
      {list.isSuccess && comments.length === 0 && <p className="empty">Belum ada komentar. Jadilah yang pertama.</p>}
      <ol className="comment-list">
        {comments.map((c) => (
          <CommentItem key={c.id} comment={c} onChange={refresh} />
        ))}
      </ol>
      {list.hasNextPage && (
        <div style={{ marginTop: 20, display: 'flex', justifyContent: 'center' }}>
          <button type="button" className="btn btn--small" onClick={() => list.fetchNextPage()} disabled={list.isFetchingNextPage}>
            Muat komentar lama
          </button>
        </div>
      )}
    </section>
  )
}

function CommentItem({ comment: c, onChange }: { comment: Comment; onChange: () => void }) {
  const { data: me } = useMe()
  const [editing, setEditing] = useState(false)
  const mine = !!me && (me.id === c.user_id || me.role === 'admin')

  const save = useMutation({
    mutationFn: (body: string) => api(`/comments/${c.id}`, { method: 'PATCH', json: { body } }),
    onSuccess: () => {
      setEditing(false)
      onChange()
    },
  })
  const remove = useMutation({ mutationFn: () => api(`/comments/${c.id}`, { method: 'DELETE' }), onSuccess: onChange })

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const body = String(new FormData(e.currentTarget).get('body') ?? '').trim()
    if (body) save.mutate(body)
  }

  return (
    <li className="comment">
      <p className="comment__head">
        <strong>{c.author.name}</strong>
        <span>
          {timeAgo(c.created_at)}
          {c.edited && ' · diedit'}
        </span>
      </p>
      {editing ? (
        <form onSubmit={onSubmit} style={{ marginTop: 10, display: 'flex', flexDirection: 'column', gap: 10 }}>
          <label htmlFor={`edit-${c.id}`} className="sr-only">
            Edit komentar
          </label>
          <textarea id={`edit-${c.id}`} name="body" rows={3} maxLength={2000} defaultValue={c.body} className="textarea" style={{ borderColor: 'var(--tarp)' }} autoFocus />
          <div style={{ display: 'flex', gap: 10 }}>
            <button type="submit" className="btn btn--primary btn--small" disabled={save.isPending}>
              Simpan
            </button>
            <button type="button" className="btn btn--small" onClick={() => setEditing(false)}>
              Batal
            </button>
          </div>
        </form>
      ) : (
        <>
          <p className="comment__body">{c.body}</p>
          {mine && (
            <div style={{ marginTop: 4, display: 'flex', gap: 4 }}>
              <button type="button" className="text-btn" onClick={() => setEditing(true)}>
                Edit
              </button>
              <button type="button" className="text-btn text-btn--danger" disabled={remove.isPending} onClick={() => window.confirm('Hapus komentar ini?') && remove.mutate()}>
                Hapus
              </button>
            </div>
          )}
        </>
      )}
    </li>
  )
}
