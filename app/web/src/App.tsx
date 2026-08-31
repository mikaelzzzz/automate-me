import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { api, type Briefing, type LedgerEntry, type MandateRecord, type Pnl, type Proposal } from './lib/api'
import { deriveActivity } from './lib/activity'
import { AgentRail, type AgentHandle, type Screen } from './components/AgentRail'
import { Dashboard } from './pages/Dashboard'
import { ProposalsView } from './pages/Proposals'
import { BriefingView } from './components/BriefingView'
import { LedgerView } from './components/LedgerView'
import { ConsentModal } from './components/ConsentModal'

const RAIL: { id: Screen; label: string; icon: ReactNode }[] = [
  {
    id: 'pnl',
    label: 'P&L',
    icon: (
      <>
        <rect x="3" y="3" width="7" height="7" rx="1.6" />
        <rect x="14" y="3" width="7" height="7" rx="1.6" />
        <rect x="3" y="14" width="7" height="7" rx="1.6" />
        <rect x="14" y="14" width="7" height="7" rx="1.6" />
      </>
    ),
  },
  {
    id: 'briefing',
    label: 'Briefing',
    icon: (
      <>
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
      </>
    ),
  },
  { id: 'proposals', label: 'Proposals', icon: <path d="M13 2 4.5 13H11l-1 9 8.5-11H12l1-9z" /> },
  { id: 'ledger', label: 'Ledger', icon: <path d="M4 20V10M10 20V4M16 20v-7M22 20H2" /> },
  {
    id: 'teams',
    label: 'Teams',
    icon: (
      <>
        <circle cx="9" cy="8" r="3.2" />
        <path d="M2.5 20a6.5 6.5 0 0 1 13 0M16.5 5.2a3.2 3.2 0 0 1 0 5.9M18 20a6.6 6.6 0 0 0-2.2-4.9" />
      </>
    ),
  },
  { id: 'guardian', label: 'Guardian', icon: <path d="M12 2.5 20 6v6c0 5-3.4 8.4-8 9.5-4.6-1.1-8-4.5-8-9.5V6l8-3.5z" /> },
]

