import { useState } from 'react'
import { money, moneyShort } from '../lib/money'

export interface ReportSeries {
  name: string
  values: number[]
  secondary?: boolean
}

/** SVG stays dependency-free; a keyboard-accessible date selector exposes exact values. */
export function ReportLineChart({ label, labels, series, projected = false }: { label: string; labels: string[]; series: ReportSeries[]; projected?: boolean }) {
  const [selected, setSelected] = useState<number | null>(null)
  const active = Math.min(selected ?? labels.length - 1, labels.length - 1)
  const width = 800, height = 260, left = 78, right = 28, top = 24, bottom = 38
  const values = series.flatMap((s) => s.values)
  const low = Math.min(0, ...values), high = Math.max(100, ...values)
  const padding = (high - low) * 0.12
  const min = low < 0 ? low - padding : 0, max = high + padding
  const x = (i: number) => left + i / Math.max(1, labels.length - 1) * (width - left - right)
  const y = (v: number) => top + (max - v) / (max - min) * (height - top - bottom)
  const ticks = new Set(Array.from({ length: Math.min(7, labels.length) }, (_, i) => Math.round(i * (labels.length - 1) / Math.max(1, Math.min(6, labels.length - 1)))))
  return (
    <div className="report-line-chart">
      <div className="report-chart-scroll" role="group" aria-label={`Gráfica: ${label.toLowerCase()}`} tabIndex={0}>
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
          {series.map((s) => (
            <g key={s.name} className={s.secondary ? 'report-series-secondary' : 'report-series-total'}>
              <path d={s.values.map((v, i) => `${i === 0 ? 'M' : 'L'}${x(i)},${y(v)}`).join(' ')} className={`report-chart-line ${projected ? 'is-projected' : ''}`} />
              {s.values.map((v, i) => <circle key={i} cx={x(i)} cy={y(v)} r={i === active ? 5 : 3} className="report-chart-point" />)}
            </g>
          ))}
        </svg>
      </div>
      <div className="report-chart-detail">
        <label className="small muted">{label}<select className="input" value={active} onChange={(e) => setSelected(Number(e.target.value))}>{labels.map((text, i) => <option key={i} value={i}>{text}</option>)}</select></label>
        <div className="report-chart-legend" aria-live="polite">
          {series.map((s) => <span key={s.name}><i className={s.secondary ? 'legend-secondary' : 'legend-total'} aria-hidden="true" />{s.name} <strong className="num">{money(s.values[active] ?? 0)}</strong></span>)}
        </div>
      </div>
    </div>
  )
}
