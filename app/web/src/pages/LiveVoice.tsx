import { useCallback, useEffect, useRef, useState } from 'react'
import { LiveVoice as LiveSession, type LiveEvent, type LiveState } from '../lib/live'
import type { Screen } from '../components/AgentRail'

// A live call with the agent. The waveform is the real signal — sampled from
// the microphone on one side and from the model's own audio on the other — so
// you can see who has the floor. Every tool the model reaches for shows up in
// the transcript as it runs.

type Turn =
  | { id: number; kind: 'you'; text: string; open: boolean }
  | { id: number; kind: 'agent'; text: string; open: boolean }
  | { id: number; kind: 'tool'; tool: string; status: 'running' | 'done' | 'error'; detail?: string; via?: string[]; model?: string }

let seq = 1

const AGENT_LABEL: Record<string, string> = {
  automate_me: 'Orchestrator',
  routine_analyst: 'Routine Analyst',
  automation_advisor: 'Automation Advisor',
  day_planner: 'Day Planner',
}

const TOOL_LABEL: Record<string, string> = {
  consult_specialist: 'Asking the specialist graph',
  get_life_pnl: 'Reading your Life P&L',
  add_routine_task: 'Saving the routine and pricing it',
  propose_automations: 'Ranking automations by payback',
  approve_proposal: 'Recording your approval',
  plan_my_day: 'Planning the day · routes, weather, floods',
  get_daily_briefing: 'Reading today’s briefing',
  write_departure_blocks: 'Writing “Leave at” blocks to the calendar',
}

const STATE_LINE: Record<LiveState, string> = {
  idle: 'not connected',
  connecting: 'connecting…',
  listening: 'listening — just talk',
  thinking: 'running your tools',
  speaking: 'Automate.me is speaking',
}

