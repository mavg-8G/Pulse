import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { join } from 'node:path'

const dir = fileURLToPath(new URL('../../artifacts/quality/', import.meta.url))
const before = JSON.parse(readFileSync(join(dir, 'perf-before.json'), 'utf8'))
const after = JSON.parse(readFileSync(join(dir, 'perf-after.json'), 'utf8'))
const median = (values) => [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)]
const rows = []
for (const count of [0, 100, 5000]) for (const path of ['/', '/movimientos', '/plan', '/reportes']) {
  const a = before.samples.map((s, i) => ({ ...s, cls: s.cls / (before.cls_collectors === 1 ? 1 : i + 1) })).filter((s) => s.count === count && s.path === path)
  const b = after.samples.filter((s) => s.count === count && s.path === path)
  if (a.length !== 5 || b.length !== 5) throw new Error(`Faltan muestras: ${count} ${path}`)
  const old = median(a.map((s) => s.elapsed))
  const now = median(b.map((s) => s.elapsed))
  rows.push({ count, path, before_ms: old, after_ms: now, change_pct: Math.round((now / old - 1) * 1000) / 10,
    before_requests: median(a.map((s) => s.requests)), after_requests: median(b.map((s) => s.requests)),
    before_cls: median(a.map((s) => s.cls)), after_cls: median(b.map((s) => s.cls)), within_budget: now <= old * 1.1 })
}
writeFileSync(join(dir, 'comparison.json'), JSON.stringify({ cls_reference_correction: 'before tenía i+1 observadores por documento; after tiene uno', rows }, null, 2))
console.log('| Filas | Vista | Antes (ms) | Después (ms) | Cambio | API antes → después | CLS antes → después |')
console.log('|---:|---|---:|---:|---:|---:|---:|')
for (const r of rows) console.log(`| ${r.count} | ${r.path} | ${r.before_ms} | ${r.after_ms} | ${r.change_pct}% | ${r.before_requests} → ${r.after_requests} | ${r.before_cls.toFixed(4)} → ${r.after_cls.toFixed(4)} |`)
if (rows.some((r) => !r.within_budget)) console.log('Revisar excepciones al presupuesto del 10 %:', rows.filter((r) => !r.within_budget).map((r) => `${r.count} ${r.path}`).join(', '))
