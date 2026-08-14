import { useMemo, useState } from 'react'
import type { LedgerEntry } from '../lib/api'
import { brl, hours } from '../lib/api'

// Hero chart: cumulative time/money bought back, week over week.
// Confirmed = solid gold area; projected = dashed blue line (dash is the
// secondary encoding on top of the validator-approved pair).
const GOLD = '#a07c12'
const BLUE = '#2c5fa8'

interface Pt {
  x: number
  week: string
  confirmed: number
  projected: number
  hoursCum: number
  simulated: boolean
}

export function EvolutionChart({ entries }: { entries: LedgerEntry[] }) {
  const [hover, setHover] = useState<number | null>(null)

  const pts = useMemo<Pt[]>(() => {
    let cCents = 0
    let pCents = 0
    let h = 0
    return entries.map((e, i) => {
      h += e.hours_recovered
      if (e.confirmed) cCents += e.brl_recovered_cents
      pCents += e.brl_recovered_cents
      return {
        x: i,
        week: new Date(e.week_start).toLocaleDateString('en-US', { month: 'short', day: 'numeric' }),
        confirmed: cCents,
        projected: pCents,
        hoursCum: h,
        simulated: e.id.startsWith('led-sim'),
      }
    })
  }, [entries])

  if (pts.length < 2) {
    return (
      <div className="text-ink-secondary text-sm py-8 text-center">
        Your savings curve starts with the first automation. Approve one and watch it climb.
      </div>
    )
  }

  const W = 560
  const H = 180
  const PAD = { l: 28, r: 96, t: 16, b: 26 }
  const maxY = Math.max(...pts.map((p) => p.projected), 1)
  const sx = (i: number) => PAD.l + (i / (pts.length - 1)) * (W - PAD.l - PAD.r)
  const sy = (v: number) => H - PAD.b - (v / maxY) * (H - PAD.t - PAD.b)

  const line = (key: 'confirmed' | 'projected') =>
    pts.map((p, i) => `${i ? 'L' : 'M'}${sx(i).toFixed(1)},${sy(p[key]).toFixed(1)}`).join(' ')
  const area = `${line('confirmed')} L${sx(pts.length - 1).toFixed(1)},${H - PAD.b} L${PAD.l},${H - PAD.b} Z`

  const last = pts[pts.length - 1]
  const anySim = pts.some((p) => p.simulated)

  return (
    <div>
      <div className="flex items-center gap-4 mb-2 text-xs text-ink-secondary">
        <span className="inline-flex items-center gap-1.5">
          <span className="w-3 h-0.5 rounded" style={{ background: GOLD }} /> Confirmed
        </span>
        <span className="inline-flex items-center gap-1.5">
          <svg width="14" height="2">
            <line x1="0" y1="1" x2="14" y2="1" stroke={BLUE} strokeWidth="2" strokeDasharray="4 3" />
          </svg>
          Projected
        </span>
        {anySim && (
          <span className="ml-auto bg-surface-subtle rounded-pill px-2.5 py-0.5">simulated weeks</span>
        )}
      </div>

      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="w-full"
        role="img"
        aria-label="Cumulative savings, confirmed versus projected"
        onMouseLeave={() => setHover(null)}
      >
        {[0.5, 1].map((f) => (
          <line
            key={f}
            x1={PAD.l}
            x2={W - PAD.r}
            y1={sy(maxY * f)}
            y2={sy(maxY * f)}
            stroke="rgba(36,35,33,0.06)"
          />
        ))}

        <path d={area} fill={GOLD} opacity="0.14" />
        <path d={line('projected')} fill="none" stroke={BLUE} strokeWidth="2" strokeDasharray="5 4" strokeLinecap="round" />
        <path d={line('confirmed')} fill="none" stroke={GOLD} strokeWidth="2" strokeLinecap="round" />

        {/* direct labels at line ends — selective, not on every point */}
        <text x={W - PAD.r + 6} y={sy(last.confirmed) + 4} fontSize="11" fill={GOLD} fontWeight="600">
          {brl(last.confirmed)}
        </text>
        {last.projected !== last.confirmed && (
          <text x={W - PAD.r + 6} y={sy(last.projected) + 4} fontSize="11" fill={BLUE}>
            {brl(last.projected)}
          </text>
        )}

        {pts.map((p, i) => (
          <g key={i}>
            {/* generous hover targets */}
            <rect
              x={sx(i) - (W - PAD.l - PAD.r) / (pts.length - 1) / 2}
              y={0}
              width={(W - PAD.l - PAD.r) / (pts.length - 1)}
              height={H}
              fill="transparent"
              onMouseEnter={() => setHover(i)}
            />
            <text x={sx(i)} y={H - 8} fontSize="10" fill="#7a7772" textAnchor="middle">
              {p.week}
            </text>
          </g>
        ))}

        {hover !== null && (
          <g pointerEvents="none">
            <line x1={sx(hover)} x2={sx(hover)} y1={PAD.t} y2={H - PAD.b} stroke="rgba(36,35,33,0.25)" strokeDasharray="2 3" />
            <circle cx={sx(hover)} cy={sy(pts[hover].confirmed)} r="4" fill={GOLD} stroke="#fbfaf6" strokeWidth="2" />
            <circle cx={sx(hover)} cy={sy(pts[hover].projected)} r="4" fill={BLUE} stroke="#fbfaf6" strokeWidth="2" />
          </g>
        )}
      </svg>

      {hover !== null && (
        <div className="text-xs text-ink-secondary bg-surface-raised border-subtle rounded-xl px-3 py-2 inline-block shadow-[var(--shadow-lift)]">
          <span className="font-medium text-ink">week of {pts[hover].week}</span>
          {' · '}confirmed {brl(pts[hover].confirmed)} · projected {brl(pts[hover].projected)} ·{' '}
          {hours(pts[hover].hoursCum)} back
        </div>
      )}
    </div>
  )
}
