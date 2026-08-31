import type { Proposal } from '../lib/api'
import { brl } from '../lib/api'
import type { Screen } from '../components/AgentRail'

// Everything the agent wants to do, and what it is waiting on. Purchases are
// separated from guided recipes because only one of them needs your signature.
export function ProposalsView({
  proposals,
  onBuy,
  onAsk,
  onGo,
}: {
  proposals: Proposal[]
  onBuy: (p: Proposal) => void
  onAsk: (text: string) => void
  onGo: (s: Screen) => void
}) {
  const rank = { approved: 0, proposed: 1, executed: 2, declined: 3 }
  const sorted = [...proposals].sort((a, b) => {
    const d = rank[a.status] - rank[b.status]
    return d !== 0 ? d : b.net_monthly_cents - a.net_monthly_cents
  })
  const waiting = sorted.filter((p) => p.status === 'approved' && p.executable)
  const total = proposals.filter((p) => p.status !== 'declined').reduce((s, p) => s + p.net_monthly_cents, 0)

  if (proposals.length === 0) {
    return (
      <div className="max-w-[560px] rise">
        <p className="scap m-0">Proposals</p>
        <h1 className="display m-0 mt-1 text-[30px] font-semibold leading-tight">Nothing proposed yet</h1>
        <p className="text-ink-secondary text-sm mt-3 leading-relaxed">
          Describe a routine to the agent and it matches your list against the catalog, ranks by payback, and drops the
          proposals here.
        </p>
        <button
          onClick={() => onAsk('What should I automate first?')}
          className="mt-4 rounded-pill bg-teal text-white text-sm px-4 py-2 cursor-pointer"
        >
          Ask the agent
        </button>
      </div>
    )
  }

  return (
    <div className="space-y-5 max-w-[1180px]">
      <header className="rise">
        <p className="scap m-0">Proposals</p>
        <h1 className="display m-0 mt-1.5 leading-none" style={{ fontSize: 'clamp(2rem, 3.4vw, 2.7rem)', fontWeight: 600 }}>
          <span className="tabular">{brl(total)}</span>{' '}
          <span className="text-ink-tertiary font-normal" style={{ fontSize: '0.5em' }}>
            /month on the table
          </span>
        </h1>
        <p className="text-ink-secondary text-sm mt-2 m-0">
          {waiting.length > 0
            ? `${waiting.length} approved and waiting on your signature — the agent cannot sign a payment mandate.`
            : 'Ranked by what each one recovers every month, after its running cost.'}
        </p>
      </header>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {sorted.map((p, i) => (
          <ProposalCard key={p.id} p={p} delay={i * 50} onBuy={() => onBuy(p)} onAsk={onAsk} onGo={onGo} />
        ))}
      </div>
    </div>
  )
}

function ProposalCard({
  p,
  delay,
  onBuy,
  onAsk,
  onGo,
}: {
  p: Proposal
  delay: number
  onBuy: () => void
  onAsk: (t: string) => void
  onGo: (s: Screen) => void
}) {
  const done = p.status === 'executed'
  const approved = p.status === 'approved'
  const pct = p.payback_months <= 0 ? 100 : Math.min(100 / p.payback_months, 92)
  return (
    <article
      className={`bg-surface rounded-card border shadow-[var(--shadow-lift)] p-5 flex flex-col gap-3 rise ${
        approved ? 'border-gold' : 'border-line'
      }`}
      style={{ animationDelay: `${delay}ms` }}
    >
      <div className="flex items-start justify-between gap-2">
        <h3 className="m-0 text-[15px] font-semibold leading-snug">{p.recipe_title}</h3>
        <Status s={p.status} />
      </div>
      {p.recipe_description && <p className="m-0 text-xs text-ink-tertiary leading-relaxed">{p.recipe_description}</p>}

      <div className="flex items-baseline gap-1.5">
        <span className="display text-[26px] text-positive tabular leading-none">{brl(p.net_monthly_cents)}</span>
        <span className="text-xs text-ink-tertiary">recovered / month</span>
      </div>

      <div>
        <div className="flex justify-between text-xs text-ink-tertiary mb-1">
          <span>{p.upfront_cents > 0 ? `${brl(p.upfront_cents)} upfront` : 'no upfront cost'}</span>
          <span className="tabular">{p.payback_months > 0 ? `pays back in ${p.payback_months.toFixed(1)} mo` : 'pays back immediately'}</span>
        </div>
        <div className="h-1.5 rounded-pill bg-surface-sunk overflow-hidden">
          <div className="h-full rounded-pill bg-gold" style={{ width: `${pct}%` }} />
        </div>
      </div>

      <div className="mt-auto pt-1">
        {done ? (
          <button
            onClick={() => onGo('ledger')}
            className="text-xs text-positive bg-positive-tint hover:brightness-[0.98] rounded-pill px-3 py-1.5 cursor-pointer inline-flex items-center gap-1.5"
          >
            <span className="w-1.5 h-1.5 rounded-full bg-positive" /> purchased via AP2 · receipts →
          </button>
        ) : p.executable ? (
          <button
            onClick={onBuy}
            className={`w-full rounded-pill text-sm font-medium py-2 cursor-pointer ${
              approved ? 'bg-gold text-teal pop hover:brightness-105' : 'bg-teal text-white hover:bg-teal-soft'
            }`}
          >
            {approved ? 'Review & authorize' : 'Review the purchase'}
          </button>
        ) : (
          <button
            onClick={() => onAsk(`How do I set up "${p.recipe_title}"? Give me the concrete steps.`)}
            className="w-full rounded-pill border border-line bg-surface text-sm py-2 cursor-pointer hover:bg-gold-tint transition-colors"
          >
            Ask the agent how
          </button>
        )}
      </div>
    </article>
  )
}

function Status({ s }: { s: Proposal['status'] }) {
  const map = {
    proposed: { bg: 'var(--color-surface-sunk)', fg: 'var(--color-ink-secondary)', label: 'proposed' },
    approved: { bg: 'var(--color-gold-tint)', fg: 'var(--color-gold-deep)', label: 'approved' },
    executed: { bg: 'var(--color-positive-tint)', fg: 'var(--color-positive)', label: 'done' },
    declined: { bg: 'var(--color-surface-sunk)', fg: 'var(--color-ink-tertiary)', label: 'declined' },
  }[s]
  return (
    <span className="text-[11px] font-medium rounded-pill px-2.5 py-1 shrink-0" style={{ background: map.bg, color: map.fg }}>
      {map.label}
    </span>
  )
}
