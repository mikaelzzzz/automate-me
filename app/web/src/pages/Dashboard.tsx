import { useMemo } from 'react'
import type { LedgerEntry, Pnl, Proposal } from '../lib/api'
import { brl } from '../lib/api'
import { useCountUp } from '../lib/useCountUp'
import { BeforeAfterChart } from '../components/BeforeAfterChart'
import { EvolutionChart } from '../components/EvolutionChart'

export function Dashboard({
  pnl,
  proposals,
  ledger,
  onBuy,
  onAsk,
  onShowLedger,
}: {
  pnl: Pnl
  proposals: Proposal[]
  ledger: LedgerEntry[]
  onBuy: (p: Proposal) => void
  onAsk: (text: string) => void
  onShowLedger: () => void
}) {
  const tasks = pnl.tasks ?? []
  const annual = useCountUp(pnl.total_cents_month * 12)
  const monthly = useCountUp(pnl.total_cents_month)
  const hoursMonth = useCountUp(pnl.total_hours_month)

  const recovered = useMemo(
    () => ledger.filter((e) => e.confirmed).reduce((s, e) => s + e.brl_recovered_cents, 0),
    [ledger],
  )
  const recoveredAnim = useCountUp(recovered)

  const ranked = useMemo(
    () =>
      [...proposals].sort((a, b) => {
        const order = { approved: 0, proposed: 1, executed: 2, declined: 3 }
        const d = order[a.status] - order[b.status]
        return d !== 0 ? d : b.net_monthly_cents - a.net_monthly_cents
      }),
    [proposals],
  )

  return (
    <div className="space-y-6">
      {/* Anchoring: the ANNUAL leak is the first number, display-sized.
          Loss aversion: it's the user's own routine, priced. */}
      <header className="rise">
        <p className="text-ink-secondary m-0 text-sm">
          Your routine is leaking, at {brl(pnl.hourly_rate_cents)}/h of your time
        </p>
        <h1
          className="m-0 leading-tight tabular"
          style={{ fontFamily: 'var(--font-display)', fontSize: 'clamp(2.2rem, 5vw, 3.2rem)', fontWeight: 600 }}
        >
          {brl(Math.round(annual))} <span className="text-ink-tertiary text-2xl">a year</span>
        </h1>
        <p className="text-ink-secondary mt-1 text-sm tabular">
          {brl(Math.round(monthly))} every month · {hoursMonth.toFixed(0)} hours you don't get back
        </p>
      </header>

      {/* KPI pills */}
      <div className="flex flex-wrap gap-3 rise" style={{ animationDelay: '60ms' }}>
        <Kpi label="hours leaking / month" value={`${hoursMonth.toFixed(0)} h`} tone="leak" />
        <Kpi label="money leaking / month" value={brl(Math.round(monthly))} tone="leak" />
        <Kpi label="bought back so far" value={brl(Math.round(recoveredAnim))} tone="win" onClick={onShowLedger} />
      </div>

      <div className="grid gap-5 xl:grid-cols-2">
        <section className="bg-surface/85 rounded-card hairline shadow-[var(--shadow-lift)] p-5 rise" style={{ animationDelay: '120ms' }}>
          <h2 className="m-0 mb-4 text-lg font-medium">Before / after automation</h2>
          <BeforeAfterChart tasks={tasks} proposals={proposals} />
        </section>

        <section className="bg-surface/85 rounded-card hairline shadow-[var(--shadow-lift)] p-5 rise" style={{ animationDelay: '180ms' }}>
          <div className="flex items-baseline justify-between mb-4">
            <h2 className="m-0 text-lg font-medium">Time bought back</h2>
            <button onClick={onShowLedger} className="text-xs text-ink-secondary hover:text-ink cursor-pointer bg-transparent">
              ledger →
            </button>
          </div>
          <EvolutionChart entries={ledger} />
        </section>
      </div>

      {/* Proposals — goal gradient: payback rendered as distance to break-even */}
      <section className="rise" style={{ animationDelay: '240ms' }}>
        <div className="flex items-baseline justify-between mb-3">
          <h2 className="m-0 text-lg font-medium">What your agent proposes</h2>
          {proposals.length > 0 && (
            <span className="text-xs text-ink-tertiary">{proposals.length} proposals · ranked by monthly savings</span>
          )}
        </div>
        {proposals.length === 0 ? (
          <div className="bg-surface/85 rounded-card hairline p-6 text-sm text-ink-secondary flex flex-wrap items-center gap-3">
            <span className="flex-1 min-w-[240px]">Describe one routine to the agent and proposals appear here, ranked by payback.</span>
            <button onClick={() => onAsk('What should I automate first?')} className="rounded-pill bg-ink text-white text-sm px-4 py-2 cursor-pointer">
              Ask the agent
            </button>
          </div>
        ) : (
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {ranked.map((p) => (
              <ProposalCard key={p.id} p={p} onBuy={() => onBuy(p)} onAsk={onAsk} onShowLedger={onShowLedger} />
            ))}
          </div>
        )}
      </section>
    </div>
  )
}

