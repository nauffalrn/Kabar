import { FormEvent } from 'react'
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { useLogout, useMe } from '../auth'
import { initials } from '../format'

export default function Header() {
  const { data: me } = useMe()
  const logout = useLogout()
  const navigate = useNavigate()
  const location = useLocation()
  const [params] = useSearchParams()
  const onFeed = location.pathname === '/'

  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const next = new URLSearchParams()
    for (const key of ['query', 'author'] as const) {
      const v = String(form.get(key) ?? '').trim()
      if (v) next.set(key, v)
    }
    if (onFeed && params.get('category')) next.set('category', params.get('category')!)
    navigate({ pathname: '/', search: next.toString() })
  }

  const next = encodeURIComponent(location.pathname + location.search)

  return (
    <header className="masthead">
      <div className="wrap masthead__row">
        <Link to="/" className="logo">
          KABAR
        </Link>
        {/* key resets the uncontrolled fields when the URL changes */}
        <form role="search" className="search" onSubmit={submit} key={onFeed ? params.toString() : 'other'}>
          <label className="q">
            Cari artikel
            <input className="input" type="search" name="query" placeholder="banjir, startup, reading…" defaultValue={onFeed ? params.get('query') ?? '' : ''} />
          </label>
          <label className="a">
            Nama penulis
            <input className="input" type="text" name="author" placeholder="Semua penulis" defaultValue={onFeed ? params.get('author') ?? '' : ''} />
          </label>
          <button type="submit" className="btn btn--primary">
            Cari
          </button>
        </form>
        <nav aria-label="Akun" className="account">
          <Link to="/tulis" className="btn btn--light">
            Tulis
          </Link>
          {me ? (
            <>
              {me.role === 'admin' && (
                <Link to="/moderasi" className="link-light">
                  Moderasi
                </Link>
              )}
              <Link to={`/penulis/${me.author_id}`} className="account__user link-light" style={{ textDecoration: 'none' }}>
                <span className="avatar" aria-hidden="true">
                  {initials(me.name)}
                </span>
                {me.name}
              </Link>
              <button type="button" className="link-light" onClick={() => logout().then(() => navigate('/'))}>
                Keluar
              </button>
            </>
          ) : (
            <Link to={`/masuk?next=${next}`} className="link-light">
              Masuk
            </Link>
          )}
        </nav>
      </div>
    </header>
  )
}
