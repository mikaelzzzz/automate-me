import { GoogleGenAI, Modality, type LiveServerMessage, type Session } from '@google/genai'

// Voice orchestration. The browser holds the microphone and speaks straight to
// the Gemini Live API with a single-use ephemeral token; the API key never
// leaves the server. When the model decides to act, its function calls come
// back to /app/api/live/tool, which runs the very same tools the ADK graph
// runs — the Value Engine, the store, Routes/Weather. The voice is a new front
// door onto the agent we already have, not a second agent.

const MIC_RATE = 16000 // what the Live API expects from us
const OUT_RATE = 24000 // what it sends back

export interface LiveEvent {
  kind:
    | 'state'
    | 'user-transcript'
    | 'agent-transcript'
    | 'tool-start'
    | 'tool-done'
    | 'tool-error'
    | 'interrupted'
    | 'error'
  state?: LiveState
  text?: string
  /** true while the model is still mid-sentence */
  partial?: boolean
  tool?: string
  result?: unknown
}

export type LiveState = 'idle' | 'connecting' | 'listening' | 'thinking' | 'speaking'

interface SessionConfig {
  available: boolean
  token?: string
  model?: string
  voice?: string
  system_instruction?: string
  tools?: Record<string, unknown>[]
  reason?: string
}

/** Float32 [-1,1] → little-endian PCM16 → base64, the Live API's input format. */
function encodePCM16(input: Float32Array): string {
  const buf = new ArrayBuffer(input.length * 2)
  const view = new DataView(buf)
  for (let i = 0; i < input.length; i++) {
    const s = Math.max(-1, Math.min(1, input[i]))
    view.setInt16(i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true)
  }
  let binary = ''
  const bytes = new Uint8Array(buf)
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(binary)
}

