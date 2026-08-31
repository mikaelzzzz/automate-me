// Typed client for the Go backend: dashboard API (/app/api) + chat (adkrest /api).

export interface Task {
  id: string
  name: string
  est_minutes: number
  freq_per_month: number
  source: string
  confirmed: boolean
}

export interface PnlTask extends Task {
  hours_month: number
  cost_cents_month: number
}

export interface Pnl {
  tasks: PnlTask[] | null
  total_hours_month: number
  total_cents_month: number
  hourly_rate_cents: number
}

export type ProposalStatus = 'proposed' | 'approved' | 'executed' | 'declined'

export interface Proposal {
  id: string
  user_id: string
  task_id: string
  recipe_id: string
  recipe_title: string
  recipe_description: string
  recipe_class: 'executable' | 'advised' | 'roadmap' | ''
  /** The agent can buy it through the AP2 rail (recipe has a product). */
  executable: boolean
  product_id?: string
  /** Value Engine inputs, so the consent screen shows the same arithmetic. */
  upfront_cents: number
  monthly_running_cents: number
  minutes_saved_per_occurrence: number
  task_minutes: number
  task_freq_per_month: number
  hourly_rate_cents: number
  monthly_savings_cents: number
  net_monthly_cents: number
  payback_months: number
  status: ProposalStatus
}

export interface LedgerEntry {
  id: string
  week_start: string
  recipe_id: string
  recipe_title: string
  hours_recovered: number
  brl_recovered_cents: number
  confirmed: boolean
  mandate_ref?: string
}

/** AP2 audit trail: the four signed artifacts of one purchase. */
export interface MandateRecord {
  id: string
  proposal_id: string
  checkout_id: string
  checkout_jwt: string
  checkout_mandate: string
  checkout_receipt: string
  payment_mandate: string
  payment_receipt: string
  status: 'pending' | 'completed' | 'failed'
  created_at: string
  updated_at: string
}

export interface BriefingCard {
  id: string
  day: string
  event_summary: string
  event_start: string
  origin: string
  destination: string
  departure_time: string
  route_summary: string
  route_minutes: number
  traffic_minutes: number
  traffic_cents: number
  weather: string
  weather_temp_c: number
  rain_chance_pct: number
  clothing: string
  flood_risk: 'none' | 'historic' | 'alert'
  flood_detail: string
  flood_points: number
  alert_headline?: string
  alternative_note: string
  notes?: string[]
  calendar_block_id?: string
  calendar_block_mode?: 'simulated' | 'google'
}

// One row of the day exactly as the calendar has it. The briefing prices the
// trips; the agenda shows everything, and says why the rest was not priced.
// The profile is the money spine: every figure in the product is this rate
// multiplied by time.
export interface ProfileUser {
  id: string
  name: string
  mode: string
  hourly_rate_cents: number
  rate_basis?: 'declared' | 'income'
  monthly_income_cents?: number
  hours_per_week?: number
  home_address?: string
  work_address?: string
  work_setup?: 'remote' | 'hybrid' | 'onsite'
  onboarded: boolean
}

export interface Profile {
  user: ProfileUser
  tasks: number
  hours_per_month: number
  cost_of_inaction_cents: number
  proposals: number
  best_monthly_savings_cents: number
  previous_hourly_rate_cents?: number
  cost_delta_cents?: number
}

export interface ProfileInput {
  name?: string
  hourly_rate_cents?: number
  monthly_income_cents?: number
  hours_per_week?: number
  home_address?: string
  work_address?: string
  work_setup?: string
}

export interface AgendaEntry {
  id: string
  summary: string
  start: string
  end: string
  kind: 'trip' | 'remote' | 'no_place' | 'ignored'
  location?: string
  reason?: string
}

export interface Agenda {
  day: string
  source: string
  available: boolean
  note: string
  trips: number
  remote: number
  no_place: number
  skipped: number
  entries: AgendaEntry[]
}

export interface Briefing {
  day: string
  cards: BriefingCard[]
  available: boolean
  // What the calendar day looked like: how many appointments were remote,
  // unplaced, or ignored. Present once a real calendar is connected.
  note?: string
}

export interface ConsentResult {
  mandate_record_id: string
  checkout: {
    id: string
    merchant: { id: string; name: string; website?: string }
    items: { id: string; title: string; price: { amount: number; currency: string }; quantity: number }[]
    total: { amount: number; currency: string }
  }
  checkout_receipt_jwt: string
  payment_receipt_jwt: string
  completed: boolean
  failure_reason?: string
}

async function j<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `HTTP ${res.status}`)
  }
  return res.json() as Promise<T>
}

export const api = {
  pnl: () => fetch('/app/api/pnl').then((r) => j<Pnl>(r)),
  proposals: () => fetch('/app/api/proposals').then((r) => j<Proposal[]>(r)),
  approve: (id: string) =>
    fetch(`/app/api/proposals/${id}/approve`, { method: 'POST' }).then((r) => j<Proposal>(r)),
  consent: (proposalId: string) =>
    fetch('/app/api/trusted/consent', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ proposal_id: proposalId }),
    }).then((r) => j<ConsentResult>(r)),
  ledger: () => fetch('/app/api/ledger').then((r) => j<LedgerEntry[]>(r)),
  mandates: () => fetch('/app/api/mandates').then((r) => j<MandateRecord[]>(r)),
  profile: () => fetch('/app/api/profile').then((r) => j<Profile>(r)),
  saveProfile: (input: ProfileInput) =>
    fetch('/app/api/profile', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    }).then((r) => j<Profile>(r)),
  agenda: () => fetch('/app/api/agenda').then((r) => j<Agenda>(r)),
  briefing: () => fetch('/app/api/briefing').then((r) => j<Briefing>(r)),
  runBriefing: () => fetch('/app/api/briefing/run', { method: 'POST' }).then((r) => j<Briefing>(r)),
  writeBlock: (cardId: string) =>
    fetch(`/app/api/briefing/${cardId}/block`, { method: 'POST' }).then((r) => j<BriefingCard>(r)),
}

// Money: integer centavos → "R$1,500.00". Never do float math on money.
export function brl(cents: number): string {
  const v = cents / 100
  return v.toLocaleString('en-US', { style: 'currency', currency: 'BRL', currencyDisplay: 'symbol' })
}

/** Whole reais — for hero figures where the cents are noise. */
export function brlWhole(cents: number): string {
  return (cents / 100).toLocaleString('en-US', {
    style: 'currency',
    currency: 'BRL',
    currencyDisplay: 'symbol',
    maximumFractionDigits: 0,
  })
}

export function hours(h: number): string {
  return `${h.toLocaleString('en-US', { maximumFractionDigits: 1 })} h`
}

export function decodeJwtPayload(jwt: string): Record<string, unknown> | null {
  try {
    const p = jwt.split('.')[1]
    const pad = p + '='.repeat((4 - (p.length % 4)) % 4)
    return JSON.parse(atob(pad.replace(/-/g, '+').replace(/_/g, '/')))
  } catch {
    return null
  }
}
