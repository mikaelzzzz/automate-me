import { useCallback, useEffect, useState } from 'react'
import { api, type LedgerEntry, type Pnl, type Proposal } from './lib/api'
import { Dashboard } from './pages/Dashboard'
import { ChatDrawer } from './components/ChatDrawer'

type Tab = 'Dashboard' | 'Briefing' | 'Teams' | 'Ledger'
const TABS: Tab[] = ['Dashboard', 'Briefing', 'Teams', 'Ledger']

export default function App() {
  const [tab, setTab] = useState<Tab>('Dashboard')
  const [pnl, setPnl] = useState<Pnl | null>(null)
  const [proposals, setProposals] = useState<Proposal[]>([])
  const [ledger, setLedger] = useState<LedgerEntry[]>([])
  const [error, setError] = useState('')

  const refresh = useCallback(() => {
    Promise.all([api.pnl(), api.proposals(), api.ledger()])
      .then(([p, pr, l]) => {
        setPnl(p)
        setProposals(pr ?? [])
        setLedger(l ?? [])
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  useEffect(refresh, [refresh])

  return (
    <div className="max-w-[1280px] mx-auto px-6 pb-24">
      <nav className="sticky top-0 z-30 py-4 flex items-center gap-4">
        <span className="font-semibold tracking-tight" style={{ fontFamily: 'var(--font-display)' }}>
          Automate<span className="text-sun-deep">.</span>me
        </span>
        <div className="rounded-pill bg-white/55 hairline px-2 py-1.5 flex gap-1 backdrop-blur-md">
          {TABS.map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              aria-current={tab === t}
              className={`rounded-pill px-3.5 py-1.5 text-sm cursor-pointer transition-colors ${
                tab === t ? 'bg-ink text-white font-medium' : 'text-ink-secondary hover:bg-white/80 hover:text-ink'
              }`}
            >
              {t}
            </button>
          ))}
        </div>
        <span className="ml-auto text-xs text-ink-tertiary rounded-pill bg-white/55 hairline px-3 py-1.5">
          demo mode · simulated payments
        </span>
      </nav>

      {error && (
        <div className="bg-danger-tint text-danger rounded-xl px-4 py-3 text-sm my-4">
          Backend unreachable: {error}
        </div>
      )}

      <main className="mt-2">
        {tab === 'Dashboard' &&
          (pnl ? (
            <Dashboard pnl={pnl} proposals={proposals} ledger={ledger} refresh={refresh} />
          ) : (
            !error && <div className="text-ink-tertiary text-sm py-16 text-center">loading…</div>
          ))}
        {tab === 'Briefing' && (
          <ComingSoon
            title="Daily Briefing"
            line="Departure times from live traffic, what to wear, and flood alerts for São Paulo — pushed before you ask."
          />
        )}
        {tab === 'Teams' && (
          <ComingSoon
            title="Team automation report"
            line="Paste your team's manual tasks and get a shareable report: hours lost, cost, and what to automate first."
          />
        )}
        {tab === 'Ledger' && <LedgerView entries={ledger} />}
      </main>

      <ChatDrawer onDataChanged={refresh} />
    </div>
  )
}

function ComingSoon({ title, line }: { title: string; line: string }) {
  return (
    <div className="bg-surface/85 rounded-card hairline shadow-[var(--shadow-lift)] p-10 text-center max-w-[560px] mx-auto mt-10">
      <div className="text-4xl mb-3">☀️</div>
      <h2 className="m-0 text-xl font-medium">{title}</h2>
      <p className="text-ink-secondary text-sm mt-2">{line}</p>
      <p className="text-xs text-ink-tertiary mt-4">shipping this week — building in public</p>
    </div>
  )
}

function LedgerView({ entries }: { entries: LedgerEntry[] }) {
  return (
    <div className="bg-surface/85 rounded-card hairline shadow-[var(--shadow-lift)] p-5 max-w-[720px]">
      <h2 className="m-0 mb-4 text-lg font-medium">Savings ledger</h2>
      {entries.length === 0 && (
        <p className="text-sm text-ink-secondary">Approve your first automation and proof lands here.</p>
      )}
      <div className="space-y-1.5">
        {entries.map((e) => (
          <div
            key={e.id}
            className="flex items-center gap-3 rounded-row px-3 py-2.5 hover:bg-sun/10 text-sm"
          >
            <span className="text-ink-tertiary text-xs tabular w-20 shrink-0">
              {new Date(e.week_start).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}
            </span>
            <span className="flex-1">{e.recipe_id}</span>
            {e.mandate_ref && (
              <span className="text-[11px] rounded-pill bg-sun-soft/60 px-2 py-0.5">AP2 receipt</span>
            )}
            <span
              className="text-[11px] rounded-pill px-2 py-0.5"
              style={{ background: e.confirmed ? '#eef8ea' : 'rgba(36,35,33,0.06)' }}
            >
              {e.confirmed ? 'confirmed' : 'projected'}
            </span>
            <span className="tabular font-medium w-24 text-right">
              {(e.brl_recovered_cents / 100).toLocaleString('en-US', {
                style: 'currency',
                currency: 'BRL',
              })}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}
