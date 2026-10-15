import { useMemo } from 'react'
import { money } from '../lib/money'

/**
 * El "pulso" del mes: cada día es un latido cuya altura es lo gastado ese día.
 * Días sin gasto quedan planos sobre la línea base. El punto marca hoy.
 */
export function PulseTrace({ daily, today }: { daily: number[]; today: number | null }) {
  const W = 620
  const H = 130
  const base = H - 18
  const { d, todayX, peakDay } = useMemo(() => {
    const n = Math.max(daily.length, 1)
    const step = W / n
    const max = Math.max(...daily.map((v) => Math.max(v, 0)), 1)
    let path = `M0 ${base}`
    const to = (x: number, y: number) => {
      path += ` L${x.toFixed(1)} ${y.toFixed(1)}`
    }
    let peak = -1
    let peakV = 0
    const last = today ?? n
    daily.forEach((v, i) => {
      if (i >= last) return
      const x0 = i * step
      const mid = x0 + step / 2
      if (v > 0) {
        const h = Math.max(6, (v / max) * (base - 8))
        to(mid - step * 0.28, base)
        to(mid - step * 0.12, base - h)
        to(mid + step * 0.06, base + Math.min(10, h * 0.12))
        to(mid + step * 0.22, base)
        if (v > peakV) {
          peakV = v
          peak = i
        }
      }
      to(x0 + step, base)
    })
    return { d: path, todayX: today ? (today - 0.5) * step : null, peakDay: peak }
  }, [daily, today])

  const total = daily.reduce((a, b) => a + b, 0)
  const empty = !daily.some((v) => v > 0)
  const label =
    total > 0 && peakDay >= 0
      ? `Gasto diario del mes. El día con más gasto fue el ${peakDay + 1}, con ${money(daily[peakDay])}.`
      : 'Sin gastos registrados este mes.'

  return (
    <figure className={`pulse ${empty ? 'pulse-empty' : ''}`}>
      <div className="pulse-plot">
        <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={label}>
          {[0.25, 0.5, 0.75].map((part) => <line key={part} className="pulse-gridline" x1="0" x2={W} y1={base * part} y2={base * part} vectorEffect="non-scaling-stroke" />)}
          <line className="base" x1="0" x2={W} y1={base} y2={base} vectorEffect="non-scaling-stroke" />
          <path className="trace" d={d} vectorEffect="non-scaling-stroke" />
        </svg>
        {empty && <div className="pulse-idle" aria-hidden="true"><svg viewBox="0 0 240 70"><path className="idle-wave-base" d="M0 42h70l12-14 12 14h13l11-34 17 56 15-38 12 16h78" /><path className="idle-wave" pathLength="100" d="M0 42h70l12-14 12 14h13l11-34 17 56 15-38 12 16h78" /></svg><span>Tu historia empieza con un gasto</span></div>}
        {todayX !== null && <span className="pulse-dot" style={{ left: `${(todayX / W) * 100}%`, top: `${(base / H) * 100}%` }} />}
      </div>
      <figcaption className="axis" aria-hidden="true">
        <span>1</span>
        <span>{Math.ceil(daily.length / 2)}</span>
        <span>{daily.length}</span>
      </figcaption>
    </figure>
  )
}