export default function App() {
  const [screen, setScreen] = useState<Screen>('pnl')
  const [pnl, setPnl] = useState<Pnl | null>(null)
  const [proposals, setProposals] = useState<Proposal[]>([])
  const [ledger, setLedger] = useState<LedgerEntry[]>([])
  const [briefing, setBriefing] = useState<Briefing | null>(null)
  const [mandates, setMandates] = useState<MandateRecord[]>([])
  const [error, setError] = useState('')
  const [consentFor, setConsentFor] = useState<Proposal | null>(null)
  const [version, setVersion] = useState(0)
  const [railOpen, setRailOpen] = useState(false)
  const agent = useRef<AgentHandle>(null)

  const refresh = useCallback(() => {
    setVersion((v) => v + 1)
    Promise.all([api.pnl(), api.proposals(), api.ledger()])
      .then(([p, pr, l]) => {
        setPnl(p)
        setProposals(pr ?? [])
        setLedger(l ?? [])
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    api.briefing().then(setBriefing).catch(() => setBriefing(null))
    api.mandates().then(setMandates).catch(() => setMandates([]))
  }, [])

  useEffect(refresh, [refresh])

  const activity = useMemo(
    () => deriveActivity(pnl, proposals, ledger, briefing, mandates),
    [pnl, proposals, ledger, briefing, mandates],
  )
  const waiting = proposals.filter((p) => p.status === 'proposed' || p.status === 'approved').length

  const go = (s: Screen) => {
    setScreen(s)
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }
  const askAgent = (text: string) => {
    setRailOpen(true)
    agent.current?.send(text)
  }
  const openConsent = (id: string) => {
    const p = proposals.find((x) => x.id === id)
    if (p) setConsentFor(p)
  }

  return (
    <div className="min-h-screen flex">
      {/* capability rail */}
      <nav
        className="fixed inset-y-0 left-0 z-30 flex flex-col items-center bg-teal text-white py-4 gap-1"
        style={{ width: 'var(--rail-w)' }}
        aria-label="Sections"
      >
        <span className="display text-[19px] font-semibold mb-3 select-none">
          A<span className="text-gold">.</span>m
        </span>
        {RAIL.map((r) => {
          const active = screen === r.id
          return (
            <button
              key={r.id}
              onClick={() => go(r.id)}
              aria-current={active}
              title={r.label}
              className={`relative w-[58px] py-2 rounded-[13px] flex flex-col items-center gap-1 cursor-pointer transition-colors ${
                active ? 'bg-white/[0.09]' : 'hover:bg-white/[0.05]'
              }`}
            >
              <svg
                width="19"
                height="19"
                viewBox="0 0 24 24"
                fill="none"
                stroke={active ? '#BC9A75' : 'rgba(255,255,255,.62)'}
                strokeWidth="1.7"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden
              >
                {r.icon}
              </svg>
              <span
                className="text-[8.5px] font-semibold tracking-[0.07em] uppercase leading-none"
                style={{ color: active ? '#fff' : 'rgba(255,255,255,.55)' }}
              >
                {r.label}
              </span>
              {r.id === 'proposals' && waiting > 0 && (
                <span className="absolute top-1 right-1.5 min-w-[15px] h-[15px] px-1 rounded-full bg-gold text-teal text-[9.5px] font-bold flex items-center justify-center">
                  {waiting}
                </span>
              )}
            </button>
          )
        })}
        <div className="mt-auto w-8 h-8 rounded-full bg-surface-warm" aria-hidden />
      </nav>

      {/* main column */}
      <main
        className="flex-1 min-w-0 px-7 py-7 xl:pr-[calc(var(--agent-w)+1.75rem)]"
        style={{ marginLeft: 'var(--rail-w)' }}
      >
        {error && (
          <div className="bg-alert-tint text-alert-deep rounded-xl px-4 py-3 text-sm mb-5">Backend unreachable: {error}</div>
        )}

        {screen === 'pnl' &&
          (pnl ? (
            <Dashboard pnl={pnl} proposals={proposals} ledger={ledger} onBuy={setConsentFor} onAsk={askAgent} onGo={go} />
          ) : (
            !error && <div className="text-ink-tertiary text-sm py-20 text-center">loading…</div>
          ))}

        {screen === 'proposals' && <ProposalsView proposals={proposals} onBuy={setConsentFor} onAsk={askAgent} onGo={go} />}
        {screen === 'briefing' && <BriefingView onAsk={askAgent} version={version} />}
        {screen === 'ledger' && <LedgerView entries={ledger} onAsk={askAgent} />}
        {screen === 'teams' && (
          <Soon
            title="Team automation report"
            line="Paste your team's manual tasks and get a shareable report: hours lost, what it costs, and what to automate first. The same report page the Guardian will use for progress."
          />
        )}
        {screen === 'guardian' && (
          <Soon
            title="Plan Guardian"
            line="Every approved automation becomes a plan with expected savings and expected signals. The Guardian watches for drift — the dishwasher arrived but you still logged 40 min of dishes — nudges once, and promotes ledger entries from projected to confirmed."
          />
        )}
      </main>

      {/* agent rail — docked from xl up, a bottom sheet below it. Always
          mounted so the session and the transcript survive the transition. */}
      {railOpen && <div className="fixed inset-0 z-30 xl:hidden" style={{ background: 'rgba(10,33,40,.35)' }} onClick={() => setRailOpen(false)} />}
      <div
        className={`fixed z-40 xl:z-20 xl:inset-y-0 xl:right-0 xl:left-auto xl:h-auto xl:translate-y-0 xl:w-[var(--agent-w)] inset-x-0 bottom-0 h-[86vh] rounded-t-[18px] xl:rounded-none overflow-hidden shadow-[var(--shadow-float)] xl:shadow-none transition-transform ${
          railOpen ? 'translate-y-0' : 'translate-y-full xl:translate-y-0'
        }`}
      >
        <AgentRail
          ref={agent}
          proposals={proposals}
          activity={activity}
          onDataChanged={refresh}
          onOpenConsent={(id) => {
            setRailOpen(false)
            openConsent(id)
          }}
          onGo={(s) => {
            setRailOpen(false)
            go(s)
          }}
          onClose={() => setRailOpen(false)}
        />
      </div>

      {!railOpen && (
        <button
          onClick={() => setRailOpen(true)}
          className="xl:hidden fixed bottom-5 right-5 z-30 rounded-pill bg-teal text-white px-5 py-3 text-sm font-medium shadow-[var(--shadow-float)] cursor-pointer"
        >
          Agent {activity.length > 0 && <span className="text-gold">· {activity.length}</span>}
        </button>
      )}

      {consentFor && (
        <ConsentModal
          proposal={consentFor}
          onClose={() => setConsentFor(null)}
          onDone={(res) => {
            refresh()
            if (res?.completed) {
              agent.current?.notify({
                kind: 'purchase',
                title: consentFor.recipe_title || consentFor.recipe_id,
                total: res.checkout.total.amount,
                mandateRef: res.mandate_record_id,
              })
            }
          }}
        />
      )}
    </div>
  )
}

function Soon({ title, line }: { title: string; line: string }) {
  return (
    <div className="max-w-[560px] rise">
      <p className="scap m-0">shipping next</p>
      <h1 className="display m-0 mt-1 text-[30px] font-semibold leading-tight">{title}</h1>
      <p className="text-ink-secondary text-sm mt-3 leading-relaxed">{line}</p>
    </div>
  )
}