export function LiveVoicePage({ onDataChanged, onGo }: { onDataChanged: () => void; onGo: (s: Screen) => void }) {
  const [state, setState] = useState<LiveState>('idle')
  const [turns, setTurns] = useState<Turn[]>([])
  const [error, setError] = useState('')
  const [muted, setMuted] = useState(false)
  const [typed, setTyped] = useState('')
  const session = useRef<LiveSession | null>(null)
  const scroller = useRef<HTMLDivElement>(null)
  // Named by the server, so the page never claims a model it is not using.
  const [models, setModels] = useState({ voice: 'gemini-3.1-flash-live-preview', reasoning: 'gemini-3.5-flash' })

  useEffect(() => {
    fetch('/app/api/live/session', { method: 'POST' })
      .then((r) => r.json())
      .then((c) => c?.model && setModels({ voice: c.model, reasoning: c.reasoning_model ?? 'gemini-3.5-flash' }))
      .catch(() => {})
  }, [])

  useEffect(() => {
    scroller.current?.scrollTo({ top: scroller.current.scrollHeight, behavior: 'smooth' })
  }, [turns])

  const onEvent = useCallback(
    (ev: LiveEvent) => {
      switch (ev.kind) {
        case 'state':
          if (ev.state) setState(ev.state)
          break
        case 'error':
          setError(ev.text ?? 'voice error')
          break
        case 'user-transcript':
        case 'agent-transcript': {
          const kind = ev.kind === 'user-transcript' ? 'you' : 'agent'
          setTurns((xs) => {
            const last = xs[xs.length - 1]
            if (!ev.text) {
              return last?.kind === kind && last.open ? [...xs.slice(0, -1), { ...last, open: false }] : xs
            }
            if (last?.kind === kind && last.open) return [...xs.slice(0, -1), { ...last, text: last.text + ev.text }]
            return [...xs, { id: seq++, kind, text: ev.text, open: true }]
          })
          break
        }
        case 'tool-start':
          setTurns((xs) => [...xs, { id: seq++, kind: 'tool', tool: ev.tool ?? '', status: 'running' }])
          break
        case 'tool-done': {
          const r = (ev.result ?? {}) as Record<string, unknown>
          const via = Array.isArray(r['handled_by']) ? (r['handled_by'] as string[]) : undefined
          setTurns((xs) => markTool(xs, ev.tool ?? '', 'done', undefined, via, r['reasoned_with'] as string))
          onDataChanged()
          break
        }
        case 'tool-error':
          setTurns((xs) => markTool(xs, ev.tool ?? '', 'error', ev.text))
          break
      }
    },
    [onDataChanged],
  )

  const start = useCallback(async () => {
    setError('')
    const s = new LiveSession(onEvent)
    session.current = s
    await s.start()
  }, [onEvent])

  const stop = useCallback(async () => {
    await session.current?.stop()
    session.current = null
    setMuted(false)
  }, [])

  useEffect(() => () => void session.current?.stop(), [])

  const live = state !== 'idle'

  return (
    <div className="flex flex-col max-w-[880px] mx-auto" style={{ height: 'calc(100vh - 3.5rem)' }}>
      {/* the call band: who has the floor, right now */}
      <header className="shrink-0 rise">
        <div className="flex items-baseline justify-between gap-4 flex-wrap">
          <div>
            <p className="scap m-0">
              Live · voice <b className="font-semibold">{models.voice}</b> · reasoning{' '}
              <b className="font-semibold">{models.reasoning}</b>
            </p>
            <h1 className="display m-0 mt-1 leading-none" style={{ fontSize: 'clamp(1.5rem, 2.4vw, 1.95rem)', fontWeight: 600 }}>
              Talk to your agent
            </h1>
          </div>
          <p className="text-ink-tertiary text-xs m-0 max-w-[350px] leading-relaxed">
            The voice model handles the conversation and hands every judgement to the agent graph on{' '}
            {models.reasoning}. It can capture, price, rank, approve and plan — it cannot buy anything.
          </p>
        </div>

        <section className="mt-3 bg-teal rounded-card px-5 py-4 text-white flex items-center gap-5">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 text-[12.5px]" style={{ color: 'rgba(255,255,255,.72)' }}>
              <span
                className={`w-2 h-2 rounded-full ${state === 'listening' ? 'animate-pulse' : ''}`}
                style={{ background: live ? (state === 'speaking' ? '#BC9A75' : '#3F7D58') : 'rgba(255,255,255,.3)' }}
              />
              {STATE_LINE[state]}
              {muted && live && <span style={{ color: '#BC9A75' }}>· mic held</span>}
            </div>
            <Waveform session={session} state={state} muted={muted} />
          </div>

          <div className="flex flex-col gap-2 shrink-0">
            {!live ? (
              <button
                onClick={() => void start()}
                className="rounded-pill bg-gold text-teal font-semibold px-5 py-2.5 text-sm cursor-pointer hover:brightness-105 inline-flex items-center gap-2 whitespace-nowrap"
              >
                <Mic /> Start talking
              </button>
            ) : (
              <>
                <button
                  onClick={() => {
                    const next = !muted
                    setMuted(next)
                    session.current?.setMuted(next)
                  }}
                  className="rounded-pill px-4 py-2 text-xs font-medium cursor-pointer border whitespace-nowrap"
                  style={{
                    borderColor: 'rgba(255,255,255,.25)',
                    color: '#fff',
                    background: muted ? 'rgba(188,154,117,.25)' : 'transparent',
                  }}
                >
                  {muted ? 'Resume mic' : 'Hold mic'}
                </button>
                <button
                  onClick={() => void stop()}
                  className="rounded-pill bg-alert text-white font-semibold px-4 py-2 text-xs cursor-pointer hover:brightness-110 whitespace-nowrap"
                >
                  End call
                </button>
              </>
            )}
          </div>
        </section>
      </header>

      {error && <div className="mt-3 shrink-0 bg-alert-tint text-alert-deep rounded-xl px-4 py-3 text-sm">{error}</div>}

      {/* the conversation gets the room — this is a chat, not a control panel */}
      <section ref={scroller} className="flex-1 min-h-0 overflow-y-auto chat-scroll space-y-3 py-5">
        {turns.length === 0 && !live && (
          <div className="grid gap-2.5 sm:grid-cols-3 pt-2">
            {[
              ['“I wash dishes an hour a day.”', 'It prices the routine and saves it.'],
              ['“What should I automate first?”', 'It ranks by payback and reads the top three.'],
              ['“Brief me on my day.”', 'Departure times, traffic cost, flood risk.'],
            ].map(([q, a]) => (
              <div key={q} className="bg-surface border border-line rounded-card p-4">
                <div className="text-sm font-medium">{q}</div>
                <div className="text-xs text-ink-tertiary mt-1 leading-relaxed">{a}</div>
              </div>
            ))}
          </div>
        )}
        {turns.length === 0 && live && (
          <p className="text-center text-sm text-ink-tertiary py-10">Say something — the transcript lands here.</p>
        )}
        {turns.map((t) => (
          <TurnRow key={t.id} t={t} onGo={onGo} />
        ))}
      </section>

      {/* composer pinned to the bottom, where a chat keeps it */}
      <form
        className="shrink-0 mb-5 flex items-center gap-2 bg-surface border border-line rounded-pill px-2 py-1.5"
        onSubmit={(e) => {
          e.preventDefault()
          const t = typed.trim()
          if (!t) return
          if (!live) {
            void start()
            return
          }
          setTurns((xs) => [...xs, { id: seq++, kind: 'you', text: t, open: false }])
          session.current?.say(t)
          setTyped('')
        }}
      >
        <button
          type="button"
          onClick={() => (live ? void stop() : void start())}
          aria-label={live ? 'End the call' : 'Start talking'}
          className={`rounded-full w-9 h-9 shrink-0 cursor-pointer flex items-center justify-center transition-colors ${
            live ? 'bg-alert text-white' : 'hover:bg-gold-tint text-ink-secondary'
          } ${state === 'listening' && !muted ? 'mic-live' : ''}`}
        >
          <Mic />
        </button>
        <input
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          placeholder={live ? '…or type, same call' : 'Start talking — or type here'}
          className="flex-1 bg-transparent px-2 py-2 text-sm outline-none placeholder:text-ink-tertiary"
        />
        <button
          type="submit"
          disabled={!typed.trim() && live}
          className="rounded-full w-9 h-9 shrink-0 bg-teal text-white cursor-pointer disabled:opacity-30 flex items-center justify-center"
          aria-label="Send"
        >
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden>
            <path d="M5 12h13M12 5l7 7-7 7" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
      </form>
    </div>
  )
}

