import { useEffect, useRef, useState } from 'react'

/**
 * Animates a number toward `target` (ease-out, ~700ms). The hero figure moves
 * when the agent changes the data — that motion is the product's proof.
 * Respects prefers-reduced-motion by jumping straight to the target.
 */
export function useCountUp(target: number, duration = 700): number {
  const [value, setValue] = useState(0)
  const from = useRef(0)
  const raf = useRef(0)

  useEffect(() => {
    const reduce = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
    if (reduce) {
      from.current = target
      setValue(target)
      return
    }
    const start = performance.now()
    const begin = from.current
    const delta = target - begin
    if (delta === 0) return
    const tick = (now: number) => {
      const t = Math.min((now - start) / duration, 1)
      const eased = 1 - Math.pow(1 - t, 3)
      const v = begin + delta * eased
      setValue(v)
      if (t < 1) raf.current = requestAnimationFrame(tick)
      else from.current = target
    }
    raf.current = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf.current)
  }, [target, duration])

  return value
}
