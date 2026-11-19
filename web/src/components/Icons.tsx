type P = { size?: number; color?: string; filled?: boolean }

export function Heart({ size = 17, color = '#d7262e', filled = true }: P) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24">
      <path
        d="M12 20.5s-7.2-4.4-9.2-8.8C1.4 8.5 3.4 4.8 6.9 4.8c2.1 0 3.4 1.1 5.1 3 1.7-1.9 3-3 5.1-3 3.5 0 5.5 3.7 4.1 6.9-2 4.4-9.2 8.8-9.2 8.8z"
        fill={filled ? color : 'none'}
        stroke={color}
        strokeWidth="2"
      />
    </svg>
  )
}

export function Bubble({ size = 17, color = '#1f5fa8' }: P) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24">
      <path d="M4 5h16v11H9l-5 4z" fill="none" stroke={color} strokeWidth="2.2" strokeLinejoin="round" />
    </svg>
  )
}

export function Camera({ size = 14 }: P) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24">
      <path d="M3 7h4l2-3h6l2 3h4v13H3z" fill="none" stroke="#161616" strokeWidth="2" />
      <circle cx="12" cy="13" r="3.5" fill="none" stroke="#161616" strokeWidth="2" />
    </svg>
  )
}

export function Trash({ size = 18, color = '#b01e25' }: P) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24">
      <path
        d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v6M14 11v6"
        fill="none"
        stroke={color}
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

export function Grip() {
  return (
    <svg aria-hidden="true" width="14" height="18" viewBox="0 0 14 18">
      {[3, 9, 15].flatMap((y) => [4, 10].map((x) => <circle key={`${x}-${y}`} cx={x} cy={y} r="1.6" fill="#161616" />))}
    </svg>
  )
}

export function Plus() {
  return (
    <svg aria-hidden="true" width="28" height="28" viewBox="0 0 24 24">
      <path d="M12 5v14M5 12h14" stroke="#13406f" strokeWidth="2.4" strokeLinecap="round" />
    </svg>
  )
}

const pegColors: Record<string, [string, string]> = {
  red: ['#d7262e', '#a81c22'],
  yellow: ['#f2c230', '#c99a14'],
  green: ['#2e9a4f', '#1f6b37'],
}

// Peg is the clothespin that holds a sheet on the rope.
export function Peg({ tone }: { tone: keyof typeof pegColors }) {
  const [a, b] = pegColors[tone]
  return (
    <svg className="peg" aria-hidden="true" width="18" height="48" viewBox="0 0 18 48">
      <rect x="1" y="0" width="7.4" height="48" rx="3" fill={a} />
      <rect x="9.6" y="0" width="7.4" height="48" rx="3" fill={b} />
      <rect x="3" y="2" width="1.6" height="44" rx="0.8" fill="rgba(255,255,255,0.35)" />
      <path d="M8.4 34 L9.6 34 L9.6 48 L8.4 48 Z" fill="rgba(22,22,22,0.25)" />
      <rect x="0" y="17" width="18" height="7" rx="2" fill="#c9cdd3" stroke="#80858c" strokeWidth="0.8" />
      <path d="M3 17.5 V23.5 M6 17.5 V23.5 M9 17.5 V23.5 M12 17.5 V23.5 M15 17.5 V23.5" stroke="#80858c" strokeWidth="0.9" />
      <circle cx="9" cy="20.5" r="1.4" fill="#80858c" />
    </svg>
  )
}
