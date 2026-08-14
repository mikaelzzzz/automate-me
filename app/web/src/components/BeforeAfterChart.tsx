import { useState } from 'react'
import type { PnlTask, Proposal } from '../lib/api'
import { brl } from '../lib/api'

// Before/After: monthly cost of each routine now (gold, the leak) vs with the
// best automation applied (blue). Paired thin horizontal bars, 4px rounded
// data ends, 2px surface gap; direct labels on the leak only.
const GOLD = '#a07c12'
const BLUE = '#2c5fa8'

export function BeforeAfterChart({
  tasks,
  proposals,
}: {
  tasks: PnlTask[]
  proposals: Proposal[]
}) {
  const [hover, setHover] = useState<string | null>(null)

  const rows = tasks
    .map((t) => {
      const best = proposals
        .filter((p) => p.task_id === t.id)
        .sort((a, b) => b.net_monthly_cents - a.net_monthly_cents)[0]
    const after = best ? Math.max(t.cost_cents_month - best.net_monthly_cents, 0) : t.cost_cents_month
      return { task: t, after, automated: !!best }
    })
    .sort((a, b) => b.task.cost_cents_month - a.task.cost_cents_month)
    .slice(0, 5)

  const max = Math.max(...rows.map((r) => r.task.cost_cents_month), 1)

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-4 text-xs text-ink-secondary">
        <span className="inline-flex items-center gap-1.5">
          <span className="w-3 h-2 rounded-sm" style={{ background: GOLD }} /> Today (leaking)
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="w-3 h-2 rounded-sm" style={{ background: BLUE }} /> With automation
        </span>
      </div>

      {rows.map(({ task, after, automated }) => {
        const wNow = (task.cost_cents_month / max) * 100
        const wAfter = (after / max) * 100
        const isHover = hover === task.id
        return (
          <div
            key={task.id}
            className="group"
            onMouseEnter={() => setHover(task.id)}
            onMouseLeave={() => setHover(null)}
          >
            <div className="flex items-baseline justify-between mb-1">
              <span className="text-sm text-ink">{task.name}</span>
              <span className="text-xs tabular font-medium" style={{ color: GOLD }}>
                {brl(task.cost_cents_month)}/mo
              </span>
            </div>
            <div className="space-y-0.5">
              <div className="h-3 rounded-[4px] transition-all" style={{ width: `${wNow}%`, background: GOLD, opacity: isHover ? 1 : 0.9 }} />
              <div
                className="h-3 rounded-[4px] transition-all"
                style={{ width: `${Math.max(wAfter, 0.5)}%`, background: automated ? BLUE : 'rgba(36,35,33,0.12)' }}
              />
            </div>
            {isHover && automated && (
              <div className="text-xs text-ink-secondary mt-1">
                recover {brl(task.cost_cents_month - after)}/mo — {brl((task.cost_cents_month - after) * 12)} a year
              </div>
            )}
            {isHover && !automated && (
              <div className="text-xs text-ink-tertiary mt-1">no automation matched yet — ask the agent</div>
            )}
          </div>
        )
      })}
    </div>
  )
}
