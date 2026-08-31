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
import type { Activity } from '../lib/activity'

// The right rail: what the agent has been doing, and the way you talk to it.
// Derived activity seeds the timeline; live tool calls, hand-overs and
// actionable result cards append to the same column as you converse.

export type Screen = 'pnl' | 'live' | 'briefing' | 'proposals' | 'ledger' | 'guardian'

export interface AgentHandle {
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
  | { id: number; kind: 'approved'; proposalId: string; purchased?: boolean; total?: string }
  | { id: number; kind: 'briefing'; day: string; rows: BriefingRow[] }
  | { id: number; kind: 'purchase'; title: string; total: number; mandateRef: string }
  | { id: number; kind: 'confirm'; pending: PendingConfirmation }
  | { id: number; kind: 'error'; text: string }

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
      return 'Planning the day · one route worker per appointment'
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
  if (o.name === 'write_departure_blocks') return 'Write the “Leave at” blocks to your calendar?'
  return o.name.replace(/_/g, ' ')
}

function closeStreaming(items: Item[]): Item[] {
  const last = items[items.length - 1]
  if (last?.kind === 'agent' && last.streaming) return [...items.slice(0, -1), { ...last, streaming: false }]
  return items
}

const BASE_PROMPTS = ['What should I automate first?', 'Brief me on my day', 'I wash dishes an hour a day']

function MicGlyph({ size = 15 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden>
      <rect x="9" y="2" width="6" height="11" rx="3" />
      <path d="M5 11a7 7 0 0 0 14 0M12 18v3" />
    </svg>
  )
}

const TONE = {
  done: '#3F7D58',
  waiting: '#BC9A75',
  attention: '#C9553D',
} as const

