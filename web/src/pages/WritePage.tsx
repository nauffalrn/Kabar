import { useQuery, useQueryClient } from '@tanstack/react-query'
import { DragEvent, FormEvent, KeyboardEvent, useEffect, useRef, useState } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'
import { api, ApiError, mediaUrl, type Article, type ArticleImage } from '../api'
import { canEdit, useMe } from '../auth'
import { useCategories } from '../components/CategoryNav'
import { Grip, Plus, Trash } from '../components/Icons'

const MAX_IMAGES = 5
const MAX_BYTES = 5 * 1024 * 1024
const ACCEPT = ['image/jpeg', 'image/png', 'image/webp']

// In create mode images live only in the browser until "Terbitkan";
// in edit mode every image change is saved to the API right away.
type Img = { key: string; src: string; caption: string; file?: File; id?: string }

const fromServer = (i: ArticleImage): Img => ({ key: i.id, id: i.id, src: mediaUrl(i.thumb_url), caption: i.caption })

export default function WritePage() {
  const { id } = useParams()
  const editing = !!id
  const { data: me, isPending: meLoading } = useMe()
  const { data: cats = [] } = useCategories()
  const navigate = useNavigate()
  const qc = useQueryClient()

  const existing = useQuery({ queryKey: ['article', id, 'edit'], queryFn: () => api<Article>(`/articles/${id}`), enabled: editing })

  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [category, setCategory] = useState('')
  const [imgs, setImgs] = useState<Img[]>([])
  const [cover, setCover] = useState<string>('')
  const [error, setError] = useState<ApiError | null>(null)
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [dragKey, setDragKey] = useState<string | null>(null)
  const [overKey, setOverKey] = useState<string | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const loaded = useRef(false)

  useEffect(() => {
    const a = existing.data
    if (!a || loaded.current) return
    loaded.current = true
    setTitle(a.title)
    setBody(a.body)
    setCategory(a.category?.slug ?? '')
    const list = (a.images ?? []).map(fromServer)
    setImgs(list)
    setCover(a.images?.find((i) => i.is_cover)?.id ?? '')
  }, [existing.data])

  useEffect(() => {
    if (!category && cats.length && !editing) setCategory(cats[0].slug)
  }, [cats, category, editing])

  if (meLoading) return null
  if (!me) return <Navigate to={`/masuk?next=${encodeURIComponent(editing ? `/artikel/${id}/edit` : '/tulis')}`} replace />
  if (editing && existing.data && !canEdit(me, existing.data.author.id)) {
    return (
      <main className="page-narrow">
        <h1 className="page-title">Tidak bisa mengedit</h1>
        <p style={{ fontSize: 18 }}>Hanya penulis artikel ini atau admin yang boleh mengeditnya.</p>
      </main>
    )
  }

  const fieldErr = (f: string) => error?.fieldMessage(f)
  const run = async (fn: () => Promise<void>) => {
    setBusy(true)
    setError(null)
    try {
      await fn()
    } catch (e) {
      setError(e instanceof ApiError ? e : new ApiError(0, 'network', 'network error'))
    } finally {
      setBusy(false)
    }
  }

  async function addFiles(files: FileList | File[]) {
    const list = Array.from(files)
    const room = MAX_IMAGES - imgs.length
    const valid = list.filter((f) => ACCEPT.includes(f.type) && f.size <= MAX_BYTES).slice(0, room)
    const skipped = list.length - valid.length
    setNotice(skipped ? `${skipped} file dilewati: hanya JPG, PNG, atau WebP maksimal 5 MB, dan paling banyak ${MAX_IMAGES} gambar.` : '')
    if (!valid.length) return

    if (!editing) {
      const added = valid.map((file) => ({ key: crypto.randomUUID(), src: URL.createObjectURL(file), caption: '', file }))
      setImgs((cur) => [...cur, ...added])
      setCover((c) => c || added[0].key)
      return
    }
    await run(async () => {
      for (const file of valid) {
        const form = new FormData()
        form.append('image', file)
        const saved = await api<ArticleImage>(`/articles/${id}/images`, { method: 'POST', form })
        setImgs((cur) => [...cur, fromServer(saved)])
        if (saved.is_cover) setCover(saved.id)
      }
    })
  }

  async function removeImg(img: Img) {
    const rest = imgs.filter((i) => i.key !== img.key)
    const nextCover = cover === img.key ? rest[0]?.key ?? '' : cover
    if (editing) {
      await run(async () => {
        await api(`/images/${img.id}`, { method: 'DELETE' })
        setImgs(rest)
        setCover(nextCover)
      })
      return
    }
    if (img.file) URL.revokeObjectURL(img.src)
    setImgs(rest)
    setCover(nextCover)
  }

  async function chooseCover(img: Img) {
    setCover(img.key)
    if (editing) await run(() => api(`/articles/${id}/images/cover`, { method: 'PUT', json: { image_id: img.id } }))
  }

  async function move(from: number, to: number) {
    if (to < 0 || to >= imgs.length || from === to) return
    const next = [...imgs]
    const [item] = next.splice(from, 1)
    next.splice(to, 0, item)
    setImgs(next)
    setNotice(`Gambar dipindah ke posisi ${to + 1}.`)
    if (editing) await run(() => api(`/articles/${id}/images/order`, { method: 'PUT', json: { image_ids: next.map((i) => i.id) } }))
  }

  function setCaption(key: string, caption: string) {
    setImgs((cur) => cur.map((i) => (i.key === key ? { ...i, caption } : i)))
  }

  async function saveCaption(img: Img) {
    if (editing) await run(() => api(`/images/${img.id}`, { method: 'PATCH', json: { caption: img.caption } }))
  }

  function onGripKey(e: KeyboardEvent, index: number) {
    if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault()
      move(index, index - 1)
    } else if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault()
      move(index, index + 1)
    }
  }

  function onDrop(e: DragEvent, targetIndex: number) {
    e.preventDefault()
    const from = imgs.findIndex((i) => i.key === dragKey)
    setDragKey(null)
    setOverKey(null)
    if (from >= 0) move(from, targetIndex)
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    await run(async () => {
      let articleId = id
      if (editing) {
        await api(`/articles/${id}`, { method: 'PATCH', json: { title, body, category } })
      } else {
        const form = new FormData()
        form.append('title', title)
        form.append('body', body)
        form.append('category', category)
        form.append('cover_index', String(Math.max(0, imgs.findIndex((i) => i.key === cover))))
        for (const img of imgs) {
          form.append('images', img.file!)
          form.append('captions', img.caption)
        }
        const created = await api<Article>('/articles', { method: 'POST', form })
        articleId = created.id
      }
      qc.invalidateQueries({ queryKey: ['articles'] })
      qc.invalidateQueries({ queryKey: ['article', articleId] })
      navigate(`/artikel/${articleId}`)
    })
  }

  const errorCount = error?.fields.length ?? 0

  return (
    <main className="page-narrow">
      <form className="form" onSubmit={submit} noValidate>
        <div>
          <Link to={editing ? `/artikel/${id}` : '/'} style={{ fontSize: 14, fontWeight: 600 }}>
            ← Batal, kembali
          </Link>
          <h1 className="page-title">{editing ? 'Edit artikel' : 'Tulis artikel'}</h1>
        </div>

        {error && (
          <div role="alert" className="notice notice--error">
            <strong style={{ color: 'var(--red-dark)' }}>Belum tersimpan.</strong>{' '}
            {errorCount
              ? `Perbaiki ${errorCount} kolom di bawah. Isi artikel Anda tetap aman.`
              : error.code === 'too_many_images'
                ? 'Satu artikel paling banyak 5 gambar.'
                : 'Terjadi kesalahan. Coba lagi.'}
          </div>
        )}

        <div className="field">
          <div className="field__row">
            <label htmlFor="judul" className="field__label">
              Judul
            </label>
            <span className="field__hint">{title.length} / 300</span>
          </div>
          <input
            id="judul"
            className="input title-input"
            value={title}
            maxLength={300}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Judul yang langsung menjawab: apa yang terjadi?"
            aria-invalid={!!fieldErr('title')}
          />
          {fieldErr('title') && <span className="field__error">Judul wajib diisi.</span>}
        </div>

        <div className="field" style={{ maxWidth: 360 }}>
          <label htmlFor="kategori" className="field__label">
            Kategori
          </label>
          <select id="kategori" className="select" value={category} onChange={(e) => setCategory(e.target.value)}>
            {cats.map((c) => (
              <option key={c.id} value={c.slug}>
                {c.name}
              </option>
            ))}
          </select>
        </div>

        <fieldset style={{ margin: 0, padding: 0, border: 'none', display: 'flex', flexDirection: 'column', gap: 10 }}>
          <legend className="field__label" style={{ padding: 0 }}>
            Gambar
          </legend>
          <div className="field__row" style={{ flexWrap: 'wrap' }}>
            <p id="gambar-help" className="field__hint" style={{ margin: 0 }}>
              Seret gambar untuk mengubah urutan, atau fokus pada pegangan lalu tekan panah kiri/kanan. JPG, PNG, atau WebP, maksimal 5 MB.
            </p>
            <span style={{ fontSize: 13, fontWeight: 700, color: 'var(--tarp-dark)' }}>
              {imgs.length} / {MAX_IMAGES} gambar
            </span>
          </div>
          <div className="images">
            {imgs.map((img, i) => (
              <div
                key={img.key}
                className="img-card"
                draggable
                data-dragging={dragKey === img.key}
                data-over={overKey === img.key && dragKey !== img.key}
                onDragStart={(e) => {
                  e.dataTransfer.effectAllowed = 'move'
                  setDragKey(img.key)
                }}
                onDragOver={(e) => {
                  if (!dragKey) return
                  e.preventDefault()
                  if (overKey !== img.key) setOverKey(img.key)
                }}
                onDrop={(e) => onDrop(e, i)}
                onDragEnd={() => {
                  setDragKey(null)
                  setOverKey(null)
                }}
              >
                <div className="img-card__media">
                  <img src={img.src} alt={img.caption || `Gambar ${i + 1}`} draggable={false} />
                  <span className="img-card__num" aria-hidden="true">
                    {i + 1}
                  </span>
                  <button
                    type="button"
                    className="img-card__grip"
                    aria-label={`Pindahkan gambar ${i + 1}. Gunakan panah kiri atau kanan.`}
                    aria-describedby="gambar-help"
                    onKeyDown={(e) => onGripKey(e, i)}
                  >
                    <Grip />
                  </button>
                  {cover === img.key && <span className="img-card__cover">Sampul</span>}
                </div>
                <div className="img-card__body">
                  <label className="field" style={{ fontSize: 12, fontWeight: 700 }}>
                    Keterangan
                    <input
                      className="input"
                      value={img.caption}
                      maxLength={300}
                      onChange={(e) => setCaption(img.key, e.target.value)}
                      onBlur={() => saveCaption(img)}
                    />
                  </label>
                  <div className="img-card__foot">
                    <label className="radio">
                      <input type="radio" name="sampul" checked={cover === img.key} onChange={() => chooseCover(img)} />
                      Jadikan sampul
                    </label>
                    <button type="button" className="btn btn--danger btn--icon" aria-label={`Hapus gambar ${i + 1}`} disabled={busy} onClick={() => removeImg(img)}>
                      <Trash />
                    </button>
                  </div>
                </div>
              </div>
            ))}
            {imgs.length < MAX_IMAGES && (
              <label
                className="add-tile"
                data-over={overKey === 'add'}
                onDragOver={(e) => {
                  if (dragKey || !e.dataTransfer.types.includes('Files')) return
                  e.preventDefault()
                  setOverKey('add')
                }}
                onDragLeave={() => setOverKey(null)}
                onDrop={(e) => {
                  if (dragKey) return
                  e.preventDefault()
                  setOverKey(null)
                  addFiles(e.dataTransfer.files)
                }}
              >
                <Plus />
                {busy ? 'Mengunggah…' : 'Tambah gambar'}
                <span style={{ fontSize: 13, fontWeight: 400 }}>atau jatuhkan file di sini</span>
                <input
                  ref={fileInput}
                  type="file"
                  multiple
                  accept={ACCEPT.join(',')}
                  className="sr-only"
                  aria-describedby="gambar-help"
                  onChange={(e) => {
                    if (e.target.files) addFiles(e.target.files)
                    e.target.value = ''
                  }}
                />
              </label>
            )}
          </div>
          <p aria-live="polite" className="field__hint" style={{ margin: 0 }}>
            {notice}
            {fieldErr('images') && <span className="field__error"> Ada gambar yang tidak didukung.</span>}
          </p>
        </fieldset>

        <div className="field">
          <div className="field__row">
            <label htmlFor="isi" className="field__label">
              Isi artikel
            </label>
            <span className="field__hint">{body.length.toLocaleString('id-ID')} / 100.000</span>
          </div>
          <textarea id="isi" className="textarea" rows={14} value={body} maxLength={100000} onChange={(e) => setBody(e.target.value)} aria-invalid={!!fieldErr('body')} style={{ fontSize: 18 }} />
          {fieldErr('body') ? (
            <span className="field__error">Isi artikel wajib diisi.</span>
          ) : (
            <span className="field__hint">Boleh bahasa Indonesia, Inggris, atau campuran. Pencarian memahami keduanya.</span>
          )}
        </div>

        <div className="form__footer">
          <button type="submit" className="btn btn--primary" disabled={busy}>
            {busy ? 'Menyimpan…' : editing ? 'Simpan perubahan' : 'Terbitkan'}
          </button>
          <Link to={editing ? `/artikel/${id}` : '/'} className="btn" style={{ border: 'none' }}>
            Batal
          </Link>
        </div>
      </form>
    </main>
  )
}
