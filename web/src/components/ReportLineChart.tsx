import { useState } from 'react'
import { money, moneyShort } from '../lib/money'

export interface ReportSeries {
  name: string
  values: (number | null)[] // null: sin dato ese punto (la línea se corta)
  secondary?: boolean
  dashed?: boolean // proyección
  tone?: number // 0–5: color de la serie cuando hay varias (cuentas)
  band?: { low: (number | null)[]; high: (number | null)[]; name: string } // rango de la proyección
}

function path(values: (number | null)[], x: (i: number) => number, y: (v: number) => number) {
  let d = ''
  let pen = false
  values.forEach((v, i) => {
    if (v === null) {
      pen = false
      return
    }
    d += `${pen ? 'L' : 'M'}${x(i)},${y(v)} `
    pen = true
  })
  return d.trim()
}

const seriesClass = (s: ReportSeries) => (s.tone !== undefined ? `report-series-tone-${s.tone % 6}` : s.secondary ? 'report-series-secondary' : 'report-series-total')

/** SVG sin dependencias; un selector accesible con teclado muestra los valores exactos de cada punto. */
export function ReportLineChart({ label, labels, series, projected = false, initial }: { label: string; labels: string[]; series: ReportSeries[]; projected?: boolean; initial?: number }) {
  const [selected, setSelected] = useState<number | null>(null)
  const active = Math.min(selected ?? initial ?? labels.length - 1, labels.length - 1)
  const width = 800, height = 260, left = 78, right = 28, top = 24, bottom = 38
  const values = series.flatMap((s) => [...s.values, ...(s.band?.low ?? []), ...(s.band?.high ?? [])]).filter((v): v is number => v !== null && Number.isFinite(v))
  const low = Math.min(0, ...values), high = Math.max(100, ...values)
  const padding = (high - low) * 0.12
  const min = low < 0 ? low - padding : 0, max = high + padding
  const x = (i: number) => left + i / Math.max(1, labels.length - 1) * (width - left - right)
  const y = (v: number) => top + (max - v) / (max - min) * (height - top - bottom)
  const ticks = new Set(Array.from({ length: Math.min(7, labels.length) }, (_, i) => Math.round(i * (labels.length - 1) / Math.max(1, Math.min(6, labels.length - 1)))))
  return (
    <div className="report-line-chart">
      <div className="report-chart-scroll" role="group" aria-label={`Gráfica: ${label.toLowerCase()}`} tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
            e.preventDefault()
            setSelected(Math.max(0, Math.min(labels.length - 1, active + (e.key === 'ArrowRight' ? 1 : -1))))
          }
        }}>
        <svg viewBox={`0 0 ${width} ${height}`} aria-hidden="true" className="report-chart-svg" onPointerMove={(event) => {
          const bounds = event.currentTarget.getBoundingClientRect()
          const position = (event.clientX - bounds.left) / bounds.width * width
          setSelected(Math.max(0, Math.min(labels.length - 1, Math.round((position - left) / (width - left - right) * (labels.length - 1)))))
        }}>
          {Array.from({ length: 5 }, (_, i) => {
            const value = min + (max - min) * i / 4
            return <g key={i}><line x1={left} x2={width - right} y1={y(value)} y2={y(value)} className="report-chart-grid" /><text x={left - 12} y={y(value) + 4} textAnchor="end">{moneyShort(value)}</text></g>
          })}
          {low < 0 && <line x1={left} x2={width - right} y1={y(0)} y2={y(0)} className="report-chart-zero" />}
          {labels.map((text, i) => ticks.has(i) && <text key={i} x={x(i)} y={height - 10} textAnchor="middle">{text}</text>)}
          <line x1={x(active)} x2={x(active)} y1={top} y2={height - bottom} className="report-chart-guide" />
          {series.map((s) => s.band && (() => {
            const pts = s.band.low.map((lo, i) => (lo === null || s.band!.high[i] === null ? null : i)).filter((i): i is number => i !== null)
            if (pts.length < 2) return null
            const d = `M${pts.map((i) => `${x(i)},${y(s.band!.high[i]!)}`).join(' L')} L${[...pts].reverse().map((i) => `${x(i)},${y(s.band!.low[i]!)}`).join(' L')} Z`
            return <path key={`${s.name}-band`} d={d} className={`report-chart-band ${seriesClass(s)}`} />
          })())}
          {series.map((s) => (
            <g key={s.name} className={seriesClass(s)}>
              <path d={path(s.values, x, y)} className={`report-chart-line ${projected || s.dashed ? 'is-projected' : ''}`} />
              {s.values.map((v, i) => v !== null && (labels.length <= 40 || i === active) && <circle key={i} cx={x(i)} cy={y(v)} r={i === active ? 5 : 3} className="report-chart-point" />)}
            </g>
          ))}
        </svg>
      </div>
      <div className="report-chart-detail">
        <label className="small muted">{label}<select className="input" value={active} onChange={(e) => setSelected(Number(e.target.value))}>{labels.map((text, i) => <option key={i} value={i}>{text}</option>)}</select></label>
        <div className="report-chart-legend" aria-live="polite">
          {series.map((s) => (
            <span key={s.name}>
              <i className={`${s.tone !== undefined ? `legend-tone-${s.tone % 6}` : s.secondary ? 'legend-secondary' : 'legend-total'} ${s.dashed ? 'legend-dashed' : ''}`} aria-hidden="true" />
              {s.name} <strong className="num">{s.values[active] === null || s.values[active] === undefined ? '—' : money(s.values[active]!)}</strong>
              {s.band && s.band.low[active] !== null && s.band.low[active] !== undefined && <span className="muted small">({s.band.name} {money(s.band.low[active]!)} – {money(s.band.high[active]!)})</span>}
            </span>
          ))}
        </div>
      </div>
    </div>
  )
}