function Kpi({ label, value, tone, onClick }: { label: string; value: string; tone: 'leak' | 'win'; onClick?: () => void }) {
  const Tag = onClick ? 'button' : 'div'
  return (
    <Tag
      onClick={onClick}
      className={`rounded-pill bg-white/60 hairline px-4 py-2.5 flex items-center gap-3 min-w-[210px] text-left ${onClick ? 'cursor-pointer hover:bg-white/90 transition-colors' : ''}`}
    >
      <span
        className="w-2 h-2 rounded-full"
        style={{ background: tone === 'leak' ? '#a07c12' : '#2e7d32' }}
        aria-hidden
      />
      <div>
        <div className="text-xs text-ink-secondary leading-none mb-1">{label}</div>
        <div className="tabular font-semibold leading-none">{value}</div>
      </div>
    </Tag>
  )
}

function ProposalCard({
  p,
  onBuy,
  onAsk,
  onShowLedger,
}: {
  p: Proposal
  onBuy: () => void
  onAsk: (text: string) => void
  onShowLedger: () => void
}) {
  const title = p.recipe_title || p.recipe_id
  const executed = p.status === 'executed'
  const approved = p.status === 'approved'
  // goal gradient: 2.1 months to break even, bar already moving
  const paybackPct = p.payback_months <= 0 ? 100 : Math.min(100 / p.payback_months, 92)
  return (
    <div
      className={`bg-surface/85 rounded-card hairline shadow-[var(--shadow-lift)] p-4 flex flex-col gap-2 transition-shadow ${
        approved ? 'ring-2 ring-sun/70' : ''
      }`}
    >
      <div className="flex items-start justify-between gap-2">
        <span className="font-medium text-sm">{title}</span>
        <StatusPill status={p.status} />
      </div>
      <div className="text-sm text-ink-secondary">
        recovers <span className="text-success font-medium tabular">{brl(p.net_monthly_cents)}</span>
        /month
      </div>
      <div>
        <div className="flex justify-between text-xs text-ink-tertiary mb-1">
          <span>pays for itself</span>
          <span className="tabular">{p.payback_months > 0 ? `${p.payback_months.toFixed(1)} months` : 'immediately'}</span>
        </div>
        <div className="h-2 rounded-pill bg-[rgba(36,35,33,0.06)] overflow-hidden">
          <div className="h-full rounded-pill bg-sun" style={{ width: `${paybackPct}%` }} />
        </div>
      </div>

      {executed ? (
        <button
          onClick={onShowLedger}
          className="mt-1 text-xs text-success bg-success-tint hover:bg-[#e3f2de] rounded-pill px-3 py-1.5 inline-flex items-center gap-1.5 self-start cursor-pointer"
        >
          <span className="w-1.5 h-1.5 rounded-full bg-success" /> purchased via AP2 · receipts on ledger →
        </button>
      ) : p.executable ? (
        <button
          onClick={onBuy}
          className={`mt-1 rounded-pill text-sm py-2 cursor-pointer font-medium ${approved ? 'bg-ink text-white pop' : 'bg-ink text-white'}`}
        >
          {approved ? 'Review & sign · AP2' : `Let the agent buy it · ${p.payback_months.toFixed(1)}-month payback`}
        </button>
      ) : (
        <button
          onClick={() => onAsk(`How do I set up "${title}"? Give me the concrete steps.`)}
          className="mt-1 rounded-pill border-subtle bg-surface-raised text-sm py-2 cursor-pointer hover:bg-sun-soft/40 transition-colors"
        >
          {approved ? 'Ask the agent for the steps' : 'Ask the agent how'}
        </button>
      )}
    </div>
  )
}

function StatusPill({ status }: { status: Proposal['status'] }) {
  const map: Record<string, { bg: string; dot: string; label: string }> = {
    proposed: { bg: 'rgba(36,35,33,0.06)', dot: 'rgba(36,35,33,0.45)', label: 'proposed' },
    approved: { bg: '#fbf0cc', dot: '#e5b73c', label: 'approved' },
    executed: { bg: '#eef8ea', dot: '#2e7d32', label: 'done' },
    declined: { bg: 'rgba(36,35,33,0.06)', dot: 'rgba(36,35,33,0.3)', label: 'declined' },
  }
  const s = map[status] ?? map.proposed
  return (
    <span
      className="text-[11px] rounded-pill px-2.5 py-1 inline-flex items-center gap-1.5 shrink-0"
      style={{ background: s.bg }}
    >
      <span className="w-1.5 h-1.5 rounded-full" style={{ background: s.dot }} />
      {s.label}
    </span>
  )
}
