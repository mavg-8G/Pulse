// Servidor E2E portátil: Windows, Linux y macOS.
import { mkdirSync, existsSync, unlinkSync, writeFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'
import { CLIENT_ID, CLIENT_SECRET, startFakeGoogle } from './fake-google.mjs'
import { startFakeS3 } from './fake-s3.mjs'
import { fileURLToPath } from 'node:url'
import { resolve, join } from 'node:path'
import { execFileSync, spawn } from 'node:child_process'

const root = fileURLToPath(new URL('../../', import.meta.url))
const testRoot = resolve(root, 'web/.e2e')
const dir = resolve(process.env.PULSE_E2E_DIR || testRoot)
if (dir !== testRoot && !dir.startsWith(testRoot + (process.platform === 'win32' ? '\\' : '/'))) throw new Error('El entorno E2E debe permanecer en web/.e2e')
mkdirSync(dir, { recursive: true })
const binary = join(dir, process.platform === 'win32' ? 'pulse.exe' : 'pulse')
if (process.platform === 'win32') execFileSync('powershell.exe', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', join(root, 'scripts/stop-e2e.ps1'), '-Directory', dir])
const go = process.env.PULSE_GO || 'go'
const buildInfo = {
  built_at: new Date().toISOString().replace(/\.\d{3}Z$/, 'Z'),
  revision: execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root }).toString().trim(),
}
execFileSync(go, ['build', '-ldflags', `-X pulse/internal/buildinfo.BuiltAt=${buildInfo.built_at} -X pulse/internal/buildinfo.Revision=${buildInfo.revision}`, '-o', binary, './cmd/pulse'], { cwd: root, stdio: 'inherit' })
writeFileSync(join(dir, 'build-info.json'), JSON.stringify(buildInfo))
const secrets = join(dir, 'secrets')
if (!existsSync(join(secrets, 'pulse_kek'))) execFileSync(binary, ['genkeys', '--out', secrets])
let publicKey
if (!existsSync(join(secrets, 'vapid_private'))) {
  publicKey = execFileSync(binary, ['vapid', '--out', secrets]).toString().match(/PULSE_VAPID_PUBLIC_KEY=(\S+)/)[1]
} else {
  const { readFileSync } = await import('node:fs')
  const { createECDH } = await import('node:crypto')
  const ecdh = createECDH('prime256v1')
  ecdh.setPrivateKey(Buffer.from(readFileSync(join(secrets, 'vapid_private'), 'utf8').trim(), 'base64url'))
  publicKey = ecdh.getPublicKey().toString('base64url')
}
for (const name of ['pulse.db', 'pulse.db-shm', 'pulse.db-wal']) {
  const path = join(dir, name)
  if (existsSync(path)) unlinkSync(path)
}
const port = process.env.PORT || '8091'
// Google falso (OAuth + Gmail) para probar la conexión de Gmail sin red.
const google = await startFakeGoogle(pathToFileURL(join(root, 'internal/mailparse/testdata/')))
writeFileSync(join(secrets, 'google_client_secret'), CLIENT_SECRET)
writeFileSync(join(dir, 'fake-google.url'), google.url)
// Respaldo externo hacia un S3 falso. La clave age es solo PÚBLICA (de prueba): nadie puede leer estos respaldos.
const s3 = await startFakeS3()
writeFileSync(join(secrets, 'backup_s3_secret'), 'secreto-de-prueba')
writeFileSync(join(dir, 'fake-s3.url'), s3.url)
const child = spawn(binary, ['serve'], { stdio: 'inherit', env: {
  ...process.env, PULSE_ENV: 'development', PULSE_BASE_URL: `http://localhost:${port}`, PULSE_LISTEN: `:${port}`,
  PULSE_DB_PATH: join(dir, 'pulse.db'), PULSE_KEK_FILE: join(secrets, 'pulse_kek'), TZ: 'America/Guayaquil',
  PULSE_REGISTER_PER_HOUR: '1000', PULSE_VAPID_PUBLIC_KEY: publicKey,
  PULSE_VAPID_PRIVATE_FILE: join(secrets, 'vapid_private'), PULSE_VAPID_SUBJECT: 'mailto:test@example.com',
  PULSE_GOOGLE_FAKE_URL: google.url, PULSE_GOOGLE_CLIENT_ID: CLIENT_ID, PULSE_GOOGLE_CLIENT_SECRET_FILE: join(secrets, 'google_client_secret'),
  PULSE_GMAIL_POLL_INTERVAL: '1m',
  PULSE_BACKUP_TARGET: 's3', PULSE_BACKUP_INTERVAL: 'manual', PULSE_BACKUP_AGE_RECIPIENTS: 'age1qqxq2avup3m4mtj05lqenylg9gn2nanfymkx76zyzh2e8fvw2slqx2av3c',
  PULSE_BACKUP_S3_ENDPOINT: s3.url, PULSE_BACKUP_S3_BUCKET: 'pulse-e2e', PULSE_BACKUP_S3_ACCESS_KEY_ID: 'e2e',
  PULSE_BACKUP_S3_SECRET_FILE: join(secrets, 'backup_s3_secret'), PULSE_BACKUP_DIR: join(dir, 'backups-externos'),
} })
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => child.kill(signal))
child.on('exit', (code) => { google.server.close(); s3.server.close(); process.exit(code ?? 0) })
