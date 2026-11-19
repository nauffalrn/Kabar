const rtf = new Intl.RelativeTimeFormat('id', { numeric: 'auto' })
const dateFmt = new Intl.DateTimeFormat('id-ID', { dateStyle: 'full', timeStyle: 'short' })
const monthFmt = new Intl.DateTimeFormat('id-ID', { month: 'long', year: 'numeric' })

const steps: [Intl.RelativeTimeFormatUnit, number][] = [
  ['minute', 60],
  ['hour', 24],
  ['day', 7],
  ['week', 4.35],
  ['month', 12],
  ['year', Infinity],
]

export function timeAgo(iso: string): string {
  let value = (new Date(iso).getTime() - Date.now()) / 60000
  if (Math.abs(value) < 1) return 'baru saja'
  for (const [unit, size] of steps) {
    if (Math.abs(value) < size) return rtf.format(Math.round(value), unit)
    value /= size
  }
  return rtf.format(Math.round(value), 'year')
}

export const fullDate = (iso: string) => dateFmt.format(new Date(iso))
export const monthYear = (iso: string) => monthFmt.format(new Date(iso))

// Articles count as edited only after a real change, not the insert itself.
export const wasEdited = (created: string, updated: string) =>
  new Date(updated).getTime() - new Date(created).getTime() > 1000

export const initials = (name: string) =>
  name
    .split(/\s+/)
    .slice(0, 2)
    .map((w) => w[0]?.toUpperCase() ?? '')
    .join('')
