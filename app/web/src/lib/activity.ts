import type { Briefing, LedgerEntry, MandateRecord, Pnl, Proposal } from './api'
import { brl } from './api'

/**
 * The agent activity feed is *derived from real state*, never invented: every
 * row points at something the backend actually recorded — a briefing that ran,
 * a calendar block that was written, a proposal waiting on the user, a signed
 * AP2 purchase, a task captured from a photo.
 */
export interface Activity {
  id: string
  /** green = done, gold = waiting on the user, red = needs attention */
  tone: 'done' | 'waiting' | 'attention'
  title: string
  /** "08:02 · calendar_write · kept" */
  meta: string
  at: number
  action?: { label: string; go: 'proposals' | 'briefing' | 'ledger' | 'pnl' }
}

const time = (iso: string) =>
  new Date(iso).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false })

export function deriveActivity(
  pnl: Pnl | null,
  proposals: Proposal[],
  ledger: LedgerEntry[],
  briefing: Briefing | null,
  mandates: MandateRecord[],
): Activity[] {
  const out: Activity[] = []

  // Briefing: one row for the run, one per calendar block written.
  const cards = briefing?.cards ?? []
  if (cards.length > 0) {
    const traffic = cards.reduce((s, c) => s + c.traffic_cents, 0)
    const floods = cards.filter((c) => c.flood_risk !== 'none')
    const built = cards[0].departure_time
    out.push({
      id: 'brief-run',
      tone: 'done',
      title: `Planned ${cards.length} trip${cards.length === 1 ? '' : 's'} · priced traffic at ${brl(traffic)}`,
      meta: `${briefing?.day ?? ''} · maps_routes · weather_flood`,
      at: new Date(built).getTime() - 6e5,
      action: { label: 'Open the briefing', go: 'briefing' },
    })
    for (const c of cards.filter((c) => c.calendar_block_id)) {
      out.push({
        id: 'block-' + c.id,
        tone: 'done',
        title: `Wrote "Leave at ${time(c.departure_time)}" to Calendar`,
        meta: `${c.event_summary.split(' · ')[0]} · calendar_write${c.calendar_block_mode === 'simulated' ? ' · simulated' : ''}`,
        at: new Date(c.departure_time).getTime(),
      })
    }
    for (const c of floods) {
      out.push({
        id: 'flood-' + c.id,
        tone: c.flood_risk === 'alert' ? 'attention' : 'waiting',
        title: c.flood_risk === 'alert' ? 'Flood alert on a route today' : `Flood history on the route to ${c.event_summary.split(' · ')[0].toLowerCase()}`,
        meta: `${c.flood_points} GeoSampa point${c.flood_points === 1 ? '' : 's'} · weather_flood`,
        at: new Date(c.departure_time).getTime() - 1e5,
        action: { label: 'See the route', go: 'briefing' },
      })
    }
  }

  // Routines the agent captured from a photo.
  const fromPhoto = (pnl?.tasks ?? []).filter((t) => t.source === 'photo')
  if (fromPhoto.length > 0) {
    out.push({
      id: 'vision',
      tone: 'done',
      title: `Read ${fromPhoto.length} routine${fromPhoto.length === 1 ? '' : 's'} from your photo`,
      meta: 'vision · priced by the Value Engine',
      at: Date.now() - 36e5,
      action: { label: 'See the P&L', go: 'pnl' },
    })
  }

  // Purchases: signed, verifiable, on the ledger.
  for (const m of mandates.filter((m) => m.status === 'completed')) {
    const entry = ledger.find((e) => e.mandate_ref === m.id)
    out.push({
      id: 'mandate-' + m.id,
      tone: 'done',
      title: `Purchased ${entry?.recipe_title ?? 'an automation'} over AP2`,
      meta: `${time(m.updated_at)} · ap2_purchase · 2 mandates, 2 receipts`,
      at: new Date(m.updated_at).getTime(),
      action: { label: 'Read the receipts', go: 'ledger' },
    })
  }

  // Waiting on the user.
  const approved = proposals.filter((p) => p.status === 'approved' && p.executable)
  for (const p of approved) {
    out.push({
      id: 'await-' + p.id,
      tone: 'waiting',
      title: `${p.recipe_title} approved — waiting on your signature`,
      meta: 'the agent cannot sign payment mandates',
      at: Date.now() - 6e4,
      action: { label: 'Open consent', go: 'proposals' },
    })
  }
  const proposed = proposals.filter((p) => p.status === 'proposed')
  if (proposed.length > 0) {
    const best = proposed.reduce((a, b) => (b.net_monthly_cents > a.net_monthly_cents ? b : a))
    out.push({
      id: 'proposals',
      tone: 'waiting',
      title: `${proposed.length} proposal${proposed.length === 1 ? '' : 's'} ready · best recovers ${brl(best.net_monthly_cents)}/mo`,
      meta: 'ranked by the deterministic Value Engine',
      at: Date.now() - 12e4,
      action: { label: 'Review them', go: 'proposals' },
    })
  }

  // Proof so far.
  const confirmed = ledger.filter((e) => e.confirmed)
  if (confirmed.length > 0) {
    const total = confirmed.reduce((s, e) => s + e.brl_recovered_cents, 0)
    out.push({
      id: 'ledger',
      tone: 'done',
      title: `${brl(total)} confirmed on the ledger`,
      meta: `${confirmed.length} week${confirmed.length === 1 ? '' : 's'} of recovered time`,
      at: Date.now() - 72e5,
      action: { label: 'Open the ledger', go: 'ledger' },
    })
  }

  return out.sort((a, b) => b.at - a.at)
}
