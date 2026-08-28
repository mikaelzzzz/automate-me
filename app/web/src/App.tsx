import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type LedgerEntry, type Pnl, type Proposal } from './lib/api'
import { Dashboard } from './pages/Dashboard'
import { ChatPanel, type ChatHandle } from './components/ChatPanel'
import { ConsentModal } from './components/ConsentModal'
import { LedgerView } from './components/LedgerView'
import { BriefingView } from './components/BriefingView'

type Tab = 'Dashboard' | 'Briefing' | 'Teams' | 'Ledger'
const TABS: Tab[] = ['Dashboard', 'Briefing', 'Teams', 'Ledger']

function readChatOpen(): boolean {
  try {
    const v = localStorage.getItem('am.chat')
    if (v !== null) return v === '1'
  } catch {
    /* storage unavailable */
  }
  return window.matchMedia('(min-width: 1024px)').matches
}

export default function App() {
  const [tab, setTab] = useState<Tab>('Dashboard')
  const [pnl, setPnl] = useState<Pnl | null>(null)
  const [proposals, setProposals] = useState<Proposal[]>([])
  const [ledger, setLedger] = useState<LedgerEntry[]>([])
  const [error, setError] = useState('')
  const [chatOpen, setChatOpen] = useState(readChatOpen)
  const [consentFor, setConsentFor] = useState<Proposal | null>(null)
  const [version, setVersion] = useState(0)
  const chat = useRef<ChatHandle>(null)

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
  }, [])

  useEffect(refresh, [refresh])

  useEffect(() => {
    try {
      localStorage.setItem('am.chat', chatOpen ? '1' : '0')
    } catch {
      /* ignore */
    }
  }, [chatOpen])

  const openChat = () => setChatOpen(true)
  const askAgent = (text: string) => {
    setChatOpen(true)
    chat.current?.send(text)
  }
  const openConsent = (id: string) => {
    const p = proposals.find((x) => x.id === id)
    if (p) setConsentFor(p)
  }
  const showLedger = () => {
    setTab('Ledger')
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  return (
    <div className="max-w-[1480px] mx-auto px-5 sm:px-6 pb-24">
      <nav className="sticky top-0 z-30 py-3.5 -mx-5 sm:-mx-6 px-5 sm:px-6 flex items-center gap-3 sm:gap-4 bg-canvas/80 backdrop-blur-md">
        <span className="font-semibold tracking-tight" style={{ fontFamily: 'var(--font-display)' }}>
          Automate<span className="text-sun-deep">.</span>me
        </span>
        <div className="rounded-pill bg-white/55 hairline px-1.5 py-1 flex gap-0.5 backdrop-blur-md overflow-x-auto">
          {TABS.map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              aria-current={tab === t}
              className={`rounded-pill px-3 sm:px-3.5 py-1.5 text-sm cursor-pointer transition-colors whitespace-nowrap ${
                tab === t ? 'bg-ink text-white font-medium' : 'text-ink-secondary hover:bg-white/80 hover:text-ink'
              }`}
            >
              {t}
            </button>
          ))}
        </div>
        <span className="ml-auto text-xs text-ink-tertiary rounded-pill bg-white/55 hairline px-3 py-1.5 whitespace-nowrap">
          <span className="sm:hidden">demo</span>
          <span className="hidden sm:inline">demo mode · simulated payments</span>
        </span>
        {!chatOpen && (
          <button
            onClick={openChat}
            className="hidden lg:inline-flex rounded-pill bg-ink text-white px-4 py-1.5 text-sm cursor-pointer items-center gap-2"
          >
            ☀️ Agent
          </button>
        )}
      </nav>

      {error && (
        <div className="bg-danger-tint text-danger rounded-xl px-4 py-3 text-sm my-4">
          Backend unreachable: {error}
        </div>
      )}

      {/* on lg+ the panel is fixed; the second grid column only reserves its width */}
      <div className={`grid gap-6 items-start ${chatOpen ? 'lg:grid-cols-[minmax(0,1fr)_400px] xl:grid-cols-[minmax(0,1fr)_420px]' : ''}`}>
        <main className="mt-2 min-w-0">
          {tab === 'Dashboard' &&
            (pnl ? (
              <Dashboard
                pnl={pnl}
                proposals={proposals}
                ledger={ledger}
                onBuy={setConsentFor}
                onAsk={askAgent}
                onShowLedger={showLedger}
              />
            ) : (
              !error && <div className="text-ink-tertiary text-sm py-16 text-center">loading…</div>
            ))}
          {tab === 'Briefing' && <BriefingView onAsk={askAgent} version={version} />}
          {tab === 'Teams' && (
            <ComingSoon
              title="Team automation report"
              line="Paste your team's manual tasks and get a shareable report: hours lost, cost, and what to automate first."
            />
          )}
          {tab === 'Ledger' && <LedgerView entries={ledger} onAsk={askAgent} />}
        </main>

        <ChatPanel
          ref={chat}
          open={chatOpen}
          proposals={proposals}
          onClose={() => setChatOpen(false)}
          onDataChanged={refresh}
          onOpenConsent={openConsent}
          onShowLedger={showLedger}
          onShowBriefing={() => {
            setTab('Briefing')
            window.scrollTo({ top: 0, behavior: 'smooth' })
          }}
        />
      </div>

      {/* mobile / collapsed: push-to-open pill in the thumb zone */}
      {!chatOpen && (
        <button
          onClick={openChat}
          className="fixed bottom-5 right-5 z-40 rounded-pill bg-ink text-white px-5 py-3 text-sm font-medium shadow-[var(--shadow-float)] cursor-pointer hover:translate-y-[-1px] transition-transform lg:hidden"
        >
          ☀️ Talk to your agent
        </button>
      )}

      {consentFor && (
        <ConsentModal
          proposal={consentFor}
          recipeTitle={consentFor.recipe_title || consentFor.recipe_id}
          onClose={() => setConsentFor(null)}
          onDone={(res) => {
            refresh()
            if (res?.completed) {
              chat.current?.notify({
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
