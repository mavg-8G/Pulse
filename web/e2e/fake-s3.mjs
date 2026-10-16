// S3 falso para las pruebas E2E del respaldo externo: acepta PUT firmados (AWS4-HMAC-SHA256) y guarda en memoria.
import { createServer } from 'node:http'

export async function startFakeS3() {
  const objects = new Map()
  const server = createServer((req, res) => {
    if (req.method === 'GET' && req.url === '/__objects') {
      res.setHeader('Content-Type', 'application/json')
      return res.end(JSON.stringify([...objects].map(([k, v]) => ({ key: k, size: v.length, plaintext: v.includes(Buffer.from('SQLite format 3')) }))))
    }
    if (req.method !== 'PUT' || !String(req.headers.authorization || '').startsWith('AWS4-HMAC-SHA256 ')) {
      res.statusCode = 403
      return res.end()
    }
    const chunks = []
    req.on('data', (c) => chunks.push(c))
    req.on('end', () => {
      objects.set(req.url, Buffer.concat(chunks))
      res.end()
    })
  })
  await new Promise((r) => server.listen(0, '127.0.0.1', r))
  return { server, url: `http://127.0.0.1:${server.address().port}` }
}
