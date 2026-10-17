import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, act, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { clearViewContext, movementView, setViewOwner, useDebounced, useMovementView } from './viewState'
import { ApiError, errorMessage, get, onStepUpRequired, post } from './api'
import { fetchTransactions } from './queries'
import { QueryClient } from '@tanstack/react-query'
import { warmCache } from './offline'

afterEach(() => { cleanup(); clearViewContext(); onStepUpRequired(null); vi.unstubAllGlobals(); vi.useRealTimers() })
describe('contexto de navegación', () => {
  it('la preparación sin conexión comparte lecturas con las vistas y otras preparaciones simultáneas', async () => {
    const client = new QueryClient()
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([]), { status: 200 })))
    try {
      await Promise.all([warmCache(client), warmCache(client)])
      expect(fetch).toHaveBeenCalledTimes(6)
      await client.fetchQuery({ queryKey: ['categories'], queryFn: () => get('/api/categories'), staleTime: 30_000 })
      expect(fetch).toHaveBeenCalledTimes(6)
    } finally { client.clear() }
  })
  it('mostrar más supera 200 filas mediante paginación válida y conserva el total del periodo', async () => {
    const calls: URLSearchParams[] = []
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      const p = new URL(path, 'https://pulse.test').searchParams
      calls.push(p)
      const offset = Number(p.get('offset') ?? 0)
      const limit = Number(p.get('limit') ?? 50)
      return new Response(JSON.stringify({ items: Array.from({ length: Math.min(limit, 301 - offset) }, (_, i) => ({ id: offset + i })), total: 301, sum_cents: 30100 }), { status: 200 })
    }))
    const result = await fetchTransactions({ month: '2026-10', limit: 350 })
    expect(result.items).toHaveLength(301)
    expect(new Set(result.items.map((t) => t.id)).size).toBe(301)
    expect(result.sum_cents).toBe(30100)
    expect(calls.map((p) => [p.get('limit'), p.get('offset')])).toEqual([['200', null], ['150', '200']])
  })
  it('rechaza parámetros dañados sin conservar texto sensible de la URL', () => {
    expect(movementView(new URLSearchParams('month=2026-99&limit=99999&account=<script>&q=privado'), '2026-10')).toEqual({ month: '2026-10', limit: 50, account: '', category: '' })
    expect(movementView(new URLSearchParams('month=2025-12&limit=100&category=abc-12'), '2026-10')).toEqual({ month: '2025-12', limit: 100, account: '', category: 'abc-12' })
  })
  it('agrupa escritura rápida en una sola búsqueda sin retrasar la entrada visible', () => {
    vi.useFakeTimers()
    function Value({ text }: { text: string }) { return <output>{useDebounced(text)}</output> }
    const r = render(<Value text="" />)
    r.rerender(<Value text="s" />)
    r.rerender(<Value text="su" />)
    r.rerender(<Value text="super" />)
    expect(screen.getByRole('status').textContent).toBe('')
    act(() => vi.advanceTimersByTime(250))
    expect(screen.getByRole('status').textContent).toBe('super')
  })
  it('borra contexto y búsqueda al cambiar de persona; nunca persiste la búsqueda', () => {
    function Probe() { const v = useMovementView('2026-10'); return <input aria-label="Buscar" value={v.q} onChange={(e) => v.setQ(e.target.value)} /> }
    setViewOwner('ana')
    const a = render(<MemoryRouter><Probe /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText('Buscar'), { target: { value: 'comercio privado' } })
    expect(screen.getByLabelText<HTMLInputElement>('Buscar').value).toBe('comercio privado')
    expect(JSON.stringify(sessionStorage)).not.toContain('comercio privado')
    a.unmount()
    setViewOwner('beto')
    render(<MemoryRouter><Probe /></MemoryRouter>)
    expect(screen.getByLabelText<HTMLInputElement>('Buscar').value).toBe('')
    expect(Object.keys(sessionStorage).some((k) => k.includes('.ana.'))).toBe(false)
  })
})
describe('mensajes recuperables', () => {
  it('cancelar una consulta no se convierte en desconexión ni reintento', async () => {
    const controller = new AbortController()
    controller.abort()
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new DOMException('Cancelada', 'AbortError')))
    await expect(get('/api/transactions', controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(fetch).toHaveBeenCalledTimes(1)
  })
  it('cancelar step-up conserva una salida distinta de un fallo técnico y no repite la escritura', async () => {
    onStepUpRequired(async () => false)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code: 'step_up_required' } }), { status: 403 })))
    await expect(post('/api/test', {})).rejects.toMatchObject({ code: 'step_up_cancelled' })
    expect(fetch).toHaveBeenCalledTimes(1)
  })
  it('distingue conflicto, espera temporal, sesión vencida y fallos internos', () => {
    expect(errorMessage(new ApiError(409, 'version_conflict', 'sql detalle'))).toContain('otro dispositivo')
    expect(errorMessage(new ApiError(429, 'rate_limit', 'detalle'))).toContain('Espera')
    expect(errorMessage(new ApiError(401, 'unauthorized', 'detalle'))).toContain('sesión')
    expect(errorMessage(new ApiError(401, 'invalid_credentials', 'Email o contraseña incorrectos.'))).toContain('incorrectos')
    expect(errorMessage(new ApiError(500, 'internal', 'sql detalle'))).not.toContain('sql')
  })
})
