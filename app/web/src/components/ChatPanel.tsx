import { useCallback, useEffect, useImperativeHandle, useRef, useState, type Ref } from 'react'
import Markdown from 'react-markdown'
import {
  ChatSession,
  agentMeta,
  type Attachment,
  type FunctionCall,
  type PendingConfirmation,
  type StreamEvent,
} from '../lib/chat'
import type { Proposal } from '../lib/api'
import { brl } from '../lib/api'

// The agent panel: a live timeline of what the graph is doing — text streams
// token by token, tool calls show up as activity rows, tool results become
// cards you can act on (approve, sign) without leaving the conversation.

export interface ChatHandle {
  send: (text: string) => void
  notify: (n: Notice) => void
}

export type Notice = { kind: 'purchase'; title: string; total: number; mandateRef: string }

interface ProposalRow {
  proposal_id: string
  recipe: string
  monthly_savings: string
  net_monthly: string
  payback_months: string
  agent_can_execute: boolean
}

interface BriefingRow {
  card_id: string
  event: string
  event_at: string
  leave_at: string
  route: string
  traffic_minutes: number
  traffic_cost: string
  flood_risk: string
  flood_detail: string
  clothing: string
}

type Item =
  | { id: number; kind: 'user'; text: string; imageUrl?: string }
  | { id: number; kind: 'agent'; author: string; text: string; streaming: boolean }
  | { id: number; kind: 'activity'; label: string; status: 'running' | 'done'; tool: string; callId?: string }
  | { id: number; kind: 'task'; name: string; cost: string; updated: boolean }
  | { id: number; kind: 'proposals'; rows: ProposalRow[] }
  | { id: number; kind: 'approved'; proposalId: string }
  | { id: number; kind: 'briefing'; day: string; rows: BriefingRow[] }
  | { id: number; kind: 'purchase'; title: string; total: number; mandateRef: string }
  | { id: number; kind: 'confirm'; pending: PendingConfirmation }
  | { id: number; kind: 'error'; text: string }

// Omit that distributes over the union — plain Omit<Item,'id'> collapses to the common keys.
type DistributiveOmit<T, K extends keyof T> = T extends unknown ? Omit<T, K> : never
type NewItem = DistributiveOmit<Item, 'id'>

let seq = 1
const nextId = () => seq++

function activityLabel(call: FunctionCall): string {
  const a = call.args ?? {}
  switch (call.name) {
    case 'get_life_pnl':
      return 'Reading your Life P&L'
    case 'add_routine_task':
      return `Saving “${String(a['name'] ?? 'task')}” · ${String(a['minutes_per_occurrence'] ?? '?')} min × ${String(a['times_per_month'] ?? '?')}/mo`
    case 'propose_automations':
      return 'Matching your routine to the catalog · ranking by payback'
    case 'approve_proposal':
      return 'Recording your approval'
    case 'plan_my_day':
      return 'Planning the day · one route worker per appointment · traffic, weather, flood layers'
    case 'get_daily_briefing':
      return 'Reading today’s briefing'
    case 'write_departure_blocks':
      return 'Writing “Leave at” blocks to the calendar'
    default:
      return call.name.replace(/_/g, ' ')
  }
}

function describeConfirmation(p: PendingConfirmation, proposals: Proposal[]): string {
  const o = p.original
  if (!o) return p.hint
  if (o.name === 'approve_proposal') {
    const id = String(o.args?.['proposal_id'] ?? '')
    const prop = proposals.find((x) => x.id === id)
    const title = prop?.recipe_title ?? id.split('-').slice(-1)[0].replace(/-/g, ' ')
    return prop?.executable
      ? `Approve “${title}”? Nothing is bought yet — you still sign on the consent screen.`
      : `Approve “${title}”? The agent records it and guides the setup.`
  }
  return `${o.name.replace(/_/g, ' ')}`
}

function closeStreaming(items: Item[]): Item[] {
  const last = items[items.length - 1]
  if (last?.kind === 'agent' && last.streaming) {
    return [...items.slice(0, -1), { ...last, streaming: false }]
  }
  return items
}

const BASE_PROMPTS = [
  'I wash dishes about an hour every day',
  'What is leaking the most money?',
  'What should I automate first?',
]

