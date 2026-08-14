// Minimal adkrest chat client: list-apps → create session → run.
// Also speaks the ADK tool-confirmation protocol (adk_request_confirmation).

interface GenaiPart {
  text?: string
  functionCall?: { id?: string; name: string; args?: Record<string, unknown> }
  functionResponse?: { id?: string; name: string; response: Record<string, unknown> }
}
interface GenaiContent {
  role: string
  parts: GenaiPart[] | null
}
interface AdkEvent {
  author?: string
  partial?: boolean
  content?: GenaiContent | null
}

export interface PendingConfirmation {
  callId: string
  hint: string
  original?: { name: string; args?: Record<string, unknown> }
}

export interface ChatTurn {
  text: string
  confirmation?: PendingConfirmation
}

export class ChatSession {
  private appName = ''
  private sessionId = ''
  private userId = 'demo'
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

  async send(text: string): Promise<ChatTurn> {
    return this.run({ role: 'user', parts: [{ text }] })
  }

  async confirm(pending: PendingConfirmation, confirmed: boolean): Promise<ChatTurn> {
    return this.run({
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
    })
  }

  private async run(newMessage: GenaiContent): Promise<ChatTurn> {
    const res = await fetch('/api/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        appName: this.appName,
        userId: this.userId,
        sessionId: this.sessionId,
        newMessage,
        streaming: false,
      }),
    })
    if (!res.ok) throw new Error(`chat error ${res.status}`)
    const events = (await res.json()) as AdkEvent[]

    const texts: string[] = []
    let confirmation: PendingConfirmation | undefined
    for (const ev of events ?? []) {
      if (ev.partial || !ev.content?.parts || ev.author === 'user') continue
      for (const p of ev.content.parts) {
        if (p.text) texts.push(p.text)
        if (p.functionCall?.name === 'adk_request_confirmation') {
          const args = p.functionCall.args ?? {}
          const orig = args['originalFunctionCall'] as
            | { name?: string; args?: Record<string, unknown> }
            | undefined
          confirmation = {
            callId: p.functionCall.id ?? '',
            hint: (args['hint'] as string) ?? 'The agent asks for your confirmation.',
            original: orig?.name ? { name: orig.name, args: orig.args } : undefined,
          }
        }
      }
    }
    return { text: texts.join('\n').trim(), confirmation }
  }
}
