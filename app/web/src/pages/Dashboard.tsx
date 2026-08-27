import { useMemo, useState } from 'react'
import type { LedgerEntry, Pnl, Proposal } from '../lib/api'
import { brl } from '../lib/api'
import { BeforeAfterChart } from '../components/BeforeAfterChart'
import { EvolutionChart } from '../components/EvolutionChart'
import { ConsentModal } from '../components/ConsentModal'

const RECIPE_TITLES: Record<string, string> = {
  dishwasher: 'Buy a dishwasher (agent purchase)',
  'commute-audit': 'Commute audit',
  'leave-on-time': 'Leave-on-time blocks',
  'calendar-batching': 'Chore batching',
  'delegation-draft': 'Delegation drafts',
  'boleto-pile': 'Boleto pile to calendar',
  'school-note': 'School note to calendar',
  'teams-report': 'Team automation report',
  'robot-vacuum': 'Robot vacuum',
  'grocery-delivery': 'Grocery delivery subscription',
  'laundry-service': 'Wash-and-fold service',
  'auto-pay': 'Auto-pay migration',
  'farmacia-popular': 'Farmácia Popular check',
  'sne-discount': 'SNE 40% fine discount',
  'car-worth-it': 'Is your car worth it?',
  'virtual-assistant': 'Delegate to a virtual assistant',
  'forgotten-money': 'Forgotten money ritual',
}

export function Dashboard({
  pnl,
  proposals,
  ledger,
  refresh,
}: {
  pnl: Pnl
  proposals: Proposal[]
  ledger: LedgerEntry[]
  refresh: () => void
}) {
  const [consentFor, setConsentFor] = useState<Proposal | null>(null)
  const tasks = pnl.tasks ?? []
  const annual = pnl.total_cents_month * 12

  const recovered = useMemo(
    () => ledger.reduce((s, e) => s + e.brl_recovered_cents, 0),
    [ledger],
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
          className="m-0 leading-tight"
          style={{ fontFamily: 'var(--font-display)', fontSize: 'clamp(2.2rem, 5vw, 3.2rem)', fontWeight: 600 }}
        >
          {brl(annual)} <span className="text-ink-tertiary text-2xl">a year</span>
        </h1>
        <p className="text-ink-secondary mt-1 text-sm">
          {brl(pnl.total_cents_month)} every month · {pnl.total_hours_month.toFixed(0)} hours you don't get back
        </p>
      </header>

      {/* KPI pills */}
      <div className="flex flex-wrap gap-3 rise" style={{ animationDelay: '60ms' }}>
        <Kpi label="hours leaking / month" value={`${pnl.total_hours_month.toFixed(0)} h`} tone="leak" />
        <Kpi label="money leaking / month" value={brl(pnl.total_cents_month)} tone="leak" />
        <Kpi label="bought back so far" value={brl(recovered)} tone="win" />
      </div>

      <div className="grid gap-5 lg:grid-cols-2">
        <section className="bg-surface/85 rounded-card hairline shadow-[var(--shadow-lift)] p-5 rise" style={{ animationDelay: '120ms' }}>
          <h2 className="m-0 mb-4 text-lg font-medium">Before / after automation</h2>
          <BeforeAfterChart tasks={tasks} proposals={proposals} />
        </section>

        <section className="bg-surface/85 rounded-card hairline shadow-[var(--shadow-lift)] p-5 rise" style={{ animationDelay: '180ms' }}>
          <h2 className="m-0 mb-4 text-lg font-medium">Time bought back</h2>
          <EvolutionChart entries={ledger} />
        </section>
      </div>

      {/* Proposals — goal gradient: payback rendered as distance to break-even */}
      <section className="rise" style={{ animationDelay: '240ms' }}>
        <h2 className="m-0 mb-3 text-lg font-medium">What your agent proposes</h2>
        {proposals.length === 0 ? (
          <div className="bg-surface/85 rounded-card hairline p-6 text-sm text-ink-secondary">
            Describe one routine to the agent (bottom right) and proposals appear here, ranked by
            payback.
          </div>
        ) : (
          <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
            {proposals.map((p) => (
              <ProposalCard key={p.id} p={p} onBuy={() => setConsentFor(p)} />
            ))}
          </div>
        )}
      </section>

      {consentFor && (
        <ConsentModal
          proposal={consentFor}
          recipeTitle={RECIPE_TITLES[consentFor.recipe_id] ?? consentFor.recipe_id}
          onClose={() => setConsentFor(null)}
          onDone={refresh}
        />
      )}
    </div>
  )
}

function Kpi({ label, value, tone }: { label: string; value: string; tone: 'leak' | 'win' }) {
  return (
    <div className="rounded-pill bg-white/60 hairline px-4 py-2.5 flex items-center gap-3 min-w-[210px]">
      <span
        className="w-2 h-2 rounded-full"
        style={{ background: tone === 'leak' ? '#a07c12' : '#2e7d32' }}
        aria-hidden
      />
      <div>
        <div className="text-xs text-ink-secondary leading-none mb-1">{label}</div>
        <div className="tabular font-semibold leading-none">{value}</div>
      </div>
    </div>
  )
}

function ProposalCard({ p, onBuy }: { p: Proposal; onBuy: () => void }) {
  const title = RECIPE_TITLES[p.recipe_id] ?? p.recipe_id
  const executed = p.status === 'executed'
  // goal gradient: 2.1 months to break even, bar already moving
  const paybackPct = p.payback_months <= 0 ? 100 : Math.min(100 / p.payback_months, 92)
  return (
    <div className="bg-surface/85 rounded-card hairline shadow-[var(--shadow-lift)] p-4 flex flex-col gap-2">
      <div className="flex items-start justify-between gap-2">
        <span className="font-medium text-sm">{title}</span>
        <StatusPill status={p.status} />
      </div>
      <div className="text-sm text-ink-secondary">
        recovers <span className="text-success font-medium tabular">{brl(p.net_monthly_cents)}</span>
        /month
      </div>
      {p.payback_months > 0 && Number.isFinite(p.payback_months) && (
        <div>
          <div className="flex justify-between text-xs text-ink-tertiary mb-1">
            <span>pays for itself</span>
            <span className="tabular">{p.payback_months.toFixed(1)} months</span>
          </div>
          <div className="h-2 rounded-pill bg-[rgba(36,35,33,0.06)] overflow-hidden">
            <div className="h-full rounded-pill bg-sun" style={{ width: `${paybackPct}%` }} />
          </div>
        </div>
      )}
      {p.recipe_id === 'dishwasher' && !executed && (
        <button
          onClick={onBuy}
          className="mt-1 rounded-pill bg-ink text-white text-sm py-2 cursor-pointer font-medium"
        >
          Let the agent buy it · {p.payback_months.toFixed(1)}-month payback
        </button>
      )}
      {executed && (
        <div className="text-xs text-success bg-success-tint rounded-pill px-3 py-1.5 inline-flex items-center gap-1.5 self-start">
          <span className="w-1.5 h-1.5 rounded-full bg-success" /> purchased via AP2 · receipts on ledger
        </div>
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