function markTool(
  xs: Turn[],
  tool: string,
  status: 'done' | 'error',
  detail?: string,
  via?: string[],
  model?: string,
): Turn[] {
  const out = [...xs]
  for (let i = out.length - 1; i >= 0; i--) {
    const t = out[i]
    if (t.kind === 'tool' && t.tool === tool && t.status === 'running') {
      out[i] = { ...t, status, detail, via, model }
      break
    }
  }
  return out
}

function TurnRow({ t, onGo }: { t: Turn; onGo: (s: Screen) => void }) {
  if (t.kind === 'tool') {
    const done = t.status === 'done'
    const failed = t.status === 'error'
    return (
      <div className="chat-item flex items-center gap-2.5 text-xs text-ink-secondary pl-1">
        {t.status === 'running' ? (
          <span className="ring shrink-0" aria-hidden />
        ) : (
          <span
            className="w-3.5 h-3.5 rounded-full shrink-0 flex items-center justify-center text-[9px] text-white"
            style={{ background: failed ? '#C9553D' : '#3F7D58' }}
            aria-hidden
          >
            {failed ? '!' : '✓'}
          </span>
        )}
        <span>{TOOL_LABEL[t.tool] ?? t.tool.replace(/_/g, ' ')}</span>
        {t.model && (
          <span className="rounded-pill bg-gold-tint text-gold-deep px-2 py-0.5 text-[10.5px] font-medium">{t.model}</span>
        )}
        {t.via && t.via.length > 0 && (
          <span className="text-ink-tertiary">
            via {t.via.map((a) => AGENT_LABEL[a] ?? a.replace(/_/g, ' ')).join(' → ')}
          </span>
        )}
        {failed && <span className="text-alert">{t.detail}</span>}
        {done && (t.tool === 'plan_my_day' || t.tool === 'get_daily_briefing') && (
          <button onClick={() => onGo('briefing')} className="text-gold-deep cursor-pointer bg-transparent">
            open the briefing →
          </button>
        )}
        {done && t.tool === 'propose_automations' && (
          <button onClick={() => onGo('proposals')} className="text-gold-deep cursor-pointer bg-transparent">
            see the proposals →
          </button>
        )}
      </div>
    )
  }
  const you = t.kind === 'you'
  return (
    <div className={`chat-item flex ${you ? 'justify-end' : 'justify-start'}`}>
      <div
        className={`max-w-[76%] rounded-2xl px-4 py-2.5 text-[15px] leading-relaxed ${
          you ? 'bg-teal text-white rounded-br-md' : 'bg-surface border border-line rounded-bl-md'
        }`}
      >
        <div className="text-[10px] uppercase tracking-[0.09em] mb-0.5 opacity-55">{you ? 'you' : 'Automate.me'}</div>
        {t.text}
        {t.open && <span className="cursor" aria-hidden />}
      </div>
    </div>
  )
}

