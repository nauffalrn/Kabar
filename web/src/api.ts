export const API = '/api'

export type Category = { id: string; slug: string; name: string }
export type Author = { id: string; name: string }

export type ArticleImage = {
  id: string
  url: string
  thumb_url: string
  caption: string
  is_cover: boolean
  position: number
}

export type Article = {
  id: string
  author: Author
  category: Category | null
  title: string
  excerpt: string
  body: string
  created_at: string
  updated_at: string
  like_count: number
  comment_count: number
  image_count: number
  cover: ArticleImage | null
  images?: ArticleImage[]
  liked_by_me?: boolean
}

export type Page<T> = { data: T[]; next_cursor: string | null }

export type Comment = {
  id: string
  article_id: string
  article?: { id: string; title: string }
  user_id: string
  author: Author
  body: string
  created_at: string
  updated_at: string
  edited: boolean
  deleted?: boolean
}

export type User = {
  id: string
  email: string
  name: string
  author_id: string
  role: 'user' | 'admin'
  created_at: string
  blocked: boolean
  article_count?: number
}

export type Profile = { id: string; name: string; joined_at: string; article_count: number }

export type FieldError = { field: string; message: string }

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fields: FieldError[] = [],
    public retryAfter?: number,
  ) {
    super(message)
  }

  fieldMessage(field: string): string | undefined {
    return this.fields.find((f) => f.field === field)?.message
  }
}

type Options = { method?: string; json?: unknown; form?: FormData; signal?: AbortSignal }

export async function api<T>(path: string, opts: Options = {}): Promise<T> {
  const headers: Record<string, string> = {}
  let body: BodyInit | undefined
  if (opts.json !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.json)
  } else if (opts.form) {
    body = opts.form
  }
  const res = await fetch(API + path, { method: opts.method ?? 'GET', headers, body, signal: opts.signal, credentials: 'same-origin' })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const e = data?.error ?? {}
    const retry = Number(res.headers.get('Retry-After')) || undefined
    throw new ApiError(res.status, e.code ?? 'unknown', e.message ?? res.statusText, e.fields ?? [], retry)
  }
  return data as T
}

export const mediaUrl = (path: string) => API + path

export function query(params: Record<string, string | undefined | null>): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v) q.set(k, v)
  const s = q.toString()
  return s ? '?' + s : ''
}
