import { useMutation, useQueryClient } from '@tanstack/react-query'
import { FormEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, ApiError, type User } from '../api'

type Mode = 'login' | 'register'

export default function AuthPage({ mode }: { mode: Mode }) {
  const [params] = useSearchParams()
  const next = params.get('next') ?? '/'
  const safeNext = next.startsWith('/') && !next.startsWith('//') ? next : '/'
  const navigate = useNavigate()
  const qc = useQueryClient()

  const submit = useMutation({
    mutationFn: (body: Record<string, string>) =>
      api<User>(mode === 'login' ? '/auth/login' : '/auth/register', { method: 'POST', json: body }),
    onSuccess: (user) => {
      qc.setQueryData(['me'], user)
      qc.invalidateQueries({ queryKey: ['articles'] })
      navigate(safeNext, { replace: true })
    },
  })

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const body: Record<string, string> = { email: String(f.get('email')), password: String(f.get('password')) }
    if (mode === 'register') body.name = String(f.get('name'))
    submit.mutate(body)
  }

  const err = submit.error instanceof ApiError ? submit.error : null
  const fieldErr = (name: string) => err?.fieldMessage(name)
  const suffix = params.get('next') ? `?next=${encodeURIComponent(next)}` : ''

  return (
    <main className="auth">
      <section className="auth__intro">
        <h1>Punya kabar? Jepit di lapak.</h1>
        <p>Membaca selalu gratis tanpa akun. Akun dibutuhkan untuk menulis artikel, berkomentar, dan menyukai artikel.</p>
      </section>
      <section className="auth__card" aria-label="Formulir akun">
        <nav className="tabs" aria-label="Pilih formulir">
          <Link to={`/masuk${suffix}`} aria-current={mode === 'login' ? 'page' : undefined}>
            Masuk
          </Link>
          <Link to={`/daftar${suffix}`} aria-current={mode === 'register' ? 'page' : undefined}>
            Daftar
          </Link>
        </nav>
        <form className="form" onSubmit={onSubmit} noValidate key={mode}>
          {err && !err.fields.length && (
            <p role="alert" className="notice notice--error" style={{ margin: 0 }}>
              {err.code === 'invalid_credentials' && 'Email atau kata sandi salah.'}
              {err.code === 'email_taken' && 'Email ini sudah terdaftar. Silakan masuk.'}
              {err.code === 'user_blocked' && 'Akun ini diblokir oleh admin.'}
              {!['invalid_credentials', 'email_taken', 'user_blocked'].includes(err.code) && 'Terjadi kesalahan. Coba lagi.'}
            </p>
          )}
          {mode === 'register' && (
            <div className="field">
              <label htmlFor="name" className="field__label">
                Nama tampilan
              </label>
              <input id="name" name="name" className="input" autoComplete="name" required aria-invalid={!!fieldErr('name')} aria-describedby="name-help" />
              <span id="name-help" className={fieldErr('name') ? 'field__error' : 'field__hint'}>
                {fieldErr('name') ? 'Nama wajib diisi.' : 'Boleh sama dengan penulis lain.'}
              </span>
            </div>
          )}
          <div className="field">
            <label htmlFor="email" className="field__label">
              Email
            </label>
            <input id="email" name="email" type="email" className="input" autoComplete="email" required aria-invalid={!!fieldErr('email')} />
            {fieldErr('email') && <span className="field__error">Masukkan alamat email yang valid.</span>}
          </div>
          <div className="field">
            <label htmlFor="password" className="field__label">
              Kata sandi
            </label>
            <input
              id="password"
              name="password"
              type="password"
              className="input"
              autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
              required
              minLength={8}
              aria-invalid={!!fieldErr('password')}
              aria-describedby="password-help"
            />
            <span id="password-help" className={fieldErr('password') ? 'field__error' : 'field__hint'}>
              {mode === 'register' || fieldErr('password') ? 'Minimal 8 karakter.' : ''}
            </span>
          </div>
          <button type="submit" className="btn btn--primary" disabled={submit.isPending}>
            {submit.isPending ? 'Memproses…' : mode === 'login' ? 'Masuk' : 'Buat akun'}
          </button>
        </form>
      </section>
    </main>
  )
}