export function ChatPanel({
  ref,
  open,
  proposals,
  onClose,
  onDataChanged,
  onOpenConsent,
  onShowLedger,
  onShowBriefing,
}: {
  ref?: Ref<ChatHandle>
  open: boolean
  proposals: Proposal[]
  onClose: () => void
  onDataChanged: () => void
  onOpenConsent: (proposalId: string) => void
  onShowLedger: () => void
  onShowBriefing: () => void
}) {
  const [items, setItems] = useState<Item[]>([])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [offline, setOffline] = useState(false)
  const [attachment, setAttachment] = useState<Attachment | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const sess = useRef<ChatSession>(new ChatSession())
  const scroller = useRef<HTMLDivElement>(null)
  const stickToBottom = useRef(true)
  const textarea = useRef<HTMLTextAreaElement>(null)
  // args of in-flight tool calls, so results can be rendered with context
  const callArgs = useRef<Record<string, Record<string, unknown>>>({})
  const seenResults = useRef<Set<string>>(new Set())

  useEffect(() => {
    if (open && !sess.current.ready && !offline) {
      sess.current.init().catch(() => setOffline(true))
    }
  }, [open, offline])

  useEffect(() => {
    if (stickToBottom.current) scroller.current?.scrollTo({ top: scroller.current.scrollHeight, behavior: 'smooth' })
  }, [items, busy])

  const push = useCallback((it: NewItem) => {
    setItems((xs) => [...closeStreaming(xs), { ...it, id: nextId() } as Item])
  }, [])

  const onEvent = useCallback(
    (ev: StreamEvent) => {
      switch (ev.kind) {
        case 'text':
          setItems((xs) => {
            const last = xs[xs.length - 1]
            if (last?.kind === 'agent' && last.streaming && last.author === ev.author) {
              const text = ev.partial ? last.text + ev.text : ev.text
              return [...xs.slice(0, -1), { ...last, text, streaming: ev.partial }]
            }
            return [...xs, { id: nextId(), kind: 'agent', author: ev.author, text: ev.text, streaming: ev.partial }]
          })
          break
        case 'transfer': {
          const label = `Handing over to ${agentMeta(ev.to).label}`
          setItems((xs) => {
            const last = xs[xs.length - 1]
            if (last?.kind === 'activity' && last.label === label) return xs
            return [...closeStreaming(xs), { id: nextId(), kind: 'activity', label, status: 'done', tool: 'transfer' }]
          })
          break
        }
        case 'call': {
          callArgs.current[ev.call.name] = ev.call.args ?? {}
          const callId = ev.call.id
          setItems((xs) => {
            if (callId && xs.some((x) => x.kind === 'activity' && x.callId === callId)) return xs
            return [...closeStreaming(xs), { id: nextId(), kind: 'activity', label: activityLabel(ev.call), status: 'running', tool: ev.call.name, callId }]
          })
          break
        }
        case 'result': {
          const name = ev.result.name
          const res = ev.result.response ?? {}
          const args = callArgs.current[name] ?? {}
          const needsConfirm = typeof res['error'] === 'string' && String(res['error']).includes('requires confirmation')
          const rid = ev.result.id
          if (rid && seenResults.current.has(rid)) break
          if (rid) seenResults.current.add(rid)
          setItems((xs) => {
            const out = [...xs]
            for (let i = out.length - 1; i >= 0; i--) {
              const it = out[i]
              if (it.kind === 'activity' && it.tool === name && it.status === 'running') {
                if (needsConfirm) out.splice(i, 1)
                else out[i] = { ...it, status: 'done' }
                break
              }
            }
            return out
          })
          if (name === 'add_routine_task' && typeof res['cost_of_inaction_per_month'] === 'string') {
            push({
              kind: 'task',
              name: String(args['name'] ?? 'Task'),
              cost: String(res['cost_of_inaction_per_month']),
              updated: !!res['updated_existing'],
            })
          } else if (name === 'propose_automations' && Array.isArray(res['proposals'])) {
            push({ kind: 'proposals', rows: res['proposals'] as ProposalRow[] })
          } else if (name === 'approve_proposal' && res['status'] === 'approved') {
            push({ kind: 'approved', proposalId: String(args['proposal_id'] ?? '') })
          } else if ((name === 'plan_my_day' || name === 'get_daily_briefing') && Array.isArray(res['cards']) && (res['cards'] as unknown[]).length > 0) {
            push({ kind: 'briefing', day: String(res['day'] ?? ''), rows: res['cards'] as BriefingRow[] })
          }
          if (['add_routine_task', 'propose_automations', 'approve_proposal', 'plan_my_day', 'write_departure_blocks'].includes(name)) onDataChanged()
          break
        }
        case 'confirm':
          setItems((xs) => {
            if (xs.some((x) => x.kind === 'confirm' && x.pending.callId === ev.pending.callId)) return xs
            return [...closeStreaming(xs), { id: nextId(), kind: 'confirm', pending: ev.pending }]
          })
          break
        case 'error':
          push({ kind: 'error', text: ev.message })
          break
        case 'done':
          setItems(closeStreaming)
          setBusy(false)
          onDataChanged()
          break
      }
    },
    [push, onDataChanged],
  )

  const send = useCallback(
    async (text: string) => {
      const t = text.trim()
      const photo = attachment
      if ((!t && !photo) || busy) return
      if (!sess.current.ready) {
        try {
          await sess.current.init()
          setOffline(false)
        } catch {
          setOffline(true)
          push({ kind: 'error', text: 'Chat is offline — is GOOGLE_API_KEY set on the server?' })
          return
        }
      }
      setInput('')
      setAttachment(null)
      stickToBottom.current = true
      push({ kind: 'user', text: t || (photo ? 'Here is a photo of my routine.' : ''), imageUrl: photo?.previewUrl })
      setBusy(true)
      await sess.current.send(t, onEvent, photo ?? undefined)
    },
    [busy, attachment, onEvent, push],
  )

  // Photo → downscaled JPEG (≤1280px) → base64. Handwritten lists, boletos,
  // school notes: Gemini reads the pixels directly, no OCR step.
  const attachFile = useCallback(async (file: File) => {
    const url = URL.createObjectURL(file)
    const img = new Image()
    await new Promise<void>((ok, fail) => {
      img.onload = () => ok()
      img.onerror = () => fail(new Error('bad image'))
      img.src = url
    })
    const scale = Math.min(1, 1280 / Math.max(img.width, img.height))
    const canvas = document.createElement('canvas')
    canvas.width = Math.round(img.width * scale)
    canvas.height = Math.round(img.height * scale)
    canvas.getContext('2d')?.drawImage(img, 0, 0, canvas.width, canvas.height)
    const dataUrl = canvas.toDataURL('image/jpeg', 0.85)
    setAttachment({ mimeType: 'image/jpeg', data: dataUrl.split(',')[1], previewUrl: url })
  }, [])

  const answer = useCallback(
    async (item: Extract<Item, { kind: 'confirm' }>, yes: boolean) => {
      setItems((xs) =>
        xs.map((x) =>
          x.id === item.id
            ? ({ id: x.id, kind: 'activity', label: yes ? 'Approved by you' : 'Declined by you', status: 'done', tool: 'you' } as Item)
            : x,
        ),
      )
      setBusy(true)
      await sess.current.confirm(item.pending, yes, onEvent)
    },
    [onEvent],
  )

  useImperativeHandle(
    ref,
    () => ({
      send: (text: string) => void send(text),
      notify: (n: Notice) => {
        if (n.kind === 'purchase') push({ kind: 'purchase', title: n.title, total: n.total, mandateRef: n.mandateRef })
      },
    }),
    [send, push],
  )

  // textarea grows 1→4 rows; measure only while visible (display:none → scrollHeight 0)
  useEffect(() => {
    const el = textarea.current
    if (!el || !open) return
    if (!input) {
      el.style.height = ''
      return
    }
    el.style.height = '0px'
    el.style.height = Math.min(el.scrollHeight, 112) + 'px'
  }, [input, open])

  const approvedExecutable = proposals.find((p) => p.status === 'approved' && p.executable)
  const prompts = approvedExecutable
    ? [`__consent:${approvedExecutable.id}`, ...BASE_PROMPTS.slice(0, 2)]
    : proposals.some((p) => p.status === 'proposed')
      ? [BASE_PROMPTS[2], 'Approve the best one', BASE_PROMPTS[0]]
      : BASE_PROMPTS

  const runPrompt = (p: string) => {
    if (p.startsWith('__consent:')) onOpenConsent(p.slice('__consent:'.length))
    else void send(p)
  }

  const lastItem = items[items.length - 1]
  const showTyping = busy && !(lastItem?.kind === 'agent' && lastItem.streaming) && !(lastItem?.kind === 'activity' && lastItem.status === 'running')

  return (
    <>
      {/* mobile backdrop */}
      {open && <div className="fixed inset-0 z-30 bg-[rgba(36,35,33,0.25)] lg:hidden" onClick={onClose} />}

      <aside
        className={`${open ? 'flex' : 'hidden'} flex-col overflow-hidden bg-surface rounded-t-card lg:rounded-card border-subtle shadow-[var(--shadow-float)] fixed inset-x-0 bottom-0 z-40 h-[88vh] lg:inset-x-auto lg:bottom-auto lg:top-[5.5rem] lg:right-[max(1.5rem,calc((100vw-1480px)/2+1.5rem))] lg:w-[400px] xl:w-[420px] lg:h-[calc(100vh-6.5rem)] lg:z-20`}
        aria-label="Automate.me agent"
      >
        {/* header */}
        <div className="px-4 py-3 flex items-center gap-3 border-b border-[rgba(36,35,33,0.06)] bg-surface/95 backdrop-blur">
          <span className="w-8 h-8 rounded-full bg-sun-soft flex items-center justify-center text-base" aria-hidden>
            ☀️
          </span>
          <div className="min-w-0">
            <div className="font-medium leading-tight">Automate.me agent</div>
            <div className="text-[11px] text-ink-tertiary leading-tight flex items-center gap-1.5">
              <span
                className="w-1.5 h-1.5 rounded-full"
                style={{ background: offline ? '#b3261e' : busy ? '#e5b73c' : '#2e7d32' }}
              />
              {offline ? 'offline — dashboard still works' : busy ? 'working…' : '3 agents · gemini-3.5-flash · live'}
            </div>
          </div>
          <button
            onClick={onClose}
            className="ml-auto rounded-pill px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface-subtle cursor-pointer"
            aria-label="Hide agent panel"
          >
            Hide
          </button>
        </div>

        {/* timeline */}
        <div
          ref={scroller}
          onScroll={(e) => {
            const el = e.currentTarget
            stickToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80
          }}
          className="chat-scroll flex-1 overflow-y-auto px-4 py-4 space-y-2"
        >
          {items.length === 0 && (
            <div className="chat-item">
              <div className="text-sm bg-surface-raised border-subtle rounded-2xl rounded-bl-md px-3.5 py-3">
                <p className="m-0">
                  Tell me one routine that eats your time. I’ll price it, then find what pays for itself fastest.
                </p>
              </div>
              <div className="mt-3 flex flex-wrap gap-1.5">
                {BASE_PROMPTS.map((q) => (
                  <button
                    key={q}
                    onClick={() => void send(q)}
                    className="text-left text-xs rounded-pill border-subtle bg-surface-raised px-3 py-1.5 cursor-pointer hover:bg-sun-soft/40 transition-colors"
                  >
                    {q}
                  </button>
                ))}
              </div>
            </div>
          )}

          {items.map((it, i) => (
            <Row key={it.id} item={it} prev={items[i - 1]} proposals={proposals} onAnswer={answer} onSend={send} onOpenConsent={onOpenConsent} onShowLedger={onShowLedger} onShowBriefing={onShowBriefing} />
          ))}

          {showTyping && (
            <div className="chat-item dot-typing text-ink-tertiary text-sm pl-1" aria-label="agent is thinking">
              <span>●</span>
              <span>●</span>
              <span>●</span>
            </div>
          )}
        </div>

        {/* composer */}
        <div className="border-t border-[rgba(36,35,33,0.06)] bg-surface/95 backdrop-blur p-3 space-y-2">
          {items.length > 0 && !busy && (
            <div className="flex gap-1.5 overflow-x-auto chat-scroll pb-0.5">
              {prompts.map((p) => (
                <button
                  key={p}
                  onClick={() => runPrompt(p)}
                  className={`shrink-0 text-xs rounded-pill px-3 py-1.5 cursor-pointer transition-colors ${
                    p.startsWith('__consent:')
                      ? 'bg-ink text-white hover:bg-[#3a3835]'
                      : 'border-subtle bg-surface-raised hover:bg-sun-soft/40'
                  }`}
                >
                  {p.startsWith('__consent:') ? 'Review & sign the purchase →' : p}
                </button>
              ))}
            </div>
          )}
          {attachment && (
            <div className="flex items-center gap-2 text-xs text-ink-secondary">
              <img src={attachment.previewUrl} alt="" className="h-12 w-12 object-cover rounded-lg border-subtle" />
              <span>Photo attached — I’ll read every item on it.</span>
              <button type="button" onClick={() => setAttachment(null)} className="ml-auto rounded-pill px-2 py-1 hover:bg-surface-subtle cursor-pointer" aria-label="Remove photo">
                ✕
              </button>
            </div>
          )}
          <form
            className="flex items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              void send(input)
            }}
          >
            <input
              ref={fileInput}
              type="file"
              accept="image/*"
              capture="environment"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0]
                if (f) void attachFile(f)
                e.target.value = ''
              }}
            />
            <button
              type="button"
              onClick={() => fileInput.current?.click()}
              disabled={busy}
              title="Photo of a list, calendar, boletos or note"
              aria-label="Attach a photo"
              className="rounded-full w-10 h-10 shrink-0 border-subtle bg-surface-raised cursor-pointer hover:bg-sun-soft/40 disabled:opacity-40 text-base"
            >
              📷
            </button>
            <textarea
              ref={textarea}
              value={input}
              rows={1}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  void send(input)
                }
              }}
              placeholder="Describe a routine…"
              className="flex-1 resize-none rounded-2xl bg-surface-raised border-subtle px-4 py-2.5 text-sm outline-none leading-5"
            />
            {busy ? (
              <button
                type="button"
                onClick={() => sess.current.stop()}
                className="rounded-pill border-subtle bg-surface-raised px-4 py-2.5 text-sm cursor-pointer hover:bg-surface-subtle"
              >
                Stop
              </button>
            ) : (
              <button
                type="submit"
                disabled={!input.trim() && !attachment}
                className="rounded-pill bg-ink text-white px-4 py-2.5 text-sm cursor-pointer disabled:opacity-40 font-medium"
              >
                Send
              </button>
            )}
          </form>
        </div>
      </aside>
    </>
  )
}

