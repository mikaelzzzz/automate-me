// adkrest chat client with streaming: list-apps → create session → /run_sse.
// Every ADK event is turned into a small typed StreamEvent so the panel can
// show the agent's activity (tool calls, hand-overs, confirmations) live —
// not just the final text.

export interface FunctionCall {
  id?: string
  name: string
  args?: Record<string, unknown>
}
export interface FunctionResponse {
  id?: string
  name: string
  response: Record<string, unknown>
}
interface GenaiPart {
  text?: string
  inlineData?: { mimeType: string; data: string }
  functionCall?: FunctionCall
  functionResponse?: FunctionResponse
}

/** A photo attached to a message: base64 payload, no data: prefix. */
export interface Attachment {
  mimeType: string
  data: string
  /** object URL for the thumbnail in the transcript */
  previewUrl: string
}
interface GenaiContent {
  role: string
  parts: GenaiPart[] | null
}
interface AdkEvent {
  id?: string
  author?: string
  partial?: boolean
  content?: GenaiContent | null
  actions?: { transferToAgent?: string }
  errorMessage?: string
}

export interface PendingConfirmation {
  callId: string
  hint: string
  original?: FunctionCall
}

export type StreamEvent =
  | { kind: 'text'; author: string; text: string; partial: boolean }
  | { kind: 'call'; author: string; call: FunctionCall }
  | { kind: 'result'; author: string; result: FunctionResponse }
  | { kind: 'confirm'; author: string; pending: PendingConfirmation }
  | { kind: 'transfer'; author: string; to: string }
  | { kind: 'error'; message: string }
  | { kind: 'done' }

export type OnEvent = (ev: StreamEvent) => void

export class ChatSession {
  private appName = ''
  private sessionId = ''
  private userId = 'demo'
  private controller: AbortController | null = null
  ready = false

  async init(): Promise<void> {
    const apps = (await (await fetch('/api/list-apps')).json()) as string[]
    if (!apps?.length) throw new Error('no agent apps available')
    this.appName = apps[0]
    const res = await fetch(`/api/apps/${this.appName}/users/${this.userId}/sessions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}),
    })
    const sess = (await res.json()) as { id: string }
    this.sessionId = sess.id
    this.ready = true
  }

  send(text: string, onEvent: OnEvent, attachment?: Attachment): Promise<void> {
    const parts: GenaiPart[] = []
    if (attachment) parts.push({ inlineData: { mimeType: attachment.mimeType, data: attachment.data } })
    parts.push({ text: text || 'Here is a photo of my routine. Read every item and price it.' })
    return this.stream({ role: 'user', parts }, onEvent)
  }

  confirm(pending: PendingConfirmation, confirmed: boolean, onEvent: OnEvent): Promise<void> {
    return this.stream(
      {
        role: 'user',
        parts: [
          {
            functionResponse: {
              id: pending.callId,
              name: 'adk_request_confirmation',
              response: { confirmed },
            },
          },
        ],
      },
      onEvent,
    )
  }

  /** Abort the in-flight turn (the agent keeps its session state). */
  stop(): void {
    this.controller?.abort()
  }

  private async stream(newMessage: GenaiContent, onEvent: OnEvent): Promise<void> {
    this.controller?.abort()
    const controller = new AbortController()
    this.controller = controller
    try {
      const res = await fetch('/api/run_sse', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
        body: JSON.stringify({
          appName: this.appName,
          userId: this.userId,
          sessionId: this.sessionId,
          newMessage,
          streaming: true,
        }),
        signal: controller.signal,
      })
      if (!res.ok || !res.body) throw new Error(`chat error ${res.status}`)

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      for (;;) {
        const { value, done } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        // SSE frames are separated by a blank line; each carries `data: {json}`.
        let idx: number
        while ((idx = buf.indexOf('\n\n')) >= 0) {
          const frame = buf.slice(0, idx)
          buf = buf.slice(idx + 2)
          for (const line of frame.split('\n')) {
            if (!line.startsWith('data:')) continue
            const raw = line.slice(5).trim()
            if (!raw) continue
            try {
              this.dispatch(JSON.parse(raw) as AdkEvent, onEvent)
            } catch {
              // a malformed frame must not kill the turn
            }
          }
        }
      }
    } catch (e) {
      if ((e as Error).name !== 'AbortError') {
        onEvent({ kind: 'error', message: e instanceof Error ? e.message : String(e) })
      }
    } finally {
      if (this.controller === controller) this.controller = null
      onEvent({ kind: 'done' })
    }
  }

  private dispatch(ev: AdkEvent, onEvent: OnEvent): void {
    const author = ev.author ?? 'agent'
    if (author === 'user') return
    if (ev.errorMessage) onEvent({ kind: 'error', message: ev.errorMessage })
    if (ev.actions?.transferToAgent) onEvent({ kind: 'transfer', author, to: ev.actions.transferToAgent })
    for (const p of ev.content?.parts ?? []) {
      if (p.text) onEvent({ kind: 'text', author, text: p.text, partial: !!ev.partial })
      if (p.functionCall) {
        const fc = p.functionCall
        if (fc.name === 'adk_request_confirmation') {
          const args = fc.args ?? {}
          const orig = args['originalFunctionCall'] as FunctionCall | undefined
          onEvent({
            kind: 'confirm',
            author,
            pending: {
              callId: fc.id ?? '',
              hint: (args['hint'] as string) ?? 'The agent asks for your confirmation.',
              original: orig?.name ? { name: orig.name, args: orig.args } : undefined,
            },
          })
        } else if (fc.name === 'transfer_to_agent') {
          const to = String(fc.args?.['agent_name'] ?? '')
          if (to) onEvent({ kind: 'transfer', author, to })
        } else {
          onEvent({ kind: 'call', author, call: fc })
        }
      }
      if (p.functionResponse) {
        const fr = p.functionResponse
        if (fr.name !== 'transfer_to_agent' && fr.name !== 'adk_request_confirmation') {
          onEvent({ kind: 'result', author, result: fr })
        }
      }
    }
  }
}

/** Human names for the graph's agents — the multi-agent architecture is a feature, show it. */
export const AGENTS: Record<string, { label: string; color: string; role: string }> = {
  automate_me: { label: 'Automate.me', color: '#242321', role: 'orchestrator' },
  routine_analyst: { label: 'Routine Analyst', color: '#a07c12', role: 'captures & prices your routine' },
  automation_advisor: { label: 'Automation Advisor', color: '#2c5fa8', role: 'ranks automations by payback' },
  day_planner: { label: 'Day Planner', color: '#2e7d32', role: 'routes, traffic cost, weather, floods' },
  product_scout: { label: 'Product Scout', color: '#bc9a75', role: 'searches the live web for what to buy' },
}

export function agentMeta(author: string) {
  return AGENTS[author] ?? { label: author.replace(/_/g, ' '), color: '#7a7772', role: '' }
}
