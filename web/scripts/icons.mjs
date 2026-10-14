// Genera los íconos PNG de la PWA a partir del SVG (node scripts/icons.mjs).
import { chromium } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const svg = readFileSync(new URL('../public/icon.svg', import.meta.url), 'utf8')
const background = svg.match(/<rect[^>]*fill="([^"]+)"/)[1]
const maskable = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" fill="${background}"/>
<g transform="translate(9.6 9.6) scale(0.7)"><path d="M8 36h12l5-14 8 26 6-18 4 6h13" fill="none" stroke="#fff" stroke-width="4.5" stroke-linecap="round" stroke-linejoin="round"/></g></svg>`
const targets = [
  ['icon-192.png', 192, svg],
  ['icon-512.png', 512, svg],
  ['maskable-512.png', 512, maskable],
  ['apple-touch-icon.png', 180, maskable],
]
const browser = await chromium.launch()
const page = await browser.newPage()
for (const [name, size, src] of targets) {
  await page.setViewportSize({ width: size, height: size })
  await page.setContent(`<style>html,body{margin:0;background:transparent}svg{width:${size}px;height:${size}px;display:block}</style>${src}`)
  await page.screenshot({ path: fileURLToPath(new URL(`../public/icons/${name}`, import.meta.url)), omitBackground: true })
}
await browser.close()
console.log('íconos generados')
