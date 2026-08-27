import { useEffect, useMemo, useState } from 'react'
import type { LedgerEntry, MandateRecord } from '../lib/api'
import { api, brl, decodeJwtPayload } from '../lib/api'

// Savings ledger: every week of time bought back, and for purchases the
// verifiable AP2 trail — two mandates signed by the user, two receipts signed
// by the merchant/processor. Proof, not promises.
export function LedgerView({ entries, onAsk }: { entries: LedgerEntry[]; onAsk: (text: string) => void }) {
  const [mandates, setMandates] = useState<MandateRecord[]>([])
  const [openRef, setOpenRef] = useState<string | null>(null)

  useEffect(() => {
    api.mandates().then(setMandates).catch(() => setMandates([]))
  }, [entries])

  const totals = useMemo(() => {
    let confirmed = 0
    let projected = 0
    let hrs = 0
    for (const e of entries) {
      if (e.confirmed) confirmed += e.brl_recovered_cents
      else projected += e.brl_recovered_cents
      hrs += e.hours_recovered
    }
    return { confirmed, projected, hrs }
  }, [entries])

  const sorted = useMemo(
    () => [...entries].sort((a, b) => new Date(a.week_start).getTime() - new Date(b.week_start).getTime()),
    [entries],
  )

  if (entries.length === 0) {
    return (
      <div className="bg-surface/85 rounded-card hairline shadow-[var(--shadow-lift)] p-8 max-w-[760px] text-center">
        <h2 className="m-0 text-lg font-medium">Savings ledger</h2>
        <p className="text-sm text-ink-secondary mt-2 mb-4">Approve your first automation and proof lands here, week by week.</p>
        <button onClick={() => onAsk('What should I automate first?')} className="rounded-pill bg-ink text-white text-sm px-4 py-2 cursor-pointer">
          Ask the agent
        </button>
      </div>
    )
  }

  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_260px] max-w-[1040px]">
      <section className="bg-surface/85 rounded-card hairline shadow-[var(--shadow-lift)] p-5 rise">
        <div className="flex items-baseline justify-between mb-4">
          <h2 className="m-0 text-lg font-medium">Savings ledger</h2>
          <span className="text-xs text-ink-tertiary">weekly · BRL</span>
        </div>

        <div className="space-y-1">
          {sorted.map((e) => {
            const purchase = !!e.mandate_ref
            const rec = purchase ? mandates.find((m) => m.id === e.mandate_ref) : undefined
            const expanded = purchase && openRef === e.mandate_ref
            const future = new Date(e.week_start).getTime() > Date.now()
            return (
              <div key={e.id} className={`rounded-row transition-colors ${expanded ? 'bg-sun/10' : 'hover:bg-sun/10'}`}>
                <div className="flex items-center gap-3 px-3 py-2.5 text-sm">
                  <span className="text-ink-tertiary text-xs tabular w-16 shrink-0">
                    {new Date(e.week_start).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}
                  </span>
                  <div className="flex-1 min-w-0">
                    <div className="truncate">{e.recipe_title || e.recipe_id}</div>
                    {purchase && (
                      <div className="text-[11px] text-ink-tertiary">
                        {future ? 'starts recovering the week of ' : 'recovering since '}
                        {new Date(e.week_start).toLocaleDateString('en-US', { month: 'long', day: 'numeric' })}
                        {e.hours_recovered > 0 && <> · {e.hours_recovered.toFixed(1)} h/week</>}
                      </div>
                    )}
                  </div>
                  {purchase && (
                    <button
                      onClick={() => setOpenRef(expanded ? null : e.mandate_ref!)}
                      className="text-[11px] rounded-pill bg-sun-soft/70 hover:bg-sun-soft px-2.5 py-1 cursor-pointer inline-flex items-center gap-1"
                      aria-expanded={expanded}
                    >
                      AP2 receipt <span className={`transition-transform ${expanded ? 'rotate-90' : ''}`}>›</span>
                    </button>
                  )}
                  <span
                    className="text-[11px] rounded-pill px-2 py-0.5 shrink-0"
                    style={{ background: e.confirmed ? '#eef8ea' : 'rgba(36,35,33,0.06)', color: e.confirmed ? '#2e7d32' : undefined }}
                  >
                    {e.confirmed ? 'confirmed' : 'projected'}
                  </span>
                  <span className={`tabular font-medium w-24 text-right ${e.confirmed ? '' : 'text-ink-secondary'}`}>
                    {e.confirmed ? '' : '+'}
                    {brl(e.brl_recovered_cents)}
                  </span>
                </div>

                {expanded && (
                  <div className="px-3 pb-3 pop">
                    {rec ? <Trail rec={rec} /> : <div className="text-xs text-ink-tertiary">loading receipts…</div>}
                  </div>
                )}
              </div>
            )
          })}
        </div>

        <div className="mt-4 pt-3 border-t border-[rgba(36,35,33,0.08)] flex items-center gap-3 text-sm">
          <span className="text-ink-secondary flex-1">Bought back so far</span>
          <span className="tabular font-semibold text-success">{brl(totals.confirmed)}</span>
          <span className="text-ink-tertiary text-xs">· +{brl(totals.projected)} projected</span>
        </div>
      </section>

      <aside className="space-y-3">
        <Stat label="confirmed" value={brl(totals.confirmed)} hint="counted only after the week actually happened" tone="win" />
        <Stat label="projected" value={`+${brl(totals.projected)}`} hint="what approved automations should recover next" />
        <Stat label="hours back" value={`${totals.hrs.toFixed(1)} h`} hint="across every week on the ledger" />
        <div className="bg-surface/85 rounded-card hairline p-4 text-xs text-ink-secondary leading-relaxed">
          Purchases carry the full AP2 trail: two mandates you signed, two receipts the merchant signed. Open one and read it.
        </div>
      </aside>
    </div>
  )
}

