import { useEffect, useState } from 'react'
import type { Briefing, BriefingCard } from '../lib/api'
import { api, brl } from '../lib/api'

// Daily Briefing: one card per appointment, built unprompted by the day
// planner — when to leave, what the traffic costs, what to wear, and whether
// the route crosses ground that floods. Numbers are measured, not guessed.
export function BriefingView({ onAsk, version }: { onAsk: (text: string) => void; version: number }) {
  const [data, setData] = useState<Briefing | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')

  const load = () => api.briefing().then(setData).catch((e) => setError(String(e)))
  useEffect(() => {
    void load()
  }, [version])

  const run = async () => {
    setRunning(true)
    setError('')
    try {
      setData(await api.runBriefing())
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setRunning(false)
    }
  }

  const writeBlock = async (id: string) => {
    try {
      const card = await api.writeBlock(id)
      setData((d) => (d ? { ...d, cards: d.cards.map((c) => (c.id === card.id ? card : c)) } : d))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  if (!data) return <div className="text-ink-tertiary text-sm py-16 text-center">loading…</div>

  if (!data.available) {
    return (
      <Empty title="Daily Briefing is off on this server" line="Set MAPS_API_KEY (Routes + Weather) and the day planner comes alive." />
    )
  }

  const dayLabel = new Date(data.day + 'T12:00:00').toLocaleDateString('en-US', { weekday: 'long', month: 'long', day: 'numeric' })
  const todayKey = new Date().toLocaleDateString('en-CA', { timeZone: 'America/Sao_Paulo' })
  const when = data.day === todayKey ? 'today' : 'tomorrow'
  const worst = data.cards.reduce((m, c) => (rank(c.flood_risk) > rank(m?.flood_risk) ? c : m), data.cards[0])
  const trafficTotal = data.cards.reduce((s, c) => s + c.traffic_cents, 0)

  return (
    <div className="space-y-5 max-w-[1040px]">
      <header className="rise flex flex-wrap items-end gap-4">
        <div className="min-w-0 flex-1">
          <p className="text-ink-secondary m-0 text-sm">Daily Briefing · {dayLabel}</p>
          <h1 className="m-0 leading-tight" style={{ fontFamily: 'var(--font-display)', fontSize: 'clamp(1.8rem, 4vw, 2.6rem)', fontWeight: 600 }}>
            {data.cards.length === 0
              ? 'Nothing planned yet'
              : trafficTotal > 0
                ? <>Traffic will cost you <span className="tabular">{brl(trafficTotal)}</span> {when}</>
                : `Calm roads ${when}`}
          </h1>
          {worst && worst.flood_risk !== 'none' && (
            <p className="text-sm mt-1 m-0 text-ink-secondary">
              <span className={worst.flood_risk === 'alert' ? 'text-alert font-medium' : 'text-[#8B6B47] font-medium'}>
                {worst.flood_risk === 'alert' ? 'Flood alert' : 'Flood history'}
              </span>{' '}
              on the way to {worst.event_summary.split(' · ')[0].toLowerCase()} — {worst.flood_detail}.
            </p>
          )}
        </div>
        <div className="flex gap-2">
          <button
            onClick={run}
            disabled={running}
            className="rounded-pill bg-teal text-white text-sm px-4 py-2 cursor-pointer disabled:opacity-60 inline-flex items-center gap-2"
          >
            {running && <span className="ring" style={{ borderTopColor: '#f8d973' }} />}
            {running ? 'Fanning out route workers…' : data.cards.length ? 'Re-plan the day' : 'Plan my day'}
          </button>
          <button
            onClick={() => onAsk(`Brief me on ${when}: when do I leave, what does traffic cost, and any flood risk?`)}
            className="rounded-pill border-subtle bg-paper text-sm px-4 py-2 cursor-pointer hover:bg-gold-tint/40"
          >
            Ask the planner
          </button>
        </div>
      </header>

      {error && <div className="bg-alert-tint text-alert rounded-xl px-4 py-3 text-sm">{error}</div>}

      {data.cards.length === 0 && !running && (
        data.note
          ? <Empty title={`No trip to price ${when}`} line={data.note} />
          : <Empty title="Your day, planned before you ask" line="Three appointments seeded for the demo. Plan it and the agent computes departure times from live traffic, weather at departure and flood risk per route." />
      )}

      {data.note && data.cards.length > 0 && <p className="text-ink-secondary text-sm m-0">{data.note}</p>}

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {data.cards.map((c, i) => (
          <Card key={c.id} c={c} delay={i * 70} onBlock={() => writeBlock(c.id)} />
        ))}
      </div>

      {data.cards.length > 0 && (
        <p className="text-[11px] text-ink-tertiary">
          Sources: Google Routes API (traffic-aware, future departure) · Google Weather API (hourly forecast + public alerts) ·
          GeoSampa / Defesa Civil flooding occurrences (192 points, 2013→). Traffic cost = (duration − free-flow) × your hourly rate.
        </p>
      )}
    </div>
  )
}

function rank(r?: BriefingCard['flood_risk']) {
  return r === 'alert' ? 2 : r === 'historic' ? 1 : 0
}

function Card({ c, delay, onBlock }: { c: BriefingCard; delay: number; onBlock: () => void }) {
  const at = (iso: string) => new Date(iso).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false })
  const flood = {
    none: { label: 'no flood history on route', bg: 'rgba(19,53,63,0.05)', color: '#615f5b' },
    historic: { label: `${c.flood_points} flood point${c.flood_points === 1 ? '' : 's'} on route`, bg: '#F0E7D9', color: '#8B6B47' },
    alert: { label: 'flood alert on route', bg: '#FBEDE9', color: '#C9553D' },
  }[c.flood_risk]
  const written = !!c.calendar_block_id
  return (
    <article className="bg-surface rounded-card hairline shadow-[var(--shadow-lift)] p-5 flex flex-col gap-3 rise" style={{ animationDelay: `${delay}ms` }}>
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="text-xs text-ink-tertiary tabular">{at(c.event_start)}</div>
          <div className="font-medium leading-snug">{c.event_summary}</div>
        </div>
        <span className="text-[11px] rounded-pill px-2.5 py-1 shrink-0" style={{ background: flood.bg, color: flood.color }}>
          {flood.label}
        </span>
      </div>

      {/* the number that matters: when to leave */}
      <div className="flex items-baseline gap-2">
        <span className="text-ink-secondary text-sm">Leave at</span>
        <span className="tabular font-semibold" style={{ fontFamily: 'var(--font-display)', fontSize: '1.9rem', lineHeight: 1 }}>
          {at(c.departure_time)}
        </span>
      </div>
      <div className="text-sm text-ink-secondary -mt-1">{c.route_summary}</div>

      <div className="grid grid-cols-2 gap-2 text-xs">
        <Fact label="traffic" value={c.traffic_minutes > 0 ? `+${c.traffic_minutes} min · ${brl(c.traffic_cents)}` : 'free-flowing'} tone={c.traffic_minutes >= 15 ? 'leak' : undefined} />
        <Fact label="wear" value={c.clothing || '—'} />
        <Fact label="weather at departure" value={c.weather || 'no forecast'} span />
        {c.flood_risk !== 'none' && <Fact label="flood" value={c.flood_detail} span tone={c.flood_risk === 'alert' ? 'danger' : 'warn'} />}
        {c.alert_headline && <Fact label="public alert" value={c.alert_headline} span />}
      </div>

      {c.alternative_note && (
        <div className="text-xs bg-gold-tint/50 rounded-xl px-3 py-2">{c.alternative_note}</div>
      )}

      <div className="mt-auto pt-1">
        {written ? (
          <div className="text-xs text-positive bg-positive-tint rounded-pill px-3 py-1.5 inline-flex items-center gap-1.5">
            <span className="w-1.5 h-1.5 rounded-full bg-positive" /> “Leave at {at(c.departure_time)}” block on calendar
            {c.calendar_block_mode === 'simulated' && <span className="text-ink-tertiary">· simulated</span>}
          </div>
        ) : (
          <button onClick={onBlock} className="rounded-pill border-subtle bg-paper text-sm px-3.5 py-1.5 cursor-pointer hover:bg-gold-tint/40 transition-colors">
            Write “Leave at {at(c.departure_time)}” to calendar
          </button>
        )}
      </div>
    </article>
  )
}

function Fact({ label, value, span, tone }: { label: string; value: string; span?: boolean; tone?: 'leak' | 'warn' | 'danger' }) {
  const color = tone === 'leak' ? '#BC9A75' : tone === 'danger' ? '#C9553D' : tone === 'warn' ? '#8B6B47' : undefined
  return (
    <div className={`bg-paper border-subtle rounded-xl px-2.5 py-1.5 ${span ? 'col-span-2' : ''}`}>
      <div className="text-[10px] uppercase tracking-[0.06em] text-ink-tertiary">{label}</div>
      <div className="leading-snug" style={{ color }}>{value}</div>
    </div>
  )
}

function Empty({ title, line }: { title: string; line: string }) {
  return (
    <div className="bg-surface rounded-card hairline shadow-[var(--shadow-lift)] p-8 max-w-[640px]">
      <h2 className="m-0 text-lg font-medium">{title}</h2>
      <p className="text-ink-secondary text-sm mt-2 mb-0">{line}</p>
    </div>
  )
}
