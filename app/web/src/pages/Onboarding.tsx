import { useMemo, useState } from 'react'
import type { Profile, ProfileInput } from '../lib/api'
import { api, brl } from '../lib/api'

// Onboarding asks the two things nothing can be priced without: what an hour
// of this person's life is worth, and where their day starts. One question per
// screen, and the money moves while they type — the point of the product is
// visible before they finish setting it up.
//
// 52/12 weeks per month, the same conversion the Value Engine uses. The number
// on screen and the number in the ledger are computed the same way, so the
// preview can never promise something the engine then contradicts.
const WEEKS_PER_MONTH = 52 / 12

type Basis = 'hour' | 'income'

export function Onboarding({ current, onDone, onSkip }: { current: Profile | null; onDone: (p: Profile) => void; onSkip: () => void }) {
  const u = current?.user
  const [step, setStep] = useState(0)
  const [basis, setBasis] = useState<Basis>(u?.rate_basis === 'income' ? 'income' : 'hour')
  const [name, setName] = useState(u?.name ?? '')
  const [rate, setRate] = useState(u?.hourly_rate_cents ? String(u.hourly_rate_cents / 100) : '')
  const [income, setIncome] = useState(u?.monthly_income_cents ? String(u.monthly_income_cents / 100) : '')
  const [hours, setHours] = useState(u?.hours_per_week ? String(u.hours_per_week) : '40')
  const [home, setHome] = useState(u?.home_address ?? '')
  const [work, setWork] = useState(u?.work_address ?? '')
  const [setup, setSetup] = useState(u?.work_setup ?? '')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState<Profile | null>(null)

  const num = (s: string) => {
    const v = parseFloat(s.replace(',', '.'))
    return Number.isFinite(v) && v > 0 ? v : 0
  }
  // The rate the engine will end up with, computed here only to show it.
  const rateCents = useMemo(() => {
    if (basis === 'hour') return Math.round(num(rate) * 100)
    const h = num(hours)
    return h > 0 ? Math.round((num(income) * 100) / (h * WEEKS_PER_MONTH)) : 0
  }, [basis, rate, income, hours])

  // What the routine already on file costs at that rate. hours_per_month is
  // the same figure the P&L shows, so this is a re-pricing, not an estimate.
  const trackedHours = current?.hours_per_month ?? 0
  const previewCost = Math.round(trackedHours * rateCents)

  const submit = async () => {
    setSaving(true)
    setError('')
    const payload: ProfileInput =
      basis === 'hour'
        ? { name, hourly_rate_cents: Math.round(num(rate) * 100) }
        : { name, monthly_income_cents: Math.round(num(income) * 100), hours_per_week: num(hours) }
    payload.home_address = home
    payload.work_setup = setup || undefined
    payload.work_address = setup === 'remote' ? '' : work
    try {
      const p = await api.saveProfile(payload)
      setResult(p)
      setStep(2)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-5 py-10" style={{ background: 'var(--paper, #FAF7F2)' }}>
      <div className="w-full max-w-[560px]">
        <header className="mb-6 flex items-center justify-between gap-4">
          <div>
            <p className="scap m-0">Automate.me · setup</p>
            <h1 className="display m-0 mt-1 leading-none" style={{ fontSize: 'clamp(1.6rem, 3vw, 2.1rem)', fontWeight: 600 }}>
              {step === 2 ? 'Your hour has a price' : step === 1 ? 'Where does your day start?' : 'What is an hour of your time worth?'}
            </h1>
          </div>
          <Dots step={step} />
        </header>

        <section className="bg-surface rounded-card hairline shadow-[var(--shadow-lift)] p-6 space-y-5">
          {step === 0 && (
            <>
              <p className="text-ink-secondary text-sm m-0 leading-relaxed">
                Every number in Automate.me — what a routine costs you, what an automation pays back, what the ledger recovered —
                is this one figure multiplied by time. Nothing else is guessed.
              </p>

              <div className="flex gap-1.5">
                <Tab active={basis === 'hour'} onClick={() => setBasis('hour')}>
                  I know my hourly rate
                </Tab>
                <Tab active={basis === 'income'} onClick={() => setBasis('income')}>
                  Work it out from what I earn
                </Tab>
              </div>

              {basis === 'hour' ? (
                <Field label="An hour of my time is worth" prefix="R$" value={rate} onChange={setRate} placeholder="50" autoFocus suffix="per hour" />
              ) : (
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label="I earn, per month" prefix="R$" value={income} onChange={setIncome} placeholder="8000" autoFocus />
                  <Field label="I work, per week" value={hours} onChange={setHours} placeholder="40" suffix="hours" />
                </div>
              )}

              <RateEcho rateCents={rateCents} basis={basis} trackedHours={trackedHours} previewCost={previewCost} />

              <Field label="What should the agent call you?" value={name} onChange={setName} placeholder="Karol" optional />
            </>
          )}

          {step === 1 && (
            <>
              <p className="text-ink-secondary text-sm m-0 leading-relaxed">
                The Daily Briefing routes from here: departure times, what traffic costs at your rate, and whether the way there
                crosses ground that floods. Skip it and the agent simply won't plan trips.
              </p>
              <Field label="I start my day at" value={home} onChange={setHome} placeholder="Rua dos Pinheiros 1000, São Paulo" autoFocus optional />

              <div>
                <div className="scap mb-1.5">My work is</div>
                <div className="flex gap-1.5 flex-wrap">
                  {(['remote', 'hybrid', 'onsite'] as const).map((s) => (
                    <Tab key={s} active={setup === s} onClick={() => setSetup(setup === s ? '' : s)}>
                      {s === 'remote' ? 'Fully remote' : s === 'hybrid' ? 'Hybrid' : 'On site'}
                    </Tab>
                  ))}
                </div>
              </div>

              {setup !== 'remote' && (
                <Field label="I work at" value={work} onChange={setWork} placeholder="Av. Paulista 1578, São Paulo" optional />
              )}
              {setup === 'remote' && (
                <p className="text-xs text-ink-tertiary m-0">
                  No commute to price, then. The agent will look for leaks in the day itself instead.
                </p>
              )}
            </>
          )}

          {step === 2 && result && <Done p={result} />}

          {error && <div className="bg-alert-tint text-alert-deep rounded-xl px-4 py-3 text-sm">{error}</div>}

          <footer className="flex items-center gap-3 pt-1">
            {step === 0 && (
              <>
                <button
                  onClick={() => setStep(1)}
                  disabled={rateCents <= 0}
                  className="rounded-pill bg-teal text-white text-sm font-medium px-5 py-2.5 cursor-pointer disabled:opacity-40"
                >
                  Continue
                </button>
                <button onClick={onSkip} className="text-xs text-ink-tertiary hover:text-ink-secondary cursor-pointer bg-transparent">
                  Skip for now
                </button>
              </>
            )}
            {step === 1 && (
              <>
                <button
                  onClick={() => void submit()}
                  disabled={saving}
                  className="rounded-pill bg-teal text-white text-sm font-medium px-5 py-2.5 cursor-pointer disabled:opacity-60 inline-flex items-center gap-2"
                >
                  {saving && <span className="ring" style={{ borderTopColor: '#f8d973' }} />}
                  {saving ? 'Pricing your routine…' : 'Save and price my routine'}
                </button>
                <button onClick={() => setStep(0)} className="text-xs text-ink-tertiary hover:text-ink-secondary cursor-pointer bg-transparent">
                  Back
                </button>
              </>
            )}
            {step === 2 && result && (
              <button
                onClick={() => onDone(result)}
                className="rounded-pill bg-teal text-white text-sm font-medium px-5 py-2.5 cursor-pointer"
              >
                Show me where my time goes →
              </button>
            )}
          </footer>
        </section>
      </div>
    </div>
  )
}

// RateEcho is the whole argument of the product, live: the hour they just
// typed, and what the routine already on file costs at it.
function RateEcho({
  rateCents,
  basis,
  trackedHours,
  previewCost,
}: {
  rateCents: number
  basis: Basis
  trackedHours: number
  previewCost: number
}) {
  if (rateCents <= 0) {
    return (
      <div className="bg-paper border-subtle rounded-xl px-4 py-3 text-sm text-ink-tertiary">
        Type a number and the agent starts pricing everything against it.
      </div>
    )
  }
  return (
    <div className="bg-gold-tint/50 border-subtle rounded-xl px-4 py-3.5">
      <div className="flex items-baseline gap-2 flex-wrap">
        <span className="tabular display" style={{ fontSize: '1.7rem', fontWeight: 600 }}>
          {brl(rateCents)}
        </span>
        <span className="text-ink-secondary text-sm">an hour</span>
        {basis === 'income' && <span className="text-xs text-ink-tertiary">· derived from what you earn, over 52/12 weeks a month</span>}
      </div>
      {trackedHours > 0 && (
        <p className="text-sm text-ink-secondary m-0 mt-1.5">
          The <b className="font-medium">{trackedHours.toFixed(1)} hours a month</b> already on your record are worth{' '}
          <b className="font-medium tabular">{brl(previewCost)}</b> at that price.
        </p>
      )}
    </div>
  )
}

function Done({ p }: { p: Profile }) {
  const moved = p.cost_delta_cents ?? 0
  return (
    <div className="space-y-4">
      <div className="bg-gold-tint/50 border-subtle rounded-xl px-4 py-4">
        <div className="scap">your hour</div>
        <div className="tabular display leading-none mt-1" style={{ fontSize: '2.2rem', fontWeight: 600 }}>
          {brl(p.user.hourly_rate_cents)}
        </div>
        <p className="text-sm text-ink-secondary m-0 mt-2">
          {p.tasks > 0 ? (
            <>
              The <b className="font-medium">{p.tasks}</b> routines on your record take{' '}
              <b className="font-medium">{p.hours_per_month.toFixed(1)} hours a month</b> and cost you{' '}
              <b className="font-medium tabular">{brl(p.cost_of_inaction_cents)}</b> at that rate.
            </>
          ) : (
            <>Tell the agent one routine that eats your time and it will price it against this number.</>
          )}
        </p>
        {moved !== 0 && (
          <p className="text-xs text-ink-tertiary m-0 mt-1.5">
            Re-priced from {brl(p.previous_hourly_rate_cents ?? 0)}/h: {moved > 0 ? '+' : '−'}
            {brl(Math.abs(moved))} a month on the same routine.
          </p>
        )}
      </div>
      {p.best_monthly_savings_cents > 0 && (
        <p className="text-sm text-ink-secondary m-0">
          The best automation the engine found recovers <b className="font-medium tabular">{brl(p.best_monthly_savings_cents)}</b> a
          month — {p.proposals} proposals are ranked and waiting.
        </p>
      )}
      <p className="text-xs text-ink-tertiary m-0">
        The agent knows all of this now, in chat and out loud. You can change it any time from the P&L.
      </p>
    </div>
  )
}

function Dots({ step }: { step: number }) {
  return (
    <div className="flex gap-1.5 shrink-0" aria-hidden>
      {[0, 1, 2].map((i) => (
        <span
          key={i}
          className="rounded-full transition-all"
          style={{
            width: i === step ? 22 : 7,
            height: 7,
            background: i <= step ? '#BC9A75' : 'rgba(19,53,63,.15)',
          }}
        />
      ))}
    </div>
  )
}

function Tab({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={`rounded-pill text-xs px-3.5 py-2 cursor-pointer transition-colors ${
        active ? 'bg-teal text-white font-medium' : 'border border-line bg-paper hover:bg-gold-tint'
      }`}
    >
      {children}
    </button>
  )
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  prefix,
  suffix,
  autoFocus,
  optional,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  prefix?: string
  suffix?: string
  autoFocus?: boolean
  optional?: boolean
}) {
  return (
    <label className="block">
      <span className="scap">
        {label}
        {optional && <span className="text-ink-tertiary normal-case tracking-normal"> · optional</span>}
      </span>
      <div className="mt-1.5 flex items-center gap-2 bg-paper border border-line rounded-xl px-3 py-2.5 focus-within:border-gold">
        {prefix && <span className="text-ink-tertiary text-sm shrink-0">{prefix}</span>}
        <input
          value={value}
          autoFocus={autoFocus}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          className="flex-1 min-w-0 bg-transparent outline-none text-sm placeholder:text-ink-tertiary"
        />
        {suffix && <span className="text-ink-tertiary text-xs shrink-0">{suffix}</span>}
      </div>
    </label>
  )
}
