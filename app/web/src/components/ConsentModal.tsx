import { useState } from 'react'
import type { ConsentResult, Proposal } from '../lib/api'
import { api, brl, decodeJwtPayload } from '../lib/api'

// The Trusted Surface. Deliberately plain and factual — this screen is the
// non-agentic boundary: what you see here is exactly what gets signed.
export function ConsentModal({
  proposal,
  recipeTitle,
  onClose,
  onDone,
}: {
  proposal: Proposal
  recipeTitle: string
  onClose: () => void
  onDone: () => void
}) {
  const [phase, setPhase] = useState<'review' | 'signing' | 'done' | 'error'>('review')
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
      onDone()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setPhase('error')
    }
  }

  const checkoutReceipt = result ? decodeJwtPayload(result.checkout_receipt_jwt) : null
  const paymentReceipt = result ? decodeJwtPayload(result.payment_receipt_jwt) : null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" role="dialog" aria-modal>
      <div className="absolute inset-0 bg-[rgba(36,35,33,0.22)]" onClick={onClose} />
      <div className="relative bg-surface rounded-card border-subtle shadow-[var(--shadow-float)] max-w-[560px] w-full p-6 rise">
        {phase !== 'done' && (
          <>
            <h3 className="text-xl font-medium m-0">Confirm this purchase</h3>
            <p className="text-ink-secondary text-sm mt-1 mb-4">
              You are the only signer. The agent proposed it; nothing is bought until you confirm here.
            </p>

            <div className="bg-surface-raised rounded-2xl border-subtle p-4 space-y-2">
              <div className="flex justify-between">
                <span className="text-ink-secondary text-sm">Automation</span>
                <span className="font-medium">{recipeTitle}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-ink-secondary text-sm">Recovers</span>
                <span className="tabular font-medium text-success">
                  {brl(proposal.net_monthly_cents)}/month
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-ink-secondary text-sm">Pays for itself in</span>
                <span className="tabular font-medium">
                  {proposal.payback_months.toFixed(1)} months
                </span>
              </div>
            </div>

            <p className="text-xs text-ink-tertiary mt-3 mb-4">
              Simulation for the hackathon demo — the AP2 protocol runs for real (signed mandates,
              verifiable receipts), but no money moves.
            </p>

            {phase === 'error' && (
              <div className="bg-danger-tint text-danger text-sm rounded-xl px-3 py-2 mb-3">{error}</div>
            )}

            <div className="flex gap-2 justify-end">
              <button
                onClick={onClose}
                className="rounded-pill px-4 py-2 text-sm bg-transparent border-subtle cursor-pointer hover:bg-surface-raised"
              >
                Not now
              </button>
              <button
                onClick={run}
                disabled={phase === 'signing'}
                className="rounded-pill px-5 py-2 text-sm font-medium bg-ink text-white cursor-pointer disabled:opacity-50"
              >
                {phase === 'signing' ? 'Signing mandates…' : 'Confirm & sign · simulated'}
              </button>
            </div>
          </>
        )}

        {phase === 'done' && result && (
          <>
            <h3 className="text-xl font-medium m-0">
              Purchased <span className="text-ink-tertiary">· simulation</span>
            </h3>
            <p className="text-ink-secondary text-sm mt-1 mb-4">
              {result.checkout.items.map((i) => i.title).join(', ')} —{' '}
              <span className="tabular">{brl(result.checkout.total.amount)}</span>
            </p>

            <div className="space-y-2 text-xs">
              <ReceiptRow label="Checkout Mandate" verified detail="mandate.checkout.1 · signed by you" />
              <ReceiptRow
                label="Checkout Receipt"
                verified
                detail={`order ${String(checkoutReceipt?.order_id ?? '')} · signed by merchant`}
              />
              <ReceiptRow label="Payment Mandate" verified detail="mandate.payment.1 · signed by you" />
              <ReceiptRow
                label="Payment Receipt"
                verified
                detail={`payment ${String(paymentReceipt?.payment_id ?? '')} · psp ${String(paymentReceipt?.psp_confirmation_id ?? '')}`}
              />
            </div>

            <div className="flex justify-end mt-5">
              <button
                onClick={onClose}
                className="rounded-pill px-5 py-2 text-sm font-medium bg-ink text-white cursor-pointer"
              >
                Done
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

function ReceiptRow({ label, detail, verified }: { label: string; detail: string; verified: boolean }) {
  return (
    <div className="flex items-center gap-2 bg-surface-raised border-subtle rounded-xl px-3 py-2">
      <span
        className="w-2 h-2 rounded-full shrink-0"
        style={{ background: verified ? '#2e7d32' : '#b3261e' }}
        aria-hidden
      />
      <span className="font-medium">{label}</span>
      <span className="text-ink-tertiary truncate">{detail}</span>
    </div>
  )
}
