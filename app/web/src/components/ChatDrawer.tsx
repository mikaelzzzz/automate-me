import { useEffect, useRef, useState } from 'react'
import Markdown from 'react-markdown'
import { ChatSession, type PendingConfirmation } from '../lib/chat'

interface Msg {
  who: 'you' | 'agent'
  text: string
}

// Human wording for a tool the agent wants to run.
function describe(p: PendingConfirmation): string {
  const o = p.original
  if (!o) return p.hint
  const args = o.args ?? {}
  if (o.name === 'approve_proposal') {
    const id = String(args['proposal_id'] ?? '')
    const recipe = id.split('-').slice(-1)[0] || id
    return `Approve the "${recipe.replace(/-/g, ' ')}" proposal — purchase still needs your consent screen.`
  }
  const summary = Object.entries(args)
    .map(([k, v]) => `${k}: ${String(v)}`)
    .join(', ')
  return `${o.name.replace(/_/g, ' ')}${summary ? ` (${summary})` : ''}`
}

const QUICK = [
  'I wash dishes about an hour every day',
  'What is leaking the most money?',
  'What should I automate first?',
]

export function ChatDrawer({ onDataChanged }: { onDataChanged: () => void }) {
  const [open, setOpen] = useState(false)
  const [msgs, setMsgs] = useState<Msg[]>([])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [offline, setOffline] = useState(false)
  const [pending, setPending] = useState<PendingConfirmation | null>(null)
  const sess = useRef<ChatSession>(new ChatSession())
  const bottom = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (open && !sess.current.ready && !offline) {
      sess.current.init().catch(() => setOffline(true))
    }
  }, [open, offline])

  useEffect(() => {
    bottom.current?.scrollIntoView({ behavior: 'smooth' })
  }, [msgs, pending])

  const deliver = (text: string, confirmation?: PendingConfirmation) => {
    if (text) setMsgs((m) => [...m, { who: 'agent', text }])
    setPending(confirmation ?? null)
    onDataChanged()
  }

  const send = async (text: string) => {
    if (!text.trim() || busy) return
    setMsgs((m) => [...m, { who: 'you', text }])
    setInput('')
    setBusy(true)
    try {
      const turn = await sess.current.send(text)
      deliver(turn.text || (turn.confirmation ? '' : '…'), turn.confirmation)
    } catch {
      setMsgs((m) => [...m, { who: 'agent', text: 'Chat is offline — is GOOGLE_API_KEY set on the server?' }])
    } finally {
      setBusy(false)
    }
  }

  const answer = async (yes: boolean) => {
    if (!pending) return
    setBusy(true)
    const p = pending
    setPending(null)
    try {
      const turn = await sess.current.confirm(p, yes)
      deliver(turn.text || (yes ? 'Confirmed.' : 'Cancelled.'), turn.confirmation)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      {/* push-to-open pill, thumb zone */}
      <button
        onClick={() => setOpen((o) => !o)}
        className="fixed bottom-5 right-5 z-40 rounded-pill bg-ink text-white px-5 py-3 text-sm font-medium shadow-[var(--shadow-float)] cursor-pointer hover:translate-y-[-1px] transition-transform"
      >
        {open ? 'Close' : '☀️ Talk to your agent'}
      </button>

      {open && (
        <div className="fixed bottom-20 right-5 z-40 w-[380px] max-w-[calc(100vw-2.5rem)] h-[520px] max-h-[70vh] bg-surface rounded-card border-subtle shadow-[var(--shadow-float)] flex flex-col overflow-hidden rise">
          <div className="px-4 py-3 border-b border-[rgba(36,35,33,0.05)]">
            <div className="font-medium">Automate.me agent</div>
            <div className="text-xs text-ink-tertiary">
              {offline ? 'offline — dashboard still works' : 'finds leaks · prices them · automates'}
            </div>
          </div>

          <div className="flex-1 overflow-y-auto px-4 py-3 space-y-2">
            {msgs.length === 0 && (
              <div className="text-sm text-ink-secondary">
                <p className="mt-0">Tell me one routine that eats your time — I’ll price it.</p>
                <div className="flex flex-col gap-1.5 mt-3">
                  {QUICK.map((q) => (
                    <button
                      key={q}
                      onClick={() => send(q)}
                      className="text-left text-xs rounded-pill border-subtle bg-surface-raised px-3 py-2 cursor-pointer hover:bg-sun-soft/40"
                    >
                      {q}
                    </button>
                  ))}
                </div>
              </div>
            )}
            {msgs.map((m, i) => (
              <div
                key={i}
                className={
                  m.who === 'you'
                    ? 'ml-8 bg-sun-soft/60 rounded-2xl rounded-br-md px-3 py-2 text-sm'
                    : 'mr-8 bg-surface-raised border-subtle rounded-2xl rounded-bl-md px-3 py-2 text-sm chat-md'
                }
              >
                {m.who === 'agent' ? <Markdown>{m.text}</Markdown> : m.text}
              </div>
            ))}
            {pending && (
              <div className="mr-4 bg-sun-soft/50 border border-sun-deep/40 rounded-2xl px-3 py-2.5 text-sm">
                <div className="font-medium mb-1">Agent asks your approval</div>
                <div className="text-xs text-ink-secondary mb-2">{describe(pending)}</div>
                <div className="flex gap-2">
                  <button
                    onClick={() => answer(true)}
                    className="rounded-pill bg-ink text-white px-3 py-1 text-xs cursor-pointer"
                  >
                    Approve
                  </button>
                  <button
                    onClick={() => answer(false)}
                    className="rounded-pill border-subtle bg-surface-raised px-3 py-1 text-xs cursor-pointer"
                  >
                    Decline
                  </button>
                </div>
              </div>
            )}
            {busy && <div className="text-xs text-ink-tertiary">thinking…</div>}
            <div ref={bottom} />
          </div>

          <form
            className="p-3 border-t border-[rgba(36,35,33,0.05)] flex gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              send(input)
            }}
          >
            <input
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="Describe a routine…"
              className="flex-1 rounded-pill bg-surface-raised border-subtle px-4 py-2 text-sm outline-none"
            />
            <button
              type="submit"
              disabled={busy}
              className="rounded-pill bg-ink text-white px-4 py-2 text-sm cursor-pointer disabled:opacity-50"
            >
              Send
            </button>
          </form>
        </div>
      )}
    </>
  )
}
