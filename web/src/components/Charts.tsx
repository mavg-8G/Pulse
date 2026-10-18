import type { ReactNode } from 'react'

/** Acumulado del mes como área: línea del mes actual, la del mes anterior punteada y el tope como guía. */
export function PulseArea({ current, previous, previousName, days, budget, label }: { current: number[]; previous?: number[]; previousName?: string; days: number; budget?: number; label: string }) {
  const W = 600
  const H = 160
  const seen = Math.max(1, ...current, ...(previous ?? []))
  // El tope solo se dibuja si cabe sin aplastar la curva; si no, se menciona en el texto.
  const showBudget = !!budget && budget <= seen * 1.6
  const max = showBudget ? Math.max(seen, budget!) : seen * 1.1
  const x = (i: number) => (days <= 1 ? 0 : (i / (days - 1)) * W)
  const y = (v: number) => H - 6 - (v / max) * (H - 18)
  const line = (vals: number[]) => vals.map((v, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)} ${y(v).toFixed(1)}`).join(' ')
  const last = current.length - 1
  return (
    <div className="pulse-area" role="img" aria-label={label}>
      <div className="pulse-plot">
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" aria-hidden="true">
        <defs>
          <linearGradient id="pulse-area-fill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stopColor="var(--accent)" stopOpacity="0.28" />
            <stop offset="1" stopColor="var(--accent)" stopOpacity="0" />
          </linearGradient>
        </defs>
        {[0.25, 0.5, 0.75].map((f) => <line key={f} className="pulse-area-grid" x1="0" x2={W} y1={H * f} y2={H * f} />)}
        {showBudget ? <line className="pulse-area-budget" x1="0" x2={W} y1={y(budget!)} y2={y(budget!)} /> : null}
        {previous && previous.length > 1 && <path className="pulse-area-prev" d={line(previous)} />}
        {last >= 1 && (
          <g className="pulse-area-now">
            <path d={`${line(current)} L${x(last)} ${H} L0 ${H} Z`} fill="url(#pulse-area-fill)" />
            <path className="pulse-area-line" d={line(current)} />
          </g>
        )}
      </svg>
      {last >= 0 && <span className="pulse-area-dot" style={{ left: `${(x(last) / W) * 100}%`, top: `${(y(current[last]) / H) * 100}%` }} aria-hidden="true" />}
      </div>
      <div className="pulse-axis" aria-hidden="true"><span>1</span>{last > 0 && last < days - 1 && <span style={{ left: `${(x(last) / W) * 100}%` }}>{last + 1}</span>}<span>{days}</span></div>
      <div className="pulse-legend" aria-hidden="true"><span><i className="k-now" />Este mes</span>{previous && previous.length > 1 && <span><i className="k-prev" />{previousName}</span>}{showBudget && <span><i className="k-budget" />Presupuesto</span>}</div>
    </div>
  )
}

/** Anillo de avance (0–100) con el contenido centrado. */
export function Ring({ percent, tone = 'ok', children, size = 132 }: { percent: number; tone?: string; children?: ReactNode; size?: number }) {
  const p = Math.max(0, Math.min(100, percent))
  return (
    <div className="ring" style={{ width: size, height: size }}>
      <svg viewBox="0 0 120 120" aria-hidden="true">
        <circle className="ring-track" cx="60" cy="60" r="52" pathLength={100} />
        <circle className={`ring-value bg-stroke-${tone}`} cx="60" cy="60" r="52" pathLength={100} style={{ strokeDasharray: `${p} 100` }} />
      </svg>
      <div className="ring-center">{children}</div>
    </div>
  )
}

/** Dona por categoría; los segmentos usan el color de cada categoría. */
export function Donut({ parts, children, size = 132 }: { parts: { key: string; value: number; color: string }[]; children?: ReactNode; size?: number }) {
  const total = parts.reduce((a, p) => a + p.value, 0)
  let start = 0
  return (
    <div className="ring donut" style={{ width: size, height: size }}>
      <svg viewBox="0 0 120 120" aria-hidden="true">
        <circle className="ring-track" cx="60" cy="60" r="52" pathLength={100} />
        {total > 0 && parts.map((p, i) => {
          const len = (p.value / total) * 100
          const seg = <circle key={p.key} className="donut-seg" cx="60" cy="60" r="52" pathLength={100} stroke={p.color} style={{ strokeDasharray: `${Math.max(0, len - 0.8)} 100`, strokeDashoffset: -start, animationDelay: `${200 + i * 90}ms` }} />
          start += len
          return seg
        })}
      </svg>
      <div className="ring-center">{children}</div>
    </div>
  )
}