function Row({
  item,
  prev,
  proposals,
  onAnswer,
  onSend,
  onOpenConsent,
  onShowLedger,
  onShowBriefing,
}: {
  item: Item
  prev?: Item
  proposals: Proposal[]
  onAnswer: (item: Extract<Item, { kind: 'confirm' }>, yes: boolean) => void
  onSend: (text: string) => void
  onOpenConsent: (id: string) => void
  onShowLedger: () => void
  onShowBriefing: () => void
}) {
  switch (item.kind) {
    case 'user':
      return (
        <div className="chat-item flex justify-end">
          <div className="max-w-[85%] bg-sun-soft/70 rounded-2xl rounded-br-md px-3.5 py-2 text-sm whitespace-pre-wrap">
            {item.imageUrl && (
              <img src={item.imageUrl} alt="attached photo" className="block max-h-40 rounded-xl mb-2 border border-sun-deep/30" />
            )}
            {item.text}
          </div>
        </div>
      )
    case 'agent': {
      const meta = agentMeta(item.author)
      const showHeader = !(prev?.kind === 'agent' && prev.author === item.author)
      return (
        <div className="chat-item">
          {showHeader && (
            <div className="flex items-center gap-1.5 text-[11px] text-ink-tertiary mb-1 pl-1">
              <span className="w-1.5 h-1.5 rounded-full" style={{ background: meta.color }} />
              <span className="font-medium text-ink-secondary">{meta.label}</span>
              {meta.role && <span>· {meta.role}</span>}
            </div>
          )}
          <div className="max-w-[92%] bg-surface-raised border-subtle rounded-2xl rounded-bl-md px-3.5 py-2.5 text-sm chat-md">
            <Markdown>{item.text}</Markdown>
            {item.streaming && <span className="cursor" aria-hidden />}
          </div>
        </div>
      )
    }
    case 'activity':
      return (
        <div className="chat-item flex items-center gap-2 pl-1 text-xs text-ink-secondary py-0.5">
          {item.status === 'running' ? (
            <span className="ring shrink-0" aria-hidden />
          ) : item.tool === 'you' ? (
            <span className="w-3 h-3 rounded-full bg-sun-deep shrink-0 flex items-center justify-center text-[8px] text-ink" aria-hidden>
              ✓
            </span>
          ) : (
            <span className="w-3 h-3 rounded-full bg-success-tint text-success shrink-0 flex items-center justify-center text-[9px]" aria-hidden>
              ✓
            </span>
          )}
          <span className={item.status === 'running' ? 'animate-pulse' : ''}>{item.label}</span>
        </div>
      )
    case 'task':
      return (
        <Card tag={item.updated ? 'task updated' : 'task saved'}>
          <div className="flex items-baseline justify-between gap-3">
            <span className="font-medium text-sm">{item.name}</span>
            <span className="tabular text-sm">
              <span className="text-danger font-semibold">{item.cost}</span>
              <span className="text-ink-tertiary">/mo leaking</span>
            </span>
          </div>
        </Card>
      )
    case 'proposals': {
      const top = item.rows.slice(0, 3)
      return (
        <Card tag="ranked by monthly savings · deterministic engine">
          <div className="divide-y divide-[rgba(36,35,33,0.06)]">
            {top.map((r) => {
              const live = proposals.find((p) => p.id === r.proposal_id)
              const status = live?.status ?? 'proposed'
              return (
                <div key={r.proposal_id} className="py-2 first:pt-0 last:pb-0 flex items-center gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium truncate">{r.recipe}</div>
                    <div className="text-xs text-ink-secondary">
                      <span className="text-success font-medium tabular">{r.net_monthly}</span>/mo · pays back{' '}
                      {r.payback_months.startsWith('immediate') ? 'immediately' : `in ${r.payback_months}`}
                    </div>
                  </div>
                  {status === 'proposed' ? (
                    <button
                      onClick={() => onSend(`Approve "${r.recipe}" (proposal ${r.proposal_id})`)}
                      className="shrink-0 rounded-pill bg-ink text-white text-xs px-3 py-1.5 cursor-pointer hover:bg-[#3a3835]"
                    >
                      Approve
                    </button>
                  ) : status === 'approved' && live?.executable ? (
                    <button
                      onClick={() => onOpenConsent(r.proposal_id)}
                      className="shrink-0 rounded-pill bg-sun text-ink text-xs px-3 py-1.5 cursor-pointer font-medium"
                    >
                      Sign →
                    </button>
                  ) : (
                    <span className="shrink-0 text-[11px] rounded-pill px-2.5 py-1 bg-success-tint text-success">{status === 'executed' ? 'done' : status}</span>
                  )}
                </div>
              )
            })}
          </div>
          {item.rows.length > 3 && (
            <div className="text-[11px] text-ink-tertiary mt-2">+{item.rows.length - 3} more on the dashboard</div>
          )}
        </Card>
      )
    }
    case 'approved': {
      const p = proposals.find((x) => x.id === item.proposalId)
      const title = p?.recipe_title ?? item.proposalId
      const done = p?.status === 'executed'
      return (
        <Card tag="approved" tone="sun">
          <div className="flex items-center gap-3">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium truncate">{title}</div>
              <div className="text-xs text-ink-secondary">
                {done
                  ? 'Purchased via AP2 — receipts on the ledger.'
                  : p?.executable
                    ? 'The agent cannot sign payments. You do, on the consent screen.'
                    : 'Guided recipe — ask the agent for the steps.'}
              </div>
            </div>
            {!done && p?.executable && (
              <button
                onClick={() => onOpenConsent(item.proposalId)}
                className="shrink-0 rounded-pill bg-ink text-white text-xs px-3 py-1.5 cursor-pointer font-medium pop"
              >
                Review & sign
              </button>
            )}
            {!done && p && !p.executable && (
              <button
                onClick={() => onSend(`Give me the concrete steps to set up "${title}".`)}
                className="shrink-0 rounded-pill border-subtle bg-surface-raised text-xs px-3 py-1.5 cursor-pointer hover:bg-sun-soft/40"
              >
                Get the steps
              </button>
            )}
          </div>
        </Card>
      )
    }
    case 'briefing':
      return (
        <Card tag={`daily briefing · ${item.day}`}>
          <div className="divide-y divide-[rgba(36,35,33,0.06)]">
            {item.rows.map((r) => (
              <div key={r.card_id} className="py-2 first:pt-0 last:pb-0 flex items-center gap-3">
                <div className="tabular text-sm font-semibold w-12 shrink-0" style={{ fontFamily: 'var(--font-display)' }}>{r.leave_at}</div>
                <div className="min-w-0 flex-1">
                  <div className="text-sm truncate">{r.event}</div>
                  <div className="text-xs text-ink-secondary truncate">
                    {r.event_at} · {r.route}
                    {r.traffic_minutes > 0 && <> · <span className="text-danger">+{r.traffic_minutes} min {r.traffic_cost}</span></>}
                  </div>
                </div>
                {r.flood_risk !== 'none' && (
                  <span
                    className="shrink-0 text-[10px] rounded-pill px-2 py-0.5"
                    style={r.flood_risk === 'alert' ? { background: '#fce8e6', color: '#b3261e' } : { background: '#fbf0cc', color: '#8a6d0b' }}
                    title={r.flood_detail}
                  >
                    {r.flood_risk === 'alert' ? 'flood alert' : 'flood history'}
                  </span>
                )}
              </div>
            ))}
          </div>
          <button onClick={onShowBriefing} className="mt-2 text-xs text-ink-secondary hover:text-ink cursor-pointer bg-transparent">
            open the Briefing →
          </button>
        </Card>
      )
    case 'purchase':
      return (
        <Card tag="purchased · AP2 v0.2 · simulated" tone="success">
          <div className="flex items-center gap-3">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium truncate">{item.title}</div>
              <div className="text-xs text-ink-secondary">
                <span className="tabular">{brl(item.total)}</span> · 2 mandates signed by you · 2 receipts verified · ref{' '}
                <code className="text-[11px]">{item.mandateRef}</code>
              </div>
            </div>
            <button onClick={onShowLedger} className="shrink-0 rounded-pill border-subtle bg-surface-raised text-xs px-3 py-1.5 cursor-pointer hover:bg-sun-soft/40">
              Ledger →
            </button>
          </div>
        </Card>
      )
    case 'confirm':
      return (
        <div className="chat-item pop bg-sun-soft/60 border border-sun-deep/40 rounded-2xl px-3.5 py-3 text-sm">
          <div className="font-medium mb-0.5">Your call</div>
          <div className="text-xs text-ink-secondary mb-2.5">{describeConfirmation(item.pending, proposals)}</div>
          <div className="flex gap-2">
            <button onClick={() => onAnswer(item, true)} className="rounded-pill bg-ink text-white px-3.5 py-1.5 text-xs cursor-pointer font-medium">
              Approve
            </button>
            <button onClick={() => onAnswer(item, false)} className="rounded-pill border-subtle bg-surface-raised px-3.5 py-1.5 text-xs cursor-pointer">
              Decline
            </button>
          </div>
        </div>
      )
    case 'error':
      return <div className="chat-item bg-danger-tint text-danger rounded-2xl px-3.5 py-2 text-xs">{item.text}</div>
  }
}

function Card({ tag, tone = 'plain', children }: { tag: string; tone?: 'plain' | 'sun' | 'success'; children: React.ReactNode }) {
  const bg = tone === 'sun' ? 'bg-sun-soft/40' : tone === 'success' ? 'bg-success-tint' : 'bg-surface-raised'
  return (
    <div className={`chat-item pop ${bg} border-subtle rounded-2xl px-3.5 py-3`}>
      <div className="text-[10px] uppercase tracking-[0.08em] text-ink-tertiary mb-1.5">{tag}</div>
      {children}
    </div>
  )
}
