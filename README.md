# Pulse

Registro de gastos personales: cuentas y tarjetas, categorías, plan de gastos mensual con avisos y proyección. PWA para escritorio y móvil, modo claro/oscuro. Multiusuario **solo por invitación**, con seguridad zero-trust (ver `PLAN.md` §4).

Estado: **fases 0–7 implementadas** (cimientos, identidad y seguridad, datos y API, frontend PWA, plan de gastos, notificaciones, lectura de correos bancarios y Gmail en vivo). Push: `deploy/PUSH.md`. Gmail (Google Cloud, Pub/Sub, Gemini): `deploy/GMAIL.md`. Sigue la fase 8 (producción endurecida).

## Estructura
```
cmd/pulse/           binario: serve | migrate | invite | user | genkeys | healthcheck
internal/
  api/               HTTP: rutas, middleware (CSRF, cabeceras, IP real), auth, finanzas
  auth/              contraseñas (PBKDF2 + pimienta), TOTP, tokens, límites de intentos
  passkey/           verificación WebAuthn (passkeys) con la biblioteca estándar
  crypto/            cifrado por sobre: KEK → DEK por usuario → campos (AES-256-GCM)
  store/             SQL (siempre filtrado por user_id)
  db/                SQLite + migraciones embebidas
  budget/            cálculos del plan: meses en zona local, proyección, umbrales
  push/              Web Push VAPID, cola cifrada, reintentos y resúmenes
  webui/             sirve el frontend embebido
web/                 React + Vite + PWA (src/), tests unitarios (Vitest) y E2E (Playwright, e2e/)
deploy/              docker-compose, Caddy, smoke test, guía de despliegue
```

## Desarrollo
Requisitos: Go 1.27+ con un compilador C (SQLite usa CGO), Node 24.
En Windows puedes ejecutar `./scripts/dev.ps1`: compila el programa, prepara las claves locales y muestra una invitación. Si tienes Zig en `.tools/zig-x86_64-windows-0.15.2`, lo usa como compilador C; también puedes configurar `CC` con tu compilador.
```bash
make dev                      # API en :8080
cd web && npm install && npm run dev   # frontend en :5173 con proxy a la API
# crear una invitación para el entorno local:
PULSE_ENV=development PULSE_BASE_URL=http://localhost:5173 PULSE_DB_PATH=.dev/pulse.db \
PULSE_KEK_FILE=.dev/pulse_kek go run ./cmd/pulse invite create
```

## Pruebas
| Comando | Qué cubre |
|---|---|
| `make test` / `make race` | Unitarias e integración de Go (API completa sobre SQLite real, reloj falso) |
| `make fuzz` | Fuzzing de cifrado, WebAuthn y cálculos del plan |
| `make web-test` | Tipos de TypeScript + Vitest (montos, fechas) |
| `make e2e` | Playwright en escritorio y móvil: registro, TOTP, passkey (autenticador virtual), gastos, plan, PWA sin conexión, accesibilidad (axe) en ambos temas, CSP sin violaciones |
| `make check` | Todo lo anterior (lo mismo que la CI) |

## Despliegue
Ver [`deploy/README.md`](deploy/README.md).
