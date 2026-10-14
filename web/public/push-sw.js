/* Se importa desde el service worker de Workbox. No guarda datos financieros. */
self.addEventListener('push', (event) => {
  event.waitUntil((async () => {
    if (!event.data) return
    let payload
    try { payload = event.data.json() } catch { return }
    if (typeof payload.user_id !== 'string' || typeof payload.body !== 'string') return
    // Una entrega en vuelo después del logout nunca muestra datos de la cuenta anterior.
    const response = await fetch('/api/me', { credentials: 'same-origin', cache: 'no-store' }).catch(() => null)
    if (!response?.ok) return
    const me = await response.json()
    if (me.id !== payload.user_id) return
    const safeURL = (value) => {
      try {
        const u = new URL(value, self.location.origin)
        if (u.origin === self.location.origin && ['/', '/plan', '/movimientos', '/ajustes'].includes(u.pathname)) return u.pathname + u.search
      } catch {}
      return '/'
    }
    await self.registration.showNotification(typeof payload.title === 'string' ? payload.title.slice(0, 80) : 'Pulse', {
      body: payload.body.slice(0, 300),
      tag: String(payload.tag || 'pulse').slice(0, 180),
      icon: '/icons/icon-192.png', badge: '/icons/icon-192.png',
      data: { url: safeURL(payload.url), user_id: payload.user_id },
    })
  })())
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  event.waitUntil((async () => {
    const raw = event.notification.data?.url || '/'
    const target = new URL(raw, self.location.origin)
    if (target.origin !== self.location.origin) return
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    const current = windows.find((client) => new URL(client.url).origin === self.location.origin)
    if (current) {
      await current.navigate(target.href)
      await current.focus()
    } else { await self.clients.openWindow(target.href) }
  })())
})