export function AgentRail({
  ref,
  proposals,
  activity,
  onDataChanged,
  onOpenConsent,
  onGo,
  onClose,
}: {
  ref?: Ref<AgentHandle>
  proposals: Proposal[]
  activity: Activity[]
  onDataChanged: () => void
  onOpenConsent: (proposalId: string) => void
  onGo: (s: Screen) => void
  onClose?: () => void
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
  const callArgs = useRef<Record<string, Record<string, unknown>>>({})
  const seenResults = useRef<Set<string>>(new Set())

  useEffect(() => {
    if (!sess.current.ready && !offline) sess.current.init().catch(() => setOffline(true))
  }, [offline])

  useEffect(() => {
    if (stickToBottom.current && items.length) {
      scroller.current?.scrollTo({ top: scroller.current.scrollHeight, behavior: 'smooth' })
    }
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
            return [
              ...closeStreaming(xs),
              { id: nextId(), kind: 'activity', label: activityLabel(ev.call), status: 'running', tool: ev.call.name, callId },
            ]
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
            push({
              kind: 'approved',
              proposalId: String(args['proposal_id'] ?? ''),
              // Under the signed envelope the agent already bought it; asking
              // for a signature now would ask twice for one purchase.
              purchased: !!res['purchased_autonomously'],
              total: typeof res['purchase_total'] === 'string' ? res['purchase_total'] : undefined,
            })
          } else if (
            (name === 'plan_my_day' || name === 'get_daily_briefing') &&
            Array.isArray(res['cards']) &&
            (res['cards'] as unknown[]).length > 0
          ) {
            push({ kind: 'briefing', day: String(res['day'] ?? ''), rows: res['cards'] as BriefingRow[] })
          }
          if (
            ['add_routine_task', 'propose_automations', 'approve_proposal', 'plan_my_day', 'write_departure_blocks'].includes(name)
          )
            onDataChanged()
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
    setAttachment({ mimeType: 'image/jpeg', data: canvas.toDataURL('image/jpeg', 0.85).split(',')[1], previewUrl: url })
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

  // Size the composer to what it holds — including the placeholder, which
  // wraps to two lines in a 348px rail and used to be cut in half by the
  // one-row height. scrollHeight reflects the placeholder when the field is
  // empty, so the same measurement covers both states.
  useEffect(() => {
    const el = textarea.current
    if (!el) return
    const fit = () => {
      el.style.height = '0px'
      el.style.height = Math.min(el.scrollHeight, 112) + 'px'
    }
    fit()
    // A narrower rail rewraps the text under the composer's feet, so measure
    // again whenever the field itself changes width.
    const ro = new ResizeObserver(fit)
    ro.observe(el)
    return () => ro.disconnect()
  }, [input])

  const approvedExecutable = proposals.find((p) => p.status === 'approved' && p.executable)
  const prompts = approvedExecutable
    ? [`__consent:${approvedExecutable.id}`, BASE_PROMPTS[1]]
    : proposals.some((p) => p.status === 'proposed')
      ? [BASE_PROMPTS[0], 'Approve the best one', BASE_PROMPTS[1]]
      : BASE_PROMPTS

  const runPrompt = (p: string) =>
    p.startsWith('__consent:') ? onOpenConsent(p.slice('__consent:'.length)) : void send(p)

  const last = items[items.length - 1]
  const showTyping =
    busy && !(last?.kind === 'agent' && last.streaming) && !(last?.kind === 'activity' && last.status === 'running')

  return (
    <aside className="flex flex-col overflow-hidden bg-surface-warm border-l border-line h-full" aria-label="Agent activity">
      <header className="px-5 pt-5 pb-3 shrink-0 flex items-start gap-3">
        <div className="min-w-0 flex-1">
        <h2 className="m-0 text-[17px] font-semibold leading-tight">Agent activity</h2>
        <p className="m-0 mt-0.5 text-xs text-ink-tertiary flex items-center gap-1.5">
          <span
            className="w-1.5 h-1.5 rounded-full"
            style={{ background: offline ? TONE.attention : busy ? TONE.waiting : TONE.done }}
          />
          {offline
            ? 'chat offline — the dashboard still works'
            : busy
              ? 'working…'
              : `${activity.length} action${activity.length === 1 ? '' : 's'} today · live`}
        </p>
        </div>
        {onClose && (
          <button
            onClick={onClose}
            className="xl:hidden rounded-pill px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface-sunk cursor-pointer shrink-0"
          >
            Hide
          </button>
        )}
      </header>

      <div
        ref={scroller}
        onScroll={(e) => {
          const el = e.currentTarget
          stickToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80
        }}
        className="chat-scroll flex-1 overflow-y-auto px-5 pb-4 space-y-2.5"
      >
        {activity.map((a) => (
          <ActivityRow key={a.id} a={a} onGo={onGo} />
        ))}

        {activity.length === 0 && items.length === 0 && (
          <p className="text-sm text-ink-secondary leading-relaxed">
            Nothing yet. Tell me a routine that eats your time — or drop a photo of your list — and I’ll price it.
          </p>
        )}

        {items.length > 0 && (
          <div className="pt-3 mt-1 border-t border-line flex items-center gap-2">
            <span className="scap">this conversation</span>
          </div>
        )}

        {items.map((it, i) => (
          <Row
            key={it.id}
            item={it}
            prev={items[i - 1]}
            proposals={proposals}
            onAnswer={answer}
            onSend={send}
            onOpenConsent={onOpenConsent}
            onGo={onGo}
          />
        ))}

        {showTyping && (
          <div className="chat-item dot-typing text-ink-tertiary text-sm pl-1" aria-label="agent is thinking">
            <span>●</span>
            <span>●</span>
            <span>●</span>
          </div>
        )}
      </div>

      <div className="shrink-0 px-4 pb-4 pt-3 space-y-2 bg-surface-warm">
        {!busy && (
          <div className="flex gap-1.5 overflow-x-auto chat-scroll pb-0.5">
            {prompts.map((p) => (
              <button
                key={p}
                onClick={() => runPrompt(p)}
                className={`shrink-0 text-xs rounded-pill px-3 py-1.5 cursor-pointer transition-colors ${
                  p.startsWith('__consent:')
                    ? 'bg-gold text-teal font-medium hover:brightness-105'
                    : 'border border-line bg-surface hover:bg-gold-tint'
                }`}
              >
                {p.startsWith('__consent:') ? 'Review & authorize →' : p}
              </button>
            ))}
          </div>
        )}

        {attachment && (
          <div className="flex items-center gap-2 text-xs text-ink-secondary">
            <img src={attachment.previewUrl} alt="" className="h-11 w-11 object-cover rounded-lg border border-line" />
            <span>Photo attached — I’ll read every item on it.</span>
            <button
              type="button"
              onClick={() => setAttachment(null)}
              className="ml-auto rounded-pill px-2 py-1 hover:bg-surface-sunk cursor-pointer"
              aria-label="Remove photo"
            >
              ✕
            </button>
          </div>
        )}

        <form
          className="flex items-end gap-2 bg-surface border border-line rounded-[22px] px-2 py-1.5"
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
            onClick={() => onGo('live')}
            title="Talk to the agent out loud"
            aria-label="Open the live call"
            className="rounded-full w-9 h-9 shrink-0 cursor-pointer flex items-center justify-center hover:bg-gold-tint text-ink-secondary transition-colors"
          >
            <MicGlyph />
          </button>
          <button
            type="button"
            onClick={() => fileInput.current?.click()}
            disabled={busy}
            title="Photo of a list, calendar, boletos or note"
            aria-label="Attach a photo"
            className="rounded-full w-9 h-9 shrink-0 cursor-pointer hover:bg-gold-tint disabled:opacity-40 text-base leading-none"
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
            className="flex-1 min-w-0 resize-none bg-transparent px-1 py-2 text-sm outline-none leading-5 placeholder:text-ink-tertiary"
          />
          {busy ? (
            <button
              type="button"
              onClick={() => sess.current.stop()}
              className="rounded-pill border border-line bg-surface px-3.5 py-2 text-xs cursor-pointer hover:bg-surface-sunk shrink-0"
            >
              Stop
            </button>
          ) : (
            <button
              type="submit"
              disabled={!input.trim() && !attachment}
              aria-label="Send"
              className="rounded-full w-9 h-9 shrink-0 bg-teal text-white cursor-pointer disabled:opacity-35 flex items-center justify-center"
            >
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
                <path d="M5 12h13M12 5l7 7-7 7" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </button>
          )}
        </form>
      </div>
    </aside>
  )
}

function ActivityRow({ a, onGo }: { a: Activity; onGo: (s: Screen) => void }) {
  return (
    <div className="flex gap-2.5 rise">
      <span className="w-1.5 h-1.5 rounded-full shrink-0 mt-[7px]" style={{ background: TONE[a.tone] }} aria-hidden />
      <div className="min-w-0">
        <div className="text-[13.5px] font-medium leading-snug">{a.title}</div>
        <div className="text-[11.5px] text-ink-tertiary leading-snug">{a.meta}</div>
        {a.action && (
          <button
            onClick={() => onGo(a.action!.go)}
            className="mt-0.5 text-[11.5px] font-medium text-gold-deep hover:text-teal cursor-pointer bg-transparent"
          >
            {a.action.label} →
          </button>
        )}
      </div>
    </div>
  )
}

function Row({
  item,
  prev,
  proposals,
  onAnswer,
  onSend,
  onOpenConsent,
  onGo,
}: {
  item: Item
  prev?: Item
  proposals: Proposal[]
  onAnswer: (item: Extract<Item, { kind: 'confirm' }>, yes: boolean) => void
  onSend: (text: string) => void
  onOpenConsent: (id: string) => void
  onGo: (s: Screen) => void
}) {
  switch (item.kind) {
    case 'user':
      return (
        <div className="chat-item flex justify-end">
          <div className="max-w-[88%] bg-teal text-white rounded-2xl rounded-br-md px-3.5 py-2 text-sm whitespace-pre-wrap">
            {item.imageUrl && (
              <img src={item.imageUrl} alt="attached photo" className="block max-h-36 rounded-xl mb-2 border border-white/20" />
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
            <div className="flex items-center gap-1.5 text-[11px] text-ink-tertiary mb-1 pl-0.5">
              <span className="w-1.5 h-1.5 rounded-full" style={{ background: meta.color }} />
              <span className="font-medium text-ink-secondary">{meta.label}</span>
              {meta.role && <span>· {meta.role}</span>}
            </div>
          )}
          <div className="bg-surface border border-line rounded-2xl rounded-bl-md px-3.5 py-2.5 text-sm chat-md">
            <Markdown>{item.text}</Markdown>
            {item.streaming && <span className="cursor" aria-hidden />}
          </div>
        </div>
      )
    }
    case 'activity':
      return (
        <div className="chat-item flex items-center gap-2 pl-0.5 text-xs text-ink-secondary py-0.5">
          {item.status === 'running' ? (
            <span className="ring shrink-0" aria-hidden />
          ) : (
            <span
              className="w-3 h-3 rounded-full shrink-0 flex items-center justify-center text-[9px] text-white"
              style={{ background: item.tool === 'you' ? TONE.waiting : TONE.done }}
              aria-hidden
            >
              ✓
            </span>
          )}
          <span className={item.status === 'running' ? 'animate-pulse' : ''}>{item.label}</span>
        </div>
      )
    case 'task':
      return (
        <Card tag={item.updated ? 'routine updated' : 'routine saved'}>
          <div className="flex items-baseline justify-between gap-3">
            <span className="font-medium text-sm">{item.name}</span>
            <span className="tabular text-sm">
              <span className="text-alert font-semibold">{item.cost}</span>
              <span className="text-ink-tertiary">/mo</span>
            </span>
          </div>
        </Card>
      )
    case 'proposals': {
      const top = item.rows.slice(0, 3)
      return (
        <Card tag="ranked by monthly savings · Value Engine">
          <div className="divide-y divide-[#E8E2D6]">
            {top.map((r) => {
              const live = proposals.find((p) => p.id === r.proposal_id)
              const status = live?.status ?? 'proposed'
              return (
                <div key={r.proposal_id} className="py-2 first:pt-0 last:pb-0 flex items-center gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium truncate">{r.recipe}</div>
                    <div className="text-xs text-ink-secondary">
                      <span className="text-positive font-medium tabular">{r.net_monthly}</span>/mo · pays back{' '}
                      {r.payback_months.startsWith('immediate') ? 'immediately' : `in ${r.payback_months}`}
                    </div>
                  </div>
                  {status === 'proposed' ? (
                    <button
                      onClick={() => onSend(`Approve "${r.recipe}" (proposal ${r.proposal_id})`)}
                      className="shrink-0 rounded-pill bg-teal text-white text-xs px-3 py-1.5 cursor-pointer hover:bg-teal-soft"
                    >
                      Approve
                    </button>
                  ) : status === 'approved' && live?.executable ? (
                    <button
                      onClick={() => onOpenConsent(r.proposal_id)}
                      className="shrink-0 rounded-pill bg-gold text-teal text-xs px-3 py-1.5 cursor-pointer font-medium"
                    >
                      Sign →
                    </button>
                  ) : (
                    <span className="shrink-0 text-[11px] rounded-pill px-2.5 py-1 bg-positive-tint text-positive">
                      {status === 'executed' ? 'done' : status}
                    </span>
                  )}
                </div>
              )
            })}
          </div>
          {item.rows.length > 3 && (
            <button onClick={() => onGo('proposals')} className="text-[11px] text-gold-deep mt-2 cursor-pointer bg-transparent">
              +{item.rows.length - 3} more →
            </button>
          )}
        </Card>
      )
    }
    case 'approved': {
      const p = proposals.find((x) => x.id === item.proposalId)
      const title = p?.recipe_title ?? item.proposalId
      const done = item.purchased || p?.status === 'executed'
      return (
        <Card tag="approved" tone="gold">
          <div className="flex items-center gap-3">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium truncate">{title}</div>
              <div className="text-xs text-ink-secondary">
                {done
                  ? item.purchased
                    ? `Bought for ${item.total ?? 'the authorized amount'} under your spending authorization — signed receipt on the ledger.`
                    : 'Purchased via AP2 — receipts on the ledger.'
                  : p?.executable
                    ? 'The agent cannot sign payments. You do, on the consent screen.'
                    : 'Guided recipe — ask for the steps.'}
              </div>
            </div>
            {!done && p?.executable && (
              <button
                onClick={() => onOpenConsent(item.proposalId)}
                className="shrink-0 rounded-pill bg-teal text-white text-xs px-3 py-1.5 cursor-pointer font-medium pop"
              >
                Review & sign
              </button>
            )}
            {!done && p && !p.executable && (
              <button
                onClick={() => onSend(`Give me the concrete steps to set up "${title}".`)}
                className="shrink-0 rounded-pill border border-line bg-surface text-xs px-3 py-1.5 cursor-pointer hover:bg-gold-tint"
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
          <div className="divide-y divide-[#E8E2D6]">
            {item.rows.map((r) => (
              <div key={r.card_id} className="py-2 first:pt-0 last:pb-0 flex items-center gap-3">
                <div className="tabular text-sm font-semibold w-11 shrink-0 display">{r.leave_at}</div>
                <div className="min-w-0 flex-1">
                  <div className="text-sm truncate">{r.event}</div>
                  <div className="text-xs text-ink-secondary truncate">
                    {r.event_at} · {r.route}
                    {r.traffic_minutes > 0 && (
                      <>
                        {' '}
                        · <span className="text-alert">+{r.traffic_minutes} min {r.traffic_cost}</span>
                      </>
                    )}
                  </div>
                </div>
                {r.flood_risk !== 'none' && (
                  <span
                    className="shrink-0 text-[10px] rounded-pill px-2 py-0.5"
                    style={
                      r.flood_risk === 'alert'
                        ? { background: '#FBEDE9', color: '#8A3D2B' }
                        : { background: '#F0E7D9', color: '#8B6B47' }
                    }
                    title={r.flood_detail}
                  >
                    {r.flood_risk === 'alert' ? 'flood alert' : 'flood history'}
                  </span>
                )}
              </div>
            ))}
          </div>
          <button onClick={() => onGo('briefing')} className="mt-2 text-[11px] text-gold-deep cursor-pointer bg-transparent">
            Open the briefing →
          </button>
        </Card>
      )
    case 'purchase':
      return (
        <Card tag="purchased · AP2 v0.2 · simulated" tone="positive">
          <div className="text-sm font-medium">{item.title}</div>
          <div className="text-xs text-ink-secondary mt-0.5">
            <span className="tabular">{brl(item.total)}</span> · 2 mandates signed by you · 2 receipts verified
          </div>
          <button onClick={() => onGo('ledger')} className="mt-1.5 text-[11px] text-gold-deep cursor-pointer bg-transparent">
            Read the receipts →
          </button>
        </Card>
      )
    case 'confirm':
      return (
        <div className="chat-item pop bg-gold-tint border border-[rgba(188,154,117,.4)] rounded-2xl px-3.5 py-3 text-sm">
          <div className="font-medium mb-0.5">Your call</div>
          <div className="text-xs text-ink-secondary mb-2.5">{describeConfirmation(item.pending, proposals)}</div>
          <div className="flex gap-2">
            <button
              onClick={() => onAnswer(item, true)}
              className="rounded-pill bg-teal text-white px-3.5 py-1.5 text-xs cursor-pointer font-medium"
            >
              Approve
            </button>
            <button
              onClick={() => onAnswer(item, false)}
              className="rounded-pill border border-line bg-surface px-3.5 py-1.5 text-xs cursor-pointer"
            >
              Decline
            </button>
          </div>
        </div>
      )
    case 'error':
      return <div className="chat-item bg-alert-tint text-alert-deep rounded-2xl px-3.5 py-2 text-xs">{item.text}</div>
  }
}

function Card({ tag, tone = 'plain', children }: { tag: string; tone?: 'plain' | 'gold' | 'positive'; children: React.ReactNode }) {
  const bg = tone === 'gold' ? 'bg-gold-tint' : tone === 'positive' ? 'bg-positive-tint' : 'bg-surface'
  return (
    <div className={`chat-item pop ${bg} border border-line rounded-2xl px-3.5 py-3`}>
      <div className="scap mb-1.5">{tag}</div>
      {children}
    </div>
  )
}
