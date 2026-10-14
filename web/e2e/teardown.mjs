import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

export default async function teardown() {
  if (process.platform === 'win32') {
    execFileSync('powershell.exe', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', fileURLToPath(new URL('../../scripts/stop-e2e.ps1', import.meta.url)), '-Directory', process.env.PULSE_E2E_DIR || fileURLToPath(new URL('../.e2e/', import.meta.url))], { stdio: 'inherit' })
  }
}
