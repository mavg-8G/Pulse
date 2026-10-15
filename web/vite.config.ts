import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'
import { writeFileSync } from 'node:fs'

export default defineConfig({
  plugins: [
    react(),
    {
      // Vite vacía dist/ al compilar; dejamos .gitkeep para que `go build` funcione aunque no se haya compilado el frontend.
      name: 'keep-dist-placeholder',
      closeBundle() {
        writeFileSync('../internal/webui/dist/.gitkeep', '')
      },
    },
    VitePWA({
      registerType: 'prompt',
      injectRegister: false, // se registra desde main.tsx (sin scripts en línea: la CSP no los permite)
      manifest: false, // usamos public/manifest.webmanifest
      workbox: {
        importScripts: ['/push-sw.js'],
        globPatterns: ['**/*.{js,css,html,svg,png,woff2,webmanifest}'],
        navigateFallback: '/index.html',
        navigateFallbackDenylist: [/^\/api\//, /^\/healthz/, /^\/(privacidad|terminos|privacy|terms|about|acerca)$/],
        // La API NUNCA se guarda en caché: son datos bancarios.
        runtimeCaching: [{ urlPattern: /\/api\//, handler: 'NetworkOnly' }],
        cleanupOutdatedCaches: true,
      },
    }),
  ],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: { '/api': 'http://localhost:8080', '/healthz': 'http://localhost:8080' },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