/**
 * Mirrored bars driven by the live analysers. Gold while the agent has the
 * floor, cream while you do — so silence, listening and speaking are three
 * visibly different things.
 */
function Waveform({
  session,
  state,
  muted,
}: {
  session: React.RefObject<LiveSession | null>
  state: LiveState
  muted: boolean
}) {
  const canvas = useRef<HTMLCanvasElement>(null)
  const phase = useRef(0)
  const smoothed = useRef<Float32Array>(new Float32Array(48))

  useEffect(() => {
    const el = canvas.current
    if (!el) return
    const ctx = el.getContext('2d')
    if (!ctx) return
    let raf = 0
    const BINS = 48

    const draw = () => {
      raf = requestAnimationFrame(draw)
      const dpr = window.devicePixelRatio || 1
      const w = el.clientWidth
      const h = el.clientHeight
      if (el.width !== w * dpr || el.height !== h * dpr) {
        el.width = w * dpr
        el.height = h * dpr
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      ctx.clearRect(0, 0, w, h)

      const s = session.current
      const agentTalking = state === 'speaking'
      const side = agentTalking ? 'out' : 'in'
      const raw = s?.active ? s.waveform(side, BINS) : new Float32Array(BINS)
      const colour = agentTalking ? '#BC9A75' : muted ? 'rgba(255,255,255,.28)' : '#F4F0E8'

      // Speech peaks well under full scale; without gain the wave reads as a
      // flat line. Smooth between frames so it breathes instead of flickering.
      const GAIN = 3.4
      for (let i = 0; i < BINS; i++) {
        const target = Math.min(1, (raw[i] ?? 0) * GAIN)
        smoothed.current[i] += (target - smoothed.current[i]) * 0.35
      }

      phase.current += agentTalking ? 0.11 : 0.055
      const gap = 4
      const bw = Math.max(3, (w - gap * (BINS - 1)) / BINS)
      const mid = h / 2

      ctx.fillStyle = colour
      for (let i = 0; i < BINS; i++) {
        // A slow breathing baseline keeps the line alive during silence
        // without pretending there is signal.
        const idle = s?.active ? 0.06 + 0.045 * Math.sin(phase.current + i * 0.4) : 0.015
        const amp = Math.max(idle, smoothed.current[i])
        // taper the ends so the shape reads as one waveform, not a bar chart
        const taper = Math.sin((Math.PI * (i + 0.5)) / BINS) ** 0.45
        const barH = Math.max(3, amp * taper * (h - 6))
        const x = i * (bw + gap)
        ctx.beginPath()
        ctx.roundRect(x, mid - barH / 2, bw, barH, bw / 2)
        ctx.fill()
      }
    }
    raf = requestAnimationFrame(draw)
    return () => cancelAnimationFrame(raf)
  }, [session, state, muted])

  return <canvas ref={canvas} className="w-full h-[76px] mt-2" aria-hidden />
}

function Mic() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden>
      <rect x="9" y="2" width="6" height="11" rx="3" />
      <path d="M5 11a7 7 0 0 0 14 0M12 18v3" />
    </svg>
  )
}
