import { useEffect, useRef, useState } from 'react'
import { money, moneyShort } from '../lib/money'

export interface ReportSeries {
  name: string
  values: (number | null)[] // null: sin dato ese punto (la línea se corta)
  secondary?: boolean // línea de comparación (periodo anterior)
  dashed?: boolean // proyección: nunca un dato real
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

const seriesClass = (s: ReportSeries) => (s.tone !== undefined ? `report-series-tone-${s.tone % 6}` : s.dashed ? 'report-series-projection' : s.secondary ? 'report-series-secondary' : 'report-series-total')
const legendClass = (s: ReportSeries) => `${s.tone !== undefined ? `legend-tone-${s.tone % 6}` : s.dashed ? 'legend-projection' : s.secondary ? 'legend-secondary' : 'legend-total'} ${s.dashed ? 'legend-dashed' : ''}`

/** Ancho disponible del contenedor: la gráfica se dibuja a ese ancho y no se encoge el texto en móvil. */
function useWidth() {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(720)
  useEffect(() => {
    const el = ref.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(([e]) => setWidth(Math.max(280, Math.round(e.contentRect.width))))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, width] as const
}

/**
 * Gráfica de líneas SVG sin dependencias. Al tocar o pasar sobre un punto (o con las flechas del teclado) muestra
 * los valores exactos; la leyenda permite ocultar series. Los datos reales van en línea continua, la proyección
 * punteada y su rango como banda.
 */
export function ReportLineChart({ label, labels, details, series, initial }: { label: string; labels: string[]; details?: string[]; series: ReportSeries[]; projected?: boolean; initial?: number }) {
  const [selected, setSelected] = useState<number | null>(null)
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const [ref, width] = useWidth()
  const narrow = width < 560
  const active = Math.max(0, Math.min(selected ?? initial ?? labels.length - 1, labels.length - 1))
  const visible = series.filter((s) => !hidden.has(s.name))
  const height = narrow ? 210 : 260, left = narrow ? 50 : 72, right = 14, top = 18, bottom = 32
  const values = visible.flatMap((s) => [...s.values, ...(s.band?.low ?? []), ...(s.band?.high ?? [])]).filter((v): v is number => v !== null && Number.isFinite(v))
  const low = Math.min(0, ...values), high = Math.max(100, ...values)
  const padding = (high - low) * 0.1
  const min = low < 0 ? low - padding : 0, max = high + padding
  const x = (i: number) => left + (i / Math.max(1, labels.length - 1)) * (width - left - right)
  const y = (v: number) => top + ((max - v) / (max - min)) * (height - top - bottom)
  const tickCount = Math.min(narrow ? 4 : 7, labels.length)
  const ticks = new Set(Array.from({ length: tickCount }, (_, i) => Math.round((i * (labels.length - 1)) / Math.max(1, tickCount - 1))))
  const toggle = (name: string) => setHidden((h) => {
    const n = new Set(h)
    if (n.has(name)) n.delete(name)
    else if (series.length - n.size > 1) n.add(name) // siempre queda al menos una serie
    return n
  })
  const tipLeft = Math.min(Math.max(x(active), 90), width - 90)
  return (
    <div className="report-line-chart">
      <div className="report-chart-legend" role="group" aria-label="Series de la gráfica">
        {series.map((s) => (
          <button key={s.name} type="button" className="report-legend-item" aria-pressed={!hidden.has(s.name)} onClick={() => toggle(s.name)}>
            <i className={legendClass(s)} aria-hidden="true" />
            {s.name}
            {s.band && <span className="report-legend-band" aria-hidden="true" />}
          </button>
        ))}
      </div>
      <div ref={ref} className="report-chart-scroll" role="group" aria-label={`Gráfica: ${label.toLowerCase()}. Usa las flechas para recorrer los puntos.`} tabIndex={0}
        onKeyDown={(e) => {
          const step: Record<string, number> = { ArrowRight: 1, ArrowLeft: -1, Home: -labels.length, End: labels.length }
          if (e.key in step) {
            e.preventDefault()
            setSelected(Math.max(0, Math.min(labels.length - 1, active + step[e.key])))
          }
        }}>
        <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden="true" className="report-chart-svg"
          onPointerMove={(event) => {
            const bounds = event.currentTarget.getBoundingClientRect()
            const position = ((event.clientX - bounds.left) / bounds.width) * width
            setSelected(Math.max(0, Math.min(labels.length - 1, Math.round(((position - left) / (width - left - right)) * (labels.length - 1)))))
          }}>
          {Array.from({ length: 5 }, (_, i) => {
            const value = min + ((max - min) * i) / 4
            return <g key={i}><line x1={left} x2={width - right} y1={y(value)} y2={y(value)} className="report-chart-grid" /><text x={left - 8} y={y(value) + 4} textAnchor="end">{moneyShort(value)}</text></g>
          })}
          {low < 0 && <line x1={left} x2={width - right} y1={y(0)} y2={y(0)} className="report-chart-zero" />}
          {labels.map((text, i) => ticks.has(i) && <text key={i} x={x(i)} y={height - 8} textAnchor={i === 0 ? 'start' : i === labels.length - 1 ? 'end' : 'middle'}>{text}</text>)}
          <line x1={x(active)} x2={x(active)} y1={top} y2={height - bottom} className="report-chart-guide" />
          {visible.map((s) => s.band && (() => {
            const pts = s.band.low.map((lo, i) => (lo === null || s.band!.high[i] === null ? null : i)).filter((i): i is number => i !== null)
            if (pts.length < 2) return null
            const d = `M${pts.map((i) => `${x(i)},${y(s.band!.high[i]!)}`).join(' L')} L${[...pts].reverse().map((i) => `${x(i)},${y(s.band!.low[i]!)}`).join(' L')} Z`
            return <path key={`${s.name}-band`} d={d} className={`report-chart-band ${seriesClass(s)}`} />
          })())}
          {visible.map((s) => (
            <g key={s.name} className={seriesClass(s)}>
              <path d={path(s.values, x, y)} className={`report-chart-line ${s.dashed ? 'is-projected' : ''}`} />
              {s.values.map((v, i) => v !== null && ((!s.secondary && labels.length <= (narrow ? 14 : 31)) || i === active) && <circle key={i} cx={x(i)} cy={y(v)} r={i === active ? 5 : narrow ? 2.5 : 3} className="report-chart-point" />)}
            </g>
          ))}
        </svg>
        <div className="report-chart-tip" style={{ left: tipLeft }} aria-hidden="true">
          <strong>{details?.[active] ?? labels[active]}</strong>
          {visible.map((s) => s.values[active] !== null && s.values[active] !== undefined && (
            <span key={s.name}><i className={legendClass(s)} />{s.name}: <b className="num">{money(s.values[active]!)}</b></span>
          ))}
        </div>
      </div>
      <div className="report-chart-detail">
        <label className="small muted">{label}<select className="input" value={active} onChange={(e) => setSelected(Number(e.target.value))}>{labels.map((text, i) => <option key={i} value={i}>{details?.[i] ?? text}</option>)}</select></label>
        <p className="report-chart-values small" aria-live="polite">
          {series.map((s) => (
            <span key={s.name}>
              {s.name}: <strong className="num">{s.values[active] === null || s.values[active] === undefined ? '—' : money(s.values[active]!)}</strong>
              {s.band && s.band.low[active] != null && s.band.high[active] != null && <span className="muted"> ({s.band.name} {money(s.band.low[active]!)} – {money(s.band.high[active]!)})</span>}
            </span>
          ))}
        </p>
      </div>
    </div>
  )
}