function Stat({ label, value, hint, tone }: { label: string; value: string; hint: string; tone?: 'win' }) {
  return (
    <div className="bg-surface/85 rounded-card hairline shadow-[var(--shadow-lift)] p-4">
      <div className="text-xs text-ink-secondary">{label}</div>
      <div className={`tabular font-semibold text-xl leading-tight ${tone === 'win' ? 'text-success' : ''}`} style={{ fontFamily: 'var(--font-display)' }}>
        {value}
      </div>
      <div className="text-[11px] text-ink-tertiary mt-1">{hint}</div>
    </div>
  )
}

function Trail({ rec }: { rec: MandateRecord }) {
  const cm = decodeJwtPayload(rec.checkout_mandate)
  const cr = decodeJwtPayload(rec.checkout_receipt)
  const pm = decodeJwtPayload(rec.payment_mandate)
  const pr = decodeJwtPayload(rec.payment_receipt)
  const str = (o: Record<string, unknown> | null, k: string) => (o && o[k] != null ? String(o[k]) : '')
  const when = (o: Record<string, unknown> | null) => {
    const iat = o?.['iat']
    return typeof iat === 'number' ? new Date(iat * 1000).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : ''
  }
  const rows: { label: string; by: string; detail: string; ok: boolean }[] = [
    { label: 'Checkout Mandate', by: 'signed by you', detail: `${str(cm, 'vct') || 'mandate.checkout.1'} · ${when(cm)}`, ok: !!rec.checkout_mandate },
    { label: 'Checkout Receipt', by: 'signed by merchant', detail: `order ${str(cr, 'order_id') || '—'} · ${when(cr)}`, ok: rec.status === 'completed' || !!cr },
    { label: 'Payment Mandate', by: 'signed by you', detail: `${str(pm, 'vct') || 'mandate.payment.1'} · ${when(pm)}`, ok: !!rec.payment_mandate },
    {
      label: 'Payment Receipt',
      by: 'signed by processor',
      detail: `payment ${str(pr, 'payment_id') || '—'} · psp ${str(pr, 'psp_confirmation_id') || '—'}`,
      ok: rec.status === 'completed',
    },
  ]
  return (
    <div className="grid gap-1.5 sm:grid-cols-2">
      {rows.map((r) => (
        <div key={r.label} className="flex items-start gap-2 bg-surface-raised border-subtle rounded-xl px-3 py-2 text-xs">
          <span className="w-2 h-2 rounded-full shrink-0 mt-1" style={{ background: r.ok ? '#2e7d32' : '#b3261e' }} aria-hidden />
          <div className="min-w-0">
            <div className="font-medium">
              {r.label} <span className="text-ink-tertiary font-normal">· {r.by}</span>
            </div>
            <div className="text-ink-tertiary truncate">{r.detail}</div>
          </div>
        </div>
      ))}
      <div className="sm:col-span-2 text-[11px] text-ink-tertiary px-1">
        record <code>{rec.id}</code> · checkout <code>{rec.checkout_id}</code> · {rec.status}
      </div>
    </div>
  )
}
