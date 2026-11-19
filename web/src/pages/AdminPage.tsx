import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link, Navigate } from 'react-router-dom'
import { api, query, type Comment, type Page, type User } from '../api'
import { useMe } from '../auth'
import { ErrorNotice } from '../components/ArticleBits'
import { timeAgo } from '../format'

export default function AdminPage() {
  const { data: me, isPending } = useMe()
  const [tab, setTab] = useState<'users' | 'comments'>('users')

  if (isPending) return null
  if (!me) return <Navigate to="/masuk?next=%2Fmoderasi" replace />
  if (me.role !== 'admin') {
    return (
      <main className="page-narrow">
        <h1 className="page-title">Khusus admin</h1>
        <p style={{ fontSize: 18 }}>Halaman moderasi hanya bisa dibuka oleh admin.</p>
      </main>
    )
  }

  return (
    <main className="wrap" style={{ paddingTop: 32, paddingBottom: 80 }}>
      <h1 className="page-title" style={{ marginTop: 0 }}>
        Moderasi
      </h1>
      <p style={{ margin: '6px 0 0', fontFamily: 'var(--display)', fontSize: 15, color: 'var(--muted)' }}>
        Konten yang dihapus disembunyikan dari pembaca, tetapi tetap tersimpan untuk audit.
      </p>
      <div role="tablist" aria-label="Jenis konten" className="admin-tabs">
        <button type="button" role="tab" aria-selected={tab === 'users'} onClick={() => setTab('users')}>
          Pengguna
        </button>
        <button type="button" role="tab" aria-selected={tab === 'comments'} onClick={() => setTab('comments')}>
          Komentar
        </button>
      </div>
      {tab === 'users' ? <Users meId={me.id} /> : <Comments />}
    </main>
  )
}

function useDebounced(value: string, ms: number) {
  const [v, setV] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms)
    return () => clearTimeout(t)
  }, [value, ms])
  return v
}

function Users({ meId }: { meId: string }) {
  const [search, setSearch] = useState('')
  const q = useDebounced(search, 300)
  const qc = useQueryClient()
  const users = useQuery({ queryKey: ['admin-users', q], queryFn: () => api<User[]>('/admin/users' + query({ query: q })) })
  const toggle = useMutation({
    mutationFn: (u: User) => api(`/admin/users/${u.id}/block`, { method: u.blocked ? 'DELETE' : 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin-users'] }),
  })

  return (
    <>
      <label className="field" style={{ marginTop: 18, maxWidth: 420, fontSize: 13, fontWeight: 700 }}>
        Cari nama atau email
        <input className="input" type="search" value={search} onChange={(e) => setSearch(e.target.value)} />
      </label>
      {users.isError && <ErrorNotice error={users.error} onRetry={() => users.refetch()} />}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th scope="col">Nama</th>
              <th scope="col">Email</th>
              <th scope="col">Peran</th>
              <th scope="col">Artikel</th>
              <th scope="col">Status</th>
              <th scope="col">
                <span className="sr-only">Aksi</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {(users.data ?? []).map((u) => (
              <tr key={u.id}>
                <td style={{ fontWeight: 700 }}>
                  <Link to={`/penulis/${u.author_id}`}>{u.name}</Link>
                </td>
                <td>{u.email}</td>
                <td>{u.role}</td>
                <td>{u.article_count ?? 0}</td>
                <td style={u.blocked ? { color: 'var(--red-dark)', fontWeight: 700 } : undefined}>{u.blocked ? 'Diblokir' : 'Aktif'}</td>
                <td style={{ textAlign: 'right' }}>
                  {u.id !== meId && (
                    <button type="button" className={`btn btn--small ${u.blocked ? 'btn--primary' : ''}`} disabled={toggle.isPending} onClick={() => toggle.mutate(u)}>
                      {u.blocked ? 'Buka blokir' : 'Blokir'}
                    </button>
                  )}
                </td>
              </tr>
            ))}
            {users.isSuccess && users.data.length === 0 && (
              <tr>
                <td colSpan={6} style={{ color: 'var(--muted)' }}>
                  Tidak ada pengguna yang cocok.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <p style={{ marginTop: 14, fontFamily: 'var(--display)', fontSize: 13, color: 'var(--muted)' }}>
        Memblokir pengguna langsung mengeluarkannya dari semua perangkat. Artikelnya tetap tampil sampai dihapus.
      </p>
    </>
  )
}

function Comments() {
  const qc = useQueryClient()
  const list = useInfiniteQuery({
    queryKey: ['admin-comments'],
    queryFn: ({ pageParam }) => api<Page<Comment>>('/admin/comments' + query({ cursor: pageParam, limit: '20' })),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })
  const act = useMutation({
    mutationFn: (c: Comment) => (c.deleted ? api(`/admin/comments/${c.id}/restore`, { method: 'POST' }) : api(`/comments/${c.id}`, { method: 'DELETE' })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin-comments'] }),
  })
  const comments = list.data?.pages.flatMap((p) => p.data) ?? []

  return (
    <>
      {list.isError && <ErrorNotice error={list.error} onRetry={() => list.refetch()} />}
      {list.isSuccess && comments.length === 0 && <p className="empty">Belum ada komentar.</p>}
      <ol style={{ listStyle: 'none', margin: '18px 0 0', padding: 0, display: 'flex', flexDirection: 'column', gap: 14 }}>
        {comments.map((c) => (
          <li key={c.id} className="mod-card" data-deleted={!!c.deleted}>
            <div style={{ flex: '1 1 420px', minWidth: 0, fontFamily: 'var(--display)' }}>
              <p style={{ margin: 0, fontSize: 13, color: 'var(--muted)' }}>
                Di artikel <Link to={`/artikel/${c.article_id}`}>{c.article?.title}</Link> · {timeAgo(c.created_at)}
                {c.deleted && ' · dihapus'}
              </p>
              <p style={{ margin: '6px 0 0', fontSize: 14, fontWeight: 700 }}>{c.author.name}</p>
              <p className="comment__body" style={{ fontSize: 16 }}>
                {c.body}
              </p>
            </div>
            <button type="button" className={`btn btn--small ${c.deleted ? '' : 'btn--danger'}`} disabled={act.isPending} onClick={() => act.mutate(c)}>
              {c.deleted ? 'Pulihkan' : 'Hapus komentar'}
            </button>
          </li>
        ))}
      </ol>
      {list.hasNextPage && (
        <div style={{ marginTop: 20, display: 'flex', justifyContent: 'center' }}>
          <button type="button" className="btn btn--small" onClick={() => list.fetchNextPage()}>
            Muat lebih banyak
          </button>
        </div>
      )}
    </>
  )
}
