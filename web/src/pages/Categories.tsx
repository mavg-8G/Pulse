import { useConfirm } from '../components/Confirm'
import { useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { get } from '../lib/api'
import { Link } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { Sheet } from '../components/Sheet'
import { useToast } from '../components/Toast'
import { del, errorMessage, patch, post } from '../lib/api'
import { financeKeys, useCategories } from '../lib/queries'
import type { Category } from '../lib/types'

export function Categories() {
  const { data: cats = [] } = useCategories()
  const [edit, setEdit] = useState<{ open: boolean; cat?: Category }>({ open: false })
  const tops = cats.filter((c) => !c.parent_id)
  return (
    <>
      <p className="small"><Link to="/ajustes">Ajustes</Link></p>
      <div className="page-head">
        <h1>Categorías</h1>
        <button className="btn btn-primary" onClick={() => setEdit({ open: true })}>Agregar categoría</button>
      </div>
      <ul className="list">
        {tops.map((c) => (
          <li key={c.id}>
            <button className="tx" onClick={() => setEdit({ open: true, cat: c })}>
              <span className="tx-icon" style={{ background: `color-mix(in srgb, ${c.color} 20%, transparent)` }} aria-hidden="true">{c.icon}</span>
              <span className="tx-main">
                <span className="tx-title">{c.name}</span>
                <span className="tx-meta">
                  {c.is_system
                    ? 'Aquí caen los gastos de categorías borradas'
                    : cats.filter((x) => x.parent_id === c.id).map((x) => x.name).join(', ') || 'Sin subcategorías'}
                </span>
              </span>
              <span className="faint">›</span>
            </button>
          </li>
        ))}
      </ul>
      <RulesSection cats={cats} />
      <Sheet open={edit.open} title={edit.cat ? 'Editar categoría' : 'Nueva categoría'} onClose={() => setEdit({ open: false })}>
        <CategoryForm key={edit.cat?.id ?? 'new'} cat={edit.cat} all={cats} onDone={() => setEdit({ open: false })} onEdit={(c) => setEdit({ open: true, cat: c })} />
      </Sheet>
    </>
  )
}

function CategoryForm({ cat, all, onDone, onEdit }: { cat?: Category; all: Category[]; onDone: () => void; onEdit: (c: Category) => void }) {
  const ask = useConfirm()
  const qc = useQueryClient()
  const toast = useToast()
  const [name, setName] = useState(cat?.name ?? '')
  const [icon, setIcon] = useState(cat?.icon ?? '🏷️')
  const [color, setColor] = useState(cat?.color ?? '#2a9d8f')
  const [parent, setParent] = useState(cat?.parent_id ?? '')
  const [error, setError] = useState('')
  const subs = cat ? all.filter((c) => c.parent_id === cat.id) : []
  const parents = all.filter((c) => !c.parent_id && c.id !== cat?.id && !c.is_system)
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['categories'] })
    financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
  }
  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    try {
      const body = { name, icon, color, parent_id: parent }
      if (cat) await patch(`/api/categories/${cat.id}`, body)
      else await post('/api/categories', body)
      refresh()
      toast('Categoría guardada')
      onDone()
    } catch (err) {
      setError(errorMessage(err))
    }
  }
  async function remove() {
    if (!cat || !(await ask({ title: `¿Borrar ${cat.name}?`, body: 'Sus gastos pasarán a “Otros”.', confirm: 'Borrar categoría', danger: true }))) return
    try {
      const r = await del<{ moved_transactions: number }>(`/api/categories/${cat.id}`)
      refresh()
      toast(r.moved_transactions ? `Categoría borrada. ${r.moved_transactions} gastos pasaron a Otros.` : 'Categoría borrada')
      onDone()
    } catch (err) {
      setError(errorMessage(err))
    }
  }
  return (
    <form onSubmit={submit} className="stack">
      <label className="field">
        <span>Nombre</span>
        <input className="input" required maxLength={40} value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <div className="grid-2">
        <label className="field">
          <span>Ícono</span>
          <input className="input" maxLength={4} value={icon} onChange={(e) => setIcon(e.target.value)} />
        </label>
        <label className="field">
          <span>Color</span>
          <input className="input" type="color" value={color} onChange={(e) => setColor(e.target.value)} style={{ padding: 4 }} />
        </label>
      </div>
      {!cat?.is_system && subs.length === 0 && (
        <label className="field">
          <span>Dentro de</span>
          <select className="select" value={parent} onChange={(e) => setParent(e.target.value)}>
            <option value="">Ninguna (categoría principal)</option>
            {parents.map((c) => (
              <option key={c.id} value={c.id}>{c.icon} {c.name}</option>
            ))}
          </select>
        </label>
      )}
      {subs.length > 0 && (
        <div>
          <p className="small muted" style={{ marginBottom: 6 }}>Subcategorías</p>
          <div className="chips">
            {subs.map((s) => (
              <button type="button" className="chip" key={s.id} onClick={() => onEdit(s)}>{s.name}</button>
            ))}
          </div>
        </div>
      )}
      {error && <p className="error" role="alert">{error}</p>}
      <button className="btn btn-primary btn-block">Guardar categoría</button>
      {cat && !cat.is_system && (
        <button type="button" className="btn btn-danger btn-block" onClick={remove}>Borrar categoría</button>
      )}
    </form>
  )
}

function RulesSection({ cats }: { cats: Category[] }) {
  const qc = useQueryClient()
  const toast = useToast()
  const rules = useQuery({ queryKey: ['rules'], queryFn: () => get<{ id: string; pattern: string; category_id: string }[]>('/api/merchant-rules') })
  const byId = new Map(cats.map((c) => [c.id, c]))
  async function remove(id: string) {
    await del(`/api/merchant-rules/${id}`)
    qc.invalidateQueries({ queryKey: ['rules'] })
    toast('Regla quitada')
  }
  return (
    <section className="section" aria-labelledby="h-rules">
      <div className="section-head"><h2 id="h-rules">Reglas automáticas</h2></div>
      <p className="muted small" style={{ marginBottom: 10 }}>
        Los gastos importados de correos usan estas reglas para elegir la categoría. Se crean al corregir la categoría de un gasto y marcar “Usar siempre”.
      </p>
      {(rules.data ?? []).length === 0 ? (
        <div className="panel empty"><p>Todavía no hay reglas.</p></div>
      ) : (
        <ul className="list">
          {rules.data!.map((r) => {
            const c = byId.get(r.category_id)
            return (
              <li key={r.id} className="settings-row">
                <span className="grow">“{r.pattern}” va a {c?.icon} {c?.name ?? 'categoría borrada'}</span>
                <button className="btn btn-sm btn-danger" onClick={() => remove(r.id)}>Quitar</button>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
