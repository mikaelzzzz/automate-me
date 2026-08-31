import { useEffect, useState } from 'react'
import type { Agenda, AgendaEntry, Briefing, BriefingCard } from '../lib/api'
import { api, brl } from '../lib/api'

// Daily Briefing: one card per appointment, built unprompted by the day
// planner — when to leave, what the traffic costs, what to wear, and whether
// the route crosses ground that floods. Numbers are measured, not guessed.
export function BriefingView({ onAsk, version }: { onAsk: (text: string) => void; version: number }) {
  const [data, setData] = useState<Briefing | null>(null)
  const [agenda, setAgenda] = useState<Agenda | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')

  const load = () =>
    Promise.all([
      api.briefing().then(setData),
      // The agenda is read-only and never costs a route call, so a failure
      // here must not take the briefing down with it.
      api.agenda().then(setAgenda).catch(() => setAgenda(null)),
    ]).catch((e) => setError(String(e)))
  useEffect(() => {
    void load()
  }, [version])

  const run = async () => {
    setRunning(true)
    setError('')
    try {
      setData(await api.runBriefing())
      await api.agenda().then(setAgenda).catch(() => {})
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

      {agenda && agenda.entries.length > 0 && <AgendaPanel a={agenda} />}

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

// AgendaPanel is the day itself: every appointment the calendar holds, in
// order, with the reason each one is or is not a trip the agent can price.
function AgendaPanel({ a }: { a: Agenda }) {
  const [open, setOpen] = useState(true)
  const real = a.source.startsWith('google:')
  const counts = [
    a.trips > 0 && `${a.trips} to travel to`,
    a.remote > 0 && `${a.remote} remote`,
    a.no_place > 0 && `${a.no_place} without an address`,
    a.skipped > 0 && `${a.skipped} ignored`,
  ].filter(Boolean) as string[]

  return (
    <section className="bg-surface rounded-card hairline shadow-[var(--shadow-lift)] overflow-hidden rise">
      <header className="flex flex-wrap items-center gap-3 px-5 py-3.5 border-b border-[rgba(19,53,63,0.08)]">
        <div className="min-w-0 flex-1">
          <h2 className="m-0 text-base font-medium">Your day</h2>
          <p className="text-ink-tertiary text-xs mt-0.5 mb-0">
            {a.entries.length} appointment{a.entries.length === 1 ? '' : 's'}
            {counts.length > 0 && <> · {counts.join(' · ')}</>}
          </p>
        </div>
        <span
          className="text-[11px] rounded-pill px-2.5 py-1 shrink-0"
          style={real ? { background: '#F0E7D9', color: '#8B6B47' } : { background: 'rgba(19,53,63,0.05)', color: '#615f5b' }}
          title={a.source}
        >
          {real ? 'live Google Calendar' : 'seeded day'}
        </span>
        <button
          onClick={() => setOpen((o) => !o)}
          className="rounded-pill border-subtle bg-paper text-xs px-3 py-1.5 cursor-pointer hover:bg-gold-tint/40 transition-colors"
        >
          {open ? 'Hide' : 'Show'}
        </button>
      </header>

      {open && (
        <ul className="m-0 list-none p-0 max-h-[420px] overflow-y-auto">
          {a.entries.map((e) => (
            <AgendaRow key={e.id + e.start} e={e} />
          ))}
        </ul>
      )}
    </section>
  )
}

function AgendaRow({ e }: { e: AgendaEntry }) {
  const hhmm = (iso: string) => new Date(iso).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false })
  const style = {
    trip: { label: 'trip', bg: '#F0E7D9', color: '#8B6B47' },
    remote: { label: 'remote', bg: 'rgba(44,95,168,0.10)', color: '#2C5FA8' },
    no_place: { label: 'no address', bg: 'rgba(19,53,63,0.05)', color: '#615f5b' },
    ignored: { label: 'ignored', bg: 'transparent', color: '#9a9791' },
  }[e.kind]
  return (
    <li className="flex items-baseline gap-3 px-5 py-2.5 border-b border-[rgba(19,53,63,0.05)] last:border-0">
      <span className="tabular text-xs text-ink-tertiary w-[86px] shrink-0">
        {hhmm(e.start)}–{hhmm(e.end)}
      </span>
      <span className={`min-w-0 flex-1 text-sm truncate ${e.kind === 'ignored' ? 'text-ink-tertiary' : ''}`} title={e.location || e.summary}>
        {e.summary || 'Untitled'}
      </span>
      {e.reason && <span className="text-[11px] text-ink-tertiary hidden md:inline truncate max-w-[220px]">{e.reason}</span>}
      <span className="text-[11px] rounded-pill px-2 py-0.5 shrink-0" style={{ background: style.bg, color: style.color }}>
        {style.label}
      </span>
    </li>
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
