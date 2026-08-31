import { useState } from 'react'
import type { ConsentResult, Proposal } from '../lib/api'
import { api, brl, decodeJwtPayload } from '../lib/api'

// The Trusted Surface. Deliberately plain and factual: what you see here is
// exactly what gets signed, and no model runs on this path.

type Phase = 'review' | 'signing' | 'done' | 'error'

export function ConsentModal({
  proposal,
  onClose,
  onDone,
}: {
  proposal: Proposal
  onClose: () => void
  onDone: (res: ConsentResult | null) => void
}) {
  const [phase, setPhase] = useState<Phase>('review')
  const [result, setResult] = useState<ConsentResult | null>(null)
  const [error, setError] = useState('')

  const run = async () => {
    setPhase('signing')
    try {
      if (proposal.status === 'proposed') await api.approve(proposal.id)
      const res = await api.consent(proposal.id)
      setResult(res)
      setPhase(res.completed ? 'done' : 'error')
      if (!res.completed) setError(res.failure_reason ?? 'purchase failed')
      onDone(res)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setPhase('error')
    }
  }

  const checkoutReceipt = result ? decodeJwtPayload(result.checkout_receipt_jwt) : null
  const paymentReceipt = result ? decodeJwtPayload(result.payment_receipt_jwt) : null
  const item = result?.checkout.items?.[0]
  const price = result?.checkout.total.amount ?? proposal.upfront_cents
  const rate = proposal.hourly_rate_cents

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 sm:p-6" role="dialog" aria-modal aria-label="Confirm this purchase">
      <div className="absolute inset-0" style={{ background: 'rgba(10,33,40,.35)' }} onClick={onClose} />

      <div className="relative w-full max-w-[860px] max-h-[92vh] overflow-y-auto chat-scroll rounded-[18px] shadow-[var(--shadow-float)] grid md:grid-cols-2 pop">
        {/* ---- left: the proposal and its arithmetic ---- */}
        <div className="bg-surface p-6 sm:p-7">
          <p className="scap m-0">
            Proposal · {proposal.payback_months > 0 ? `payback ${proposal.payback_months.toFixed(1)} months` : 'pays back immediately'}
          </p>
          <h2 className="display m-0 mt-2 text-[26px] leading-[1.15] font-semibold">
            {headline(proposal.recipe_title)} and stop losing{' '}
            <em className="text-gold-deep not-italic" style={{ fontStyle: 'italic' }}>
              {brl(proposal.monthly_savings_cents)} a month
            </em>
          </h2>

          <div className="mt-4 bg-paper border border-line rounded-[11px] p-3.5 flex gap-3 items-center">
            <div className="w-14 h-14 rounded-[10px] bg-surface-warm shrink-0" aria-hidden />
            <div className="min-w-0">
              <div className="font-semibold text-sm leading-tight">{item?.title ?? proposal.recipe_title}</div>
              <div className="text-xs text-ink-tertiary mt-0.5">Merchant agent · A2A · in stock</div>
              <div className="display text-[19px] mt-1 tabular">{brl(price)}</div>
            </div>
          </div>

          <div className="mt-3 bg-paper border border-line rounded-[11px] p-3.5">
            <p className="scap m-0 mb-2">The math (Value Engine)</p>
            <MathRow
              left={`${proposal.minutes_saved_per_occurrence} min × ${fmt(proposal.task_freq_per_month)}/mo × ${brl(rate)}/h`}
              right={`${brl(proposal.monthly_savings_cents)} recovered/mo`}
            />
            {proposal.monthly_running_cents > 0 && (
              <MathRow left="Running cost" right={`– ${brl(proposal.monthly_running_cents)}/mo`} />
            )}
            <div className="border-t border-line mt-2 pt-2">
              <MathRow
                left={proposal.upfront_cents > 0 ? 'Upfront ÷ net monthly' : 'No upfront cost'}
                right={proposal.payback_months > 0 ? `payback ${proposal.payback_months.toFixed(1)} mo` : `net +${brl(proposal.net_monthly_cents)}/mo`}
                strong
              />
            </div>
          </div>

          <p className="text-xs text-ink-tertiary mt-3 mb-0 flex gap-2 leading-relaxed">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" className="shrink-0 mt-0.5" aria-hidden>
              <path d="M12 2.5 20 6v6c0 5-3.4 8.4-8 9.5-4.6-1.1-8-4.5-8-9.5V6l8-3.5z" strokeLinejoin="round" />
            </svg>
            No model runs in this surface. Signing happens in deterministic code, after your consent. Settlement is
            simulated for the demo — the protocol is not.
          </p>
        </div>

        {/* ---- right: the AP2 chain ---- */}
        <div className="bg-teal text-white p-6 sm:p-7 flex flex-col">
          <p className="scap m-0" style={{ color: '#BC9A75' }}>
            AP2 v0.2 · mandate chain
          </p>

          <ol className="list-none p-0 m-0 mt-4 space-y-0">
            <Step
              n={1}
              title="Checkout Mandate"
              detail={
                phase === 'done'
                  ? `order ${String(checkoutReceipt?.order_id ?? '—')} · receipt verified`
                  : 'mandate.checkout.1 · ECDSA P-256 · cart hash bound'
              }
              state={phase === 'done' ? 'done' : phase === 'signing' ? 'active' : 'ready'}
            />
            <Step
              n={2}
              title="Payment Mandate"
              detail={
                phase === 'done'
                  ? `payment ${String(paymentReceipt?.payment_id ?? '—')} · psp ${String(paymentReceipt?.psp_confirmation_id ?? '—')}`
                  : 'mandate.payment.1 · signs when you authorize'
              }
              state={phase === 'done' ? 'done' : phase === 'signing' ? 'active' : 'pending'}
            />
            <Step
              n={3}
              title="Receipts"
              detail={phase === 'done' ? 'both verified · attached to the ledger' : 'merchant and processor countersign'}
              state={phase === 'done' ? 'done' : 'pending'}
              last
            />
          </ol>

          <div
            className="mt-5 rounded-[11px] px-3.5 py-3 text-[12.5px] leading-relaxed"
            style={{ background: 'rgba(255,255,255,.06)', color: 'rgba(255,255,255,.8)' }}
          >
            {phase === 'done'
              ? 'Signed. The mandate record is on the audit trail and the receipts are attached to the ledger entry.'
              : 'On authorize: two mandates signed by you, two receipts verified, the purchase written to the ledger with its mandate reference.'}
          </div>

          {phase === 'error' && (
            <div className="mt-3 rounded-[11px] px-3.5 py-2.5 text-xs" style={{ background: '#8A3D2B', color: '#fff' }}>
              {error}
            </div>
          )}

          <div className="mt-auto pt-5">
            {phase === 'done' ? (
              <button
                onClick={onClose}
                className="w-full rounded-pill bg-gold text-teal font-semibold py-3 cursor-pointer hover:brightness-105"
              >
                Done
              </button>
            ) : (
              <button
                onClick={run}
                disabled={phase === 'signing'}
                className="w-full rounded-pill bg-gold text-teal font-semibold py-3 cursor-pointer hover:brightness-105 disabled:opacity-70 flex items-center justify-center gap-2"
              >
                {phase === 'signing' && <span className="ring" style={{ borderTopColor: '#13353F' }} />}
                {phase === 'signing' ? 'Signing mandates…' : `Authorize ${brl(price)}`}
              </button>
            )}
            <div className="text-center mt-2.5 text-xs" style={{ color: 'rgba(255,255,255,.45)' }}>
              {phase === 'done' ? (
                <span>simulated settlement · verifiable JWTs on the ledger</span>
              ) : (
                <button onClick={onClose} className="cursor-pointer bg-transparent" style={{ color: 'inherit' }}>
                  Not now
                </button>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

/** "Buy a dishwasher (agent purchase)" → "Buy a dishwasher" */
function headline(title: string) {
  return title.replace(/\s*\([^)]*\)\s*$/, '')
}

function fmt(n: number) {
  return Number.isInteger(n) ? String(n) : n.toFixed(2)
}

function MathRow({ left, right, strong }: { left: string; right: string; strong?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-1 text-[13px]">
      <span className={strong ? 'font-medium' : 'text-ink-secondary'}>{left}</span>
      <span className={`tabular whitespace-nowrap ${strong ? 'text-positive font-semibold' : ''}`}>{right}</span>
    </div>
  )
}

function Step({
  n,
  title,
  detail,
  state,
  last,
}: {
  n: number
  title: string
  detail: string
  state: 'ready' | 'active' | 'done' | 'pending'
  last?: boolean
}) {
  const gold = '#BC9A75'
  const dim = 'rgba(255,255,255,.45)'
  const on = state === 'done' || state === 'active' || state === 'ready'
  return (
    <li className="flex gap-3 pb-4 last:pb-0 relative">
      {!last && (
        <span
          className="absolute left-[13px] top-7 bottom-0 w-px"
          style={{ background: state === 'done' ? gold : 'rgba(255,255,255,.14)' }}
          aria-hidden
        />
      )}
      <span
        className="w-[27px] h-[27px] rounded-full shrink-0 flex items-center justify-center text-[11px] font-bold z-10"
        style={
          state === 'done'
            ? { background: gold, color: '#13353F' }
            : state === 'active'
              ? { background: 'transparent', color: gold, border: `2px solid ${gold}` }
              : { background: 'transparent', color: dim, border: `1.5px dashed rgba(255,255,255,.3)` }
        }
      >
        {state === 'done' ? '✓' : n}
      </span>
      <div className="min-w-0 pt-0.5">
        <div className="text-sm font-semibold" style={{ color: on ? '#fff' : dim }}>
          {title}
        </div>
        <div className="text-xs mt-0.5 break-words" style={{ color: 'rgba(255,255,255,.6)' }}>
          {detail}
        </div>
      </div>
    </li>
  )
}
