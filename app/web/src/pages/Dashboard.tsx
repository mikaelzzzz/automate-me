import { useMemo } from 'react'
import type { LedgerEntry, Pnl, PnlTask, Proposal } from '../lib/api'
import { brl, brlWhole } from '../lib/api'
import { useCountUp } from '../lib/useCountUp'
import type { Screen } from '../components/AgentRail'
import { EvolutionChart } from '../components/EvolutionChart'

// Life P&L: the payback table is the spine of the product. Every row is a
// routine the agent captured, what it costs you a month, the fix it found,
// and how fast that fix pays for itself.

/** "50 min/day" · "1 h 30/week" · "40 min/month" — how the routine is lived. */
export function cadence(t: PnlTask): string {
  const mins = t.est_minutes
  const per = mins >= 60 ? `${Math.floor(mins / 60)} h${mins % 60 ? ` ${mins % 60}` : ''}` : `${mins} min`
  const f = t.freq_per_month
  const unit = f >= 20 ? 'day' : f >= 3.5 ? 'week' : f >= 1.5 ? 'fortnight' : 'month'
  return `${per}/${unit}`
}

const SOURCE: Record<string, string> = {
  interview: 'declared in chat',
  photo: 'read from a photo',
  calendar: 'from Calendar',
}

export function Dashboard({
  pnl,
  proposals,
  ledger,
  onBuy,
  onAsk,
  onGo,
}: {
  pnl: Pnl
  proposals: Proposal[]
  ledger: LedgerEntry[]
  onBuy: (p: Proposal) => void
  onAsk: (text: string) => void
  onGo: (s: Screen) => void
}) {
  const tasks = pnl.tasks ?? []
  const monthly = useCountUp(pnl.total_cents_month)
  const hours = useCountUp(pnl.total_hours_month)
  const annual = pnl.total_cents_month * 12

  const recovered = useMemo(() => ledger.filter((e) => e.confirmed).reduce((s, e) => s + e.brl_recovered_cents, 0), [ledger])
  const projected = useMemo(() => ledger.filter((e) => !e.confirmed).reduce((s, e) => s + e.brl_recovered_cents, 0), [ledger])
  const hoursBack = useMemo(() => ledger.reduce((s, e) => s + e.hours_recovered, 0), [ledger])
  const recoveredAnim = useCountUp(recovered)

  // one row per routine, carrying its best proposal
  const rows = useMemo(() => {
    const best = (taskId: string) =>
      proposals
        .filter((p) => p.task_id === taskId)
        .sort((a, b) => {
          const rank = { executed: 0, approved: 1, proposed: 2, declined: 3 }
          const d = rank[a.status] - rank[b.status]
          return d !== 0 ? d : b.net_monthly_cents - a.net_monthly_cents
        })[0]
    return tasks
      .map((t) => ({ task: t, fix: best(t.id) }))
      .sort((a, b) => b.task.cost_cents_month - a.task.cost_cents_month)
  }, [tasks, proposals])

  const withFix = rows.filter((r) => r.fix).length
  const active = proposals.filter((p) => p.status === 'executed').length
  const approved = proposals.filter((p) => p.status === 'approved').length

  const month = new Date().toLocaleDateString('en-US', { month: 'long', year: 'numeric' }).toUpperCase()

  return (
    <div className="space-y-5 max-w-[1180px]">
      {/* Anchoring: the monthly leak is the headline, the annual figure right
          under it. Loss aversion: it is the user's own routine, priced. */}
      <header className="rise flex flex-wrap items-start gap-4">
        <div className="min-w-0 flex-1">
          <p className="scap m-0">Life P&amp;L · {month}</p>
          <h1 className="display m-0 mt-1.5 leading-none" style={{ fontSize: 'clamp(2.4rem, 4.4vw, 3.4rem)', fontWeight: 600 }}>
            <span className="tabular">{brlWhole(Math.round(monthly))}</span>{' '}
            <span className="text-ink-tertiary font-normal" style={{ fontSize: '0.56em' }}>
              /month leaking
            </span>
          </h1>
          <p className="text-ink-secondary text-sm mt-2 m-0 tabular">
            {hours.toFixed(0)} h you don’t get back · {brlWhole(annual)} a year at {brl(pnl.hourly_rate_cents)}/h
          </p>
        </div>
        <button
          onClick={() => onAsk('I want to add another routine.')}
          className="rounded-pill bg-teal text-white text-sm font-medium px-4 py-2.5 cursor-pointer hover:bg-teal-soft shrink-0"
        >
          + Add routine
        </button>
      </header>

      {/* the payback table */}
      <section className="bg-surface rounded-card border border-line shadow-[var(--shadow-lift)] overflow-hidden rise" style={{ animationDelay: '60ms' }}>
        <div className="flex items-baseline justify-between gap-3 px-5 py-4">
          <h2 className="m-0 text-[17px] font-semibold">Ranked by payback</h2>
          <span className="text-xs text-ink-tertiary">
            deterministic Value Engine · {withFix} fix{withFix === 1 ? '' : 'es'} for {rows.length} routine{rows.length === 1 ? '' : 's'}
          </span>
        </div>

        {rows.length === 0 ? (
          <div className="px-5 pb-6 text-sm text-ink-secondary">
            No routines yet. Tell the agent one — or drop a photo of your list — and it lands here, priced.
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm border-collapse min-w-[720px]">
              <thead>
                <tr className="bg-surface-warm">
                  {['Routine', 'R$/mo lost', 'Suggested fix', 'Payback', ''].map((h, i) => (
                    <th
                      key={h || i}
                      className={`scap font-semibold px-5 py-2.5 text-left ${i === 1 ? 'text-right' : ''} ${i === 4 ? 'w-[104px]' : ''}`}
                    >
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map(({ task, fix }) => (
                  <tr key={task.id} className="border-t border-line align-middle hover:bg-surface-warm/60 transition-colors">
                    <td className="px-5 py-3.5">
                      <div className="font-semibold leading-tight">{task.name}</div>
                      <div className="text-xs text-ink-tertiary mt-0.5">
                        {cadence(task)} · {SOURCE[task.source] ?? task.source}
                      </div>
                    </td>
                    <td className="px-5 py-3.5 text-right">
                      <span className="display text-[21px] text-gold-deep tabular">
                        {(task.cost_cents_month / 100).toLocaleString('en-US', { maximumFractionDigits: 0 })}
                      </span>
                    </td>
                    <td className="px-5 py-3.5">
                      {fix ? (
                        <>
                          <div className="leading-tight">{fix.recipe_title}</div>
                          <div className="text-xs text-ink-tertiary mt-0.5">
                            recovers <span className="text-positive tabular">{brl(fix.net_monthly_cents)}</span>/mo
                          </div>
                        </>
                      ) : (
                        <button
                          onClick={() => onAsk(`What could automate "${task.name}"?`)}
                          className="text-xs text-gold-deep hover:text-teal cursor-pointer bg-transparent"
                        >
                          ask the agent →
                        </button>
                      )}
                    </td>
                    <td className="px-5 py-3.5">{fix && <PaybackPill p={fix} />}</td>
                    <td className="px-5 py-3.5">{fix && <RowAction p={fix} onBuy={() => onBuy(fix)} onAsk={onAsk} onGo={onGo} />}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* proof */}
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_320px] rise" style={{ animationDelay: '120ms' }}>
        <section className="bg-surface rounded-card border border-line shadow-[var(--shadow-lift)] p-5">
          <div className="flex items-baseline justify-between mb-4">
            <h2 className="m-0 text-[17px] font-semibold">Bought back, week over week</h2>
          </div>
          <EvolutionChart entries={ledger} />
        </section>

        <section className="bg-teal text-white rounded-card p-5 flex flex-col">
          <p className="scap m-0" style={{ color: '#BC9A75' }}>
            Recovered so far
          </p>
          <div className="display mt-3 leading-none tabular" style={{ fontSize: '2.6rem', fontWeight: 600 }}>
            {brl(Math.round(recoveredAnim))}
          </div>
          <p className="text-sm mt-2 m-0" style={{ color: 'rgba(255,255,255,.65)' }}>
            {hoursBack.toFixed(0)} h · {ledger.filter((e) => e.confirmed).length} confirmed,{' '}
            {ledger.filter((e) => !e.confirmed).length} projected
            {projected > 0 && <> · {brl(projected)} on the way</>}
          </p>
          <div className="mt-auto pt-4 border-t text-sm" style={{ borderColor: 'rgba(255,255,255,.14)', color: 'rgba(255,255,255,.8)' }}>
            {active === 0 && approved === 0
              ? 'Guardian: nothing running yet — approve an automation and it starts watching.'
              : active === 0
                ? `Guardian: ${approved} approved, waiting on your signature.`
                : `Guardian: ${active} automation${active === 1 ? '' : 's'} running${approved > 0 ? `, ${approved} waiting on your signature` : ''}.`}
          </div>
          <button
            onClick={() => onGo('ledger')}
            className="mt-3 self-start rounded-pill bg-gold text-teal text-xs font-semibold px-3.5 py-1.5 cursor-pointer hover:brightness-105"
          >
            Open the ledger →
          </button>
        </section>
      </div>
    </div>
  )
}

function PaybackPill({ p }: { p: Proposal }) {
  if (p.status === 'executed')
    return <Pill bg="var(--color-positive-tint)" fg="var(--color-positive)" text="active" />
  if (p.payback_months > 0)
    return <Pill bg="var(--color-positive-tint)" fg="var(--color-positive)" text={`${p.payback_months.toFixed(1)} mo`} />
  // no upfront cost: rank by what it nets you every month instead
  return <Pill bg="var(--color-gold-tint)" fg="var(--color-gold-deep)" text={`net +${brl(p.net_monthly_cents)}`} />
}

function Pill({ bg, fg, text }: { bg: string; fg: string; text: string }) {
  return (
    <span className="text-xs font-medium rounded-pill px-2.5 py-1 whitespace-nowrap tabular" style={{ background: bg, color: fg }}>
      {text}
    </span>
  )
}

function RowAction({
  p,
  onBuy,
  onAsk,
  onGo,
}: {
  p: Proposal
  onBuy: () => void
  onAsk: (t: string) => void
  onGo: (s: Screen) => void
}) {
  if (p.status === 'executed')
    return (
      <button onClick={() => onGo('ledger')} className="text-positive text-sm font-medium cursor-pointer bg-transparent">
        Running
      </button>
    )
  if (p.executable)
    return (
      <button
        onClick={onBuy}
        className={`rounded-pill text-sm font-medium px-4 py-1.5 cursor-pointer w-full ${
          p.status === 'approved'
            ? 'bg-gold text-teal hover:brightness-105 pop'
            : 'bg-gold-tint text-gold-deep hover:bg-gold hover:text-teal transition-colors'
        }`}
      >
        Review
      </button>
    )
  return (
    <button
      onClick={() => onAsk(`How do I set up "${p.recipe_title}"? Give me the concrete steps.`)}
      className="rounded-pill border border-line bg-surface text-sm px-4 py-1.5 cursor-pointer hover:bg-gold-tint w-full"
    >
      Review
    </button>
  )
}