/** base64 PCM16 @24kHz → AudioBuffer ready to schedule. */
function decodePCM16(b64: string, ctx: AudioContext): AudioBuffer {
  const binary = atob(b64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  const samples = new Int16Array(bytes.buffer)
  const buffer = ctx.createBuffer(1, samples.length, OUT_RATE)
  const channel = buffer.getChannelData(0)
  for (let i = 0; i < samples.length; i++) channel[i] = samples[i] / 0x8000
  return buffer
}

// A tiny worklet so mic capture runs off the main thread. Registered from a
// blob: URL to keep the whole client in one file.
const WORKLET = `
class Capture extends AudioWorkletProcessor {
  process(inputs) {
    const ch = inputs[0] && inputs[0][0]
    if (ch) this.port.postMessage(new Float32Array(ch))
    return true
  }
}
registerProcessor('capture', Capture)
`

export class LiveVoice {
  private session: Session | null = null
  private mic: MediaStream | null = null
  private inCtx: AudioContext | null = null
  private outCtx: AudioContext | null = null
  private node: AudioWorkletNode | null = null
  private source: MediaStreamAudioSourceNode | null = null
  /** when the next chunk of speech should start, so playback stays gapless */
  private playHead = 0
  private playing: AudioBufferSourceNode[] = []
  private muted = false
  private closed = false

  private emit: (e: LiveEvent) => void

  constructor(emit: (e: LiveEvent) => void) {
    this.emit = emit
  }

  get active() {
    return !!this.session && !this.closed
  }

  async start(): Promise<void> {
    this.closed = false
    this.emit({ kind: 'state', state: 'connecting' })

    const cfg: SessionConfig = await fetch('/app/api/live/session', { method: 'POST' }).then((r) => r.json())
    if (!cfg.available || !cfg.token) {
      this.emit({ kind: 'error', text: cfg.reason ?? 'voice is not configured on this server' })
      this.emit({ kind: 'state', state: 'idle' })
      return
    }

    // Ask for the microphone before opening the socket: no point holding a
    // session open if the user says no.
    try {
      this.mic = await navigator.mediaDevices.getUserMedia({
        audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      })
    } catch {
      this.emit({ kind: 'error', text: 'microphone permission denied' })
      this.emit({ kind: 'state', state: 'idle' })
      return
    }

    // The ephemeral token is used in the API-key slot, and only v1alpha accepts it.
    const ai = new GoogleGenAI({ apiKey: cfg.token, httpOptions: { apiVersion: 'v1alpha' } })

    this.outCtx = new AudioContext({ sampleRate: OUT_RATE })
    await this.outCtx.resume()

    this.session = await ai.live.connect({
      model: cfg.model!,
      config: {
        responseModalities: [Modality.AUDIO],
        systemInstruction: { parts: [{ text: cfg.system_instruction ?? '' }] },
        speechConfig: cfg.voice ? { voiceConfig: { prebuiltVoiceConfig: { voiceName: cfg.voice } } } : undefined,
        // Transcripts of both sides, so the rail can show what was said.
        inputAudioTranscription: {},
        outputAudioTranscription: {},
        tools: cfg.tools?.length ? [{ functionDeclarations: cfg.tools }] : undefined,
      },
      callbacks: {
        onopen: () => this.emit({ kind: 'state', state: 'listening' }),
        onmessage: (m) => void this.onMessage(m),
        onerror: (e) => this.emit({ kind: 'error', text: e?.message ?? 'live connection error' }),
        onclose: () => {
          if (!this.closed) this.emit({ kind: 'state', state: 'idle' })
        },
      },
    })

    await this.pipeMic()
  }

  /** Mic → worklet → PCM16 → socket. */
  private async pipeMic() {
    this.inCtx = new AudioContext({ sampleRate: MIC_RATE })
    await this.inCtx.resume()
    const url = URL.createObjectURL(new Blob([WORKLET], { type: 'application/javascript' }))
    await this.inCtx.audioWorklet.addModule(url)
    URL.revokeObjectURL(url)

    this.source = this.inCtx.createMediaStreamSource(this.mic!)
    this.node = new AudioWorkletNode(this.inCtx, 'capture')
    this.node.port.onmessage = (ev) => {
      if (!this.session || this.muted || this.closed) return
      this.session.sendRealtimeInput({
        audio: { data: encodePCM16(ev.data as Float32Array), mimeType: `audio/pcm;rate=${MIC_RATE}` },
      })
    }
    this.source.connect(this.node)
    // The worklet emits nothing, but Chrome only pulls audio through a graph
    // that reaches a destination.
    this.node.connect(this.inCtx.destination)
  }

  private async onMessage(msg: LiveServerMessage) {
    const content = msg.serverContent

    // Barge-in: the user talked over the model. Drop everything queued.
    if (content?.interrupted) {
      this.stopPlayback()
      this.emit({ kind: 'interrupted' })
      this.emit({ kind: 'state', state: 'listening' })
    }

    // A single event can carry audio and transcript at once — read every part.
    if (content?.modelTurn?.parts) {
      for (const part of content.modelTurn.parts) {
        const data = part.inlineData?.data
        if (data) this.play(data)
      }
    }
    if (content?.inputTranscription?.text) {
      this.emit({ kind: 'user-transcript', text: content.inputTranscription.text, partial: true })
    }
    if (content?.outputTranscription?.text) {
      this.emit({ kind: 'agent-transcript', text: content.outputTranscription.text, partial: true })
    }
    if (content?.turnComplete) {
      this.emit({ kind: 'user-transcript', text: '', partial: false })
      this.emit({ kind: 'agent-transcript', text: '', partial: false })
      this.emit({ kind: 'state', state: 'listening' })
    }

    // The model wants to act. Function calling is synchronous: it will not
    // speak again until every response is back.
    if (msg.toolCall?.functionCalls?.length) {
      this.emit({ kind: 'state', state: 'thinking' })
      const responses = []
      for (const call of msg.toolCall.functionCalls) {
        const name = call.name ?? ''
        this.emit({ kind: 'tool-start', tool: name })
        try {
          const res = await fetch('/app/api/live/tool', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name, args: call.args ?? {} }),
          }).then((r) => r.json())
          if (res.error) {
            this.emit({ kind: 'tool-error', tool: name, text: res.error })
            responses.push({ id: call.id, name, response: { error: res.error } })
          } else {
            this.emit({ kind: 'tool-done', tool: name, result: res.result })
            responses.push({ id: call.id, name, response: { result: res.result } })
          }
        } catch (e) {
          const text = e instanceof Error ? e.message : String(e)
          this.emit({ kind: 'tool-error', tool: name, text })
          responses.push({ id: call.id, name, response: { error: text } })
        }
      }
      this.session?.sendToolResponse({ functionResponses: responses })
      this.emit({ kind: 'state', state: 'speaking' })
    }
  }

  private play(b64: string) {
    if (!this.outCtx) return
    const buf = decodePCM16(b64, this.outCtx)
    const src = this.outCtx.createBufferSource()
    src.buffer = buf
    src.connect(this.outCtx.destination)
    const now = this.outCtx.currentTime
    this.playHead = Math.max(this.playHead, now)
    src.start(this.playHead)
    this.playHead += buf.duration
    this.playing.push(src)
    src.onended = () => {
      this.playing = this.playing.filter((s) => s !== src)
      if (this.playing.length === 0 && !this.closed) this.emit({ kind: 'state', state: 'listening' })
    }
    this.emit({ kind: 'state', state: 'speaking' })
  }

  private stopPlayback() {
    for (const s of this.playing) {
      try {
        s.stop()
      } catch {
        /* already finished */
      }
    }
    this.playing = []
    this.playHead = 0
  }

  /** Hold the mic without tearing the session down. */
  setMuted(muted: boolean) {
    this.muted = muted
    if (muted) this.session?.sendRealtimeInput({ audioStreamEnd: true })
  }

  /** Type into a voice session — same socket, no microphone. */
  say(text: string) {
    this.session?.sendRealtimeInput({ text })
    this.emit({ kind: 'state', state: 'thinking' })
  }

  async stop() {
    this.closed = true
    this.stopPlayback()
    this.node?.port.close()
    this.node?.disconnect()
    this.source?.disconnect()
    this.mic?.getTracks().forEach((t) => t.stop())
    await this.inCtx?.close().catch(() => {})
    await this.outCtx?.close().catch(() => {})
    this.session?.close()
    this.session = null
    this.mic = null
    this.inCtx = this.outCtx = null
    this.node = null
    this.source = null
    this.emit({ kind: 'state', state: 'idle' })
  }
}
