// Google falso para E2E: OAuth (con PKCE), Gmail (perfil, lista filtrada por remitente,
// mensajes raw) y un endpoint de control para "recibir" correos. Solo escucha en 127.0.0.1.
import { createServer } from 'node:http'
import { createHash, randomBytes } from 'node:crypto'
import { readFileSync } from 'node:fs'

export const CLIENT_ID = 'pulse-e2e.apps.googleusercontent.com'
export const CLIENT_SECRET = 'secreto-e2e'
const SCOPE = 'https://www.googleapis.com/auth/gmail.readonly'

const b64url = (b) => Buffer.from(b).toString('base64url')
const rid = () => randomBytes(9).toString('base64url')

export function startFakeGoogle(fixturesDir) {
  const boxes = new Map() // email → [{id, from, raw}]
  const codes = new Map()
  const refresh = new Map()
  const access = new Map()
  const revoked = []
  let email = 'persona@gmail.com'

  const json = (res, status, body) => {
    res.writeHead(status, { 'content-type': 'application/json' })
    res.end(JSON.stringify(body))
  }
  const readBody = (req) => new Promise((ok) => { let b = ''; req.on('data', (c) => (b += c)); req.on('end', () => ok(b)) })

  const server = createServer(async (req, res) => {
    const url = new URL(req.url, 'http://127.0.0.1')
    const p = url.pathname
    if (p === '/o/oauth2/v2/auth') {
      const q = url.searchParams
      if (q.get('client_id') !== CLIENT_ID || q.get('code_challenge_method') !== 'S256' || q.get('scope') !== SCOPE) return json(res, 400, { error: 'invalid_request' })
      const code = 'code-' + rid()
      codes.set(code, { email, challenge: q.get('code_challenge'), redirect: q.get('redirect_uri') })
      const back = new URL(q.get('redirect_uri'))
      back.searchParams.set('code', code)
      back.searchParams.set('state', q.get('state'))
      back.searchParams.set('scope', SCOPE)
      res.writeHead(302, { location: back.toString() })
      return res.end()
    }
    if (p === '/token') {
      const f = new URLSearchParams(await readBody(req))
      if (f.get('client_id') !== CLIENT_ID || f.get('client_secret') !== CLIENT_SECRET) return json(res, 401, { error: 'invalid_client' })
      if (f.get('grant_type') === 'authorization_code') {
        const g = codes.get(f.get('code'))
        codes.delete(f.get('code'))
        const sum = createHash('sha256').update(f.get('code_verifier') ?? '').digest('base64url')
        if (!g || sum !== g.challenge || f.get('redirect_uri') !== g.redirect) return json(res, 400, { error: 'invalid_grant' })
        const rt = 'rt-' + rid(), at = 'at-' + rid()
        refresh.set(rt, g.email)
        access.set(at, g.email)
        return json(res, 200, { access_token: at, refresh_token: rt, expires_in: 3599, scope: SCOPE, token_type: 'Bearer' })
      }
      const who = refresh.get(f.get('refresh_token'))
      if (!who) return json(res, 400, { error: 'invalid_grant' })
      const at = 'at-' + rid()
      access.set(at, who)
      return json(res, 200, { access_token: at, expires_in: 3599, scope: SCOPE, token_type: 'Bearer' })
    }
    if (p === '/revoke') {
      const t = new URLSearchParams(await readBody(req)).get('token')
      revoked.push(t)
      refresh.delete(t)
      return json(res, 200, {})
    }
    // Control del test (no existe en Google)
    if (p === '/__login') { email = url.searchParams.get('email'); return json(res, 200, {}) }
    if (p === '/__deliver') {
      const to = url.searchParams.get('to')
      const raw = readFileSync(new URL(url.searchParams.get('fixture') + '.eml', fixturesDir)).toString().replace(/Message-ID: <([^>]+)>/i, (_, id) => `Message-ID: <${id}.${rid()}>`)
      const from = (raw.match(/^From:.*?([\w.+-]+@[\w.-]+)/im) || [])[1]?.toLowerCase()
      const box = boxes.get(to) ?? []
      box.unshift({ id: rid(), from, raw })
      boxes.set(to, box)
      return json(res, 200, { ok: true })
    }
    if (p === '/__revoked') return json(res, 200, revoked)
    if (p.startsWith('/gmail/v1/users/me/')) {
      const who = access.get((req.headers.authorization ?? '').replace('Bearer ', ''))
      if (!who) return json(res, 401, { error: { code: 401 } })
      const box = boxes.get(who) ?? []
      const rest = p.slice('/gmail/v1/users/me/'.length)
      if (rest === 'profile') return json(res, 200, { emailAddress: who, historyId: '1000' })
      if (rest === 'messages') {
        const m = /from:\(([^)]*)\)/.exec(url.searchParams.get('q') ?? '')
        if (!m) return json(res, 400, { error: 'Pulse debe filtrar por remitente' })
        const allowed = new Set(m[1].split(' OR ').map((s) => s.trim()))
        return json(res, 200, { messages: box.filter((x) => allowed.has(x.from)).map((x) => ({ id: x.id })) })
      }
      if (rest.startsWith('messages/')) {
        const msg = box.find((x) => x.id === rest.slice('messages/'.length))
        return msg ? json(res, 200, { id: msg.id, raw: b64url(msg.raw) }) : json(res, 404, { error: { code: 404 } })
      }
      if (rest === 'stop') return json(res, 200, {})
    }
    json(res, 404, { error: 'no existe' })
  })
  return new Promise((ok) => server.listen(0, '127.0.0.1', () => ok({ server, url: `http://127.0.0.1:${server.address().port}` })))
}
