import { useQuery } from '@tanstack/react-query'
import { useLayoutEffect, useRef, useState, type CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import { api, mediaUrl, type Article, type Page } from '../api'
import { timeAgo } from '../format'
import { CategoryBadge } from './ArticleBits'
import { Peg } from './Icons'

const tilt = [-1.6, 0.9, -0.5, 1.2, -1.1, 0.7, -1.3]
const tones = ['red', 'yellow', 'green', 'yellow', 'red', 'green', 'yellow'] as const

// The rope is the curve y = 4 + 4·sag·t(1−t) across the whole band (t = 0…1).
const SAG = 22

// ropeY gives each sheet the rope's height at the sheet's centre, so the peg
// always bites the rope whatever the screen width.
function useRopeDrops(count: number) {
  const track = useRef<HTMLDivElement>(null)
  const slots = useRef<(HTMLDivElement | null)[]>([])
  const [drops, setDrops] = useState<number[]>([])

  useLayoutEffect(() => {
    const el = track.current
    if (!el) return
    const measure = () => {
      const box = el.getBoundingClientRect()
      setDrops(
        slots.current.slice(0, count).map((s) => {
          if (!s) return 0
          const r = s.getBoundingClientRect()
          const t = Math.min(1, Math.max(0, (r.left + r.width / 2 - box.left) / box.width))
          return 4 + 4 * SAG * t * (1 - t)
        }),
      )
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [count])

  return { track, slots, drops }
}

export default function Terkini() {
  const { data } = useQuery({ queryKey: ['articles', 'featured'], queryFn: () => api<Page<Article>>('/articles/featured?limit=7') })
  const items = data?.data ?? []
  const { track, slots, drops } = useRopeDrops(items.length)
  if (items.length === 0) return null

  return (
    <section aria-labelledby="terkini-h" className="rope-band">
      <h2 id="terkini-h" className="sr-only">
        Terkini
      </h2>
      <div className="rope-track" ref={track}>
        <svg className="rope__line" aria-hidden="true" viewBox="0 0 1000 40" preserveAspectRatio="none">
          <path d={`M0 4 Q500 ${4 + 2 * SAG} 1000 4`} fill="none" stroke="#5b4a36" strokeWidth="2.4" vectorEffect="non-scaling-stroke" />
          <path d={`M0 5.5 Q500 ${5.5 + 2 * SAG} 1000 5.5`} fill="none" stroke="rgba(22,22,22,0.12)" strokeWidth="2" vectorEffect="non-scaling-stroke" />
        </svg>
        <div className="wrap rope__grid">
          {items.map((a, i) => (
            <div key={a.id} ref={(el) => (slots.current[i] = el)} className="rope__slot" style={{ paddingTop: (drops[i] ?? 4) + 4, visibility: drops.length ? 'visible' : 'hidden' }}>
              <Link to={`/artikel/${a.id}`} className="sheet" style={{ '--r': `${tilt[i % tilt.length]}deg`, animationDelay: `${i * 90}ms` } as CSSProperties}>
                <Peg tone={tones[i % tones.length]} />
                {a.cover && <img src={mediaUrl(a.cover.thumb_url)} alt={a.cover.caption || ''} loading="lazy" />}
                <span style={{ display: 'block', marginTop: a.cover ? 8 : 0 }}>
                  <CategoryBadge category={a.category} />
                </span>
                <span className="sheet__title">{a.title}</span>
                <span className="sheet__time">{timeAgo(a.created_at)}</span>
              </Link>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
