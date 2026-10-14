import { defineConfig, devices } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { join } from 'node:path'

// Cada ejecución tiene puerto y datos propios; nunca se conecta a otro Pulse abierto.
const PORT = Number(process.env.PULSE_E2E_PORT || execFileSync(process.execPath, ['-e', "const s=require('node:net').createServer();s.listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"]).toString().trim())
process.env.PULSE_E2E_PORT = String(PORT)
process.env.PULSE_E2E_DIR ||= join(fileURLToPath(new URL('./.e2e/', import.meta.url)), `run-${process.pid}-${PORT}`)
export default defineConfig({
  testDir: './e2e',
  globalTeardown: './e2e/teardown.mjs',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: [['list']],
  use: {
    baseURL: `http://localhost:${PORT}`,
    locale: 'es-EC',
    timezoneId: 'America/Guayaquil',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'escritorio', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } } },
    { name: 'movil', use: { ...devices['Pixel 7'] } },
  ],
  webServer: {
    command: 'node e2e/server.mjs',
    url: `http://localhost:${PORT}/healthz`,
    reuseExistingServer: false,
    timeout: 180_000,
    env: { PORT: String(PORT), PULSE_E2E_DIR: process.env.PULSE_E2E_DIR },
  },
})
