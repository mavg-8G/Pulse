import { useEffect, useRef, useState } from 'react'
import { useMotion } from './AppShell'

/** Cuenta desde el último valor mostrado hasta `value`; salta directo si el movimiento está pausado o reducido. */
export function CountUp({ value, format, duration = 900 }: { value: number; format: (v: number) => string; duration?: number }) {
  const { motion } = useMotion()
  const from = useRef(motion ? 0 : value)
  const [shown, setShown] = useState(from.current)
  useEffect(() => {
    if (!motion || from.current === value) {
      from.current = value
      setShown(value)
      return
    }
    const start = from.current
    const t0 = performance.now()
    let raf = 0
    const step = (now: number) => {
      const p = Math.min(1, (now - t0) / duration)
      const v = p === 1 ? value : start + (value - start) * (1 - Math.pow(2, -10 * p))
      from.current = v
      setShown(v)
      if (p < 1) raf = requestAnimationFrame(step)
    }
    raf = requestAnimationFrame(step)
    return () => cancelAnimationFrame(raf)
  }, [value, motion, duration])
  return <>{format(shown)}</>
}
