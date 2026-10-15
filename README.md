# Pulse

Registro de gastos personales: cuentas y tarjetas, categorías, plan de gastos mensual con avisos y proyección. PWA para escritorio y móvil, modo claro/oscuro. Multiusuario **solo por invitación**, con MFA y aislamiento de datos por usuario (ver [`PLAN.md`](PLAN.md) §4).

Estado: **fases 0–7 implementadas**. Incluye respaldos, CI/CD y páginas públicas de privacidad y términos; el seguimiento del endurecimiento de producción está en [`PLAN.md`](PLAN.md). Los cambios se registran en [`CHANGELOG.md`](CHANGELOG.md).

## Funciones

- Cuentas, tarjetas, categorías y movimientos manuales o importados, con revisión de gastos y reglas por comercio.
- Plan mensual con proyección y umbrales de gasto; notificaciones Web Push, resúmenes y horario silencioso. Configuración en [`deploy/PUSH.md`](deploy/PUSH.md).
- Importación de correos `.eml` de Banco Pichincha, Produbanco y Diners/Titanium. Gmail opcional mediante OAuth, sincronización periódica o Pub/Sub y modo sombra; Gemini opcional para plantillas desconocidas. Configuración en [`deploy/GMAIL.md`](deploy/GMAIL.md).
- Acceso por invitación, TOTP o passkeys, gestión de sesiones y cifrado de campos sensibles con claves por usuario.
- Presentación pública y páginas de privacidad y términos en español e inglés, accesibles sin sesión; exportación y supresión de cuentas mediante CLI, y retención del registro de seguridad de 365 días.

## Estructura
```
cmd/pulse/           CLI: servidor, migraciones, invitaciones, usuarios, claves, respaldos y parser
internal/
  api/               HTTP: rutas, middleware (CSRF, cabeceras, IP real), auth, finanzas
  auth/              contraseñas (PBKDF2 + pimienta), TOTP, tokens, límites de intentos
  passkey/           verificación WebAuthn (passkeys) con la biblioteca estándar
  crypto/            cifrado por sobre: KEK → DEK por usuario → campos (AES-256-GCM)
  store/             SQL (siempre filtrado por user_id)
  db/                SQLite + migraciones embebidas
  budget/            cálculos del plan: meses en zona local, proyección, umbrales
  push/              Web Push VAPID, cola cifrada, reintentos y resúmenes
  mailparse/         lectores y normalización de correos bancarios
  importer/          importación de gastos y reglas por comercio
  gmail/             OAuth, sincronización, Pub/Sub y Gemini opcional
  admin/             administración de invitaciones y usuarios
  webui/             sirve el frontend embebido
web/                 React + Vite + PWA (src/), tests unitarios (Vitest) y E2E (Playwright, e2e/)
deploy/              docker-compose, Caddy, smoke test, guía de despliegue
```

## Desarrollo

Requisitos: Go 1.27.2+ con un compilador C (SQLite usa CGO), Node 24.22.0 LTS (ver `.node-version`). CI y Docker toman las versiones de `go.mod` y `.node-version`.

El frontend usa React 19, React Router 7, TypeScript 7, Vite 8 y Vitest 5; las versiones resueltas se fijan en `web/package-lock.json`. Las dependencias Go y sus sumas están en `go.mod` y `go.sum`.

En Windows puedes ejecutar `./scripts/dev.ps1`: compila el programa, prepara las claves locales y muestra una invitación. Si tienes Zig en `.tools/zig-x86_64-windows-0.15.2`, lo usa como compilador C; también puedes configurar `CC` con tu compilador. Los datos locales se guardan en `.dev/`.

En Linux/macOS, instala primero las dependencias del frontend y luego inicia ambos servidores en terminales separadas:

```bash
cd web && npm ci && cd ..
make dev                      # API en :8080
cd web && npm run dev          # frontend en :5173 con proxy a la API
# crear una invitación para el entorno local:
PULSE_ENV=development PULSE_BASE_URL=http://localhost:5173 PULSE_DB_PATH=.dev/pulse.db \
PULSE_KEK_FILE=.dev/pulse_kek go run ./cmd/pulse invite create
```

Para compilar el frontend embebido y el binario de distribución: `make build`. La PWA y su service worker se comprueban contra esa compilación, no con el servidor de desarrollo de Vite.

## Primera cuenta y código de invitación

El código lo genera quien administra el servidor y se muestra en la terminal al crear la invitación. Para crear tu primera cuenta con rol de administrador, ejecuta en la consola del VPS, con Pulse ya desplegado:

```bash
sudo docker exec pulse /pulse invite create --role admin --email tu@correo.com --ttl 2h
```

Sustituye `tu@correo.com` por tu correo. Copia el código que empieza por `PLS-`, abre [Crear cuenta](https://pulse.espectro.uk/registro) y pégalo en **Código de invitación**. Usa el mismo correo y configura una passkey o una app de autenticación.

El código se muestra **una sola vez**, permite un único registro y, en este ejemplo, vence en dos horas. Si lo perdiste o caducó, genera otra invitación. `pulse invite list` muestra el estado de las invitaciones, pero no recupera los códigos.

Para invitar a otra persona con el rol normal de usuario:

```bash
sudo docker exec pulse /pulse invite create --email persona@example.com
```

Sin `--ttl`, la vigencia es de 72 horas. En desarrollo local con Windows, `./scripts/dev.ps1` muestra una invitación en la terminal al arrancar; esa invitación corresponde a la base local y no sirve para el VPS.

## Páginas públicas

| Contenido | Español | Inglés |
|---|---|---|
| Presentación de Pulse y uso opcional de Gmail | `/acerca` | `/about` |
| Política de privacidad | `/privacidad` | `/privacy` |
| Términos y condiciones | `/terminos` | `/terms` |

Estas páginas sirven contenido HTML legible sin ejecutar la aplicación y permiten cambiar de idioma. La pantalla de acceso enlaza a **Qué es Pulse**, **Privacidad** y **Términos**. El service worker conserva sus rutas públicas y no las sustituye por la aplicación.

## Administración y privacidad

Ejecuta la CLI con las mismas variables de configuración y claves que el servidor. La [guía de despliegue](deploy/README.md) incluye los comandos equivalentes en Docker y el procedimiento de restauración.

| Comando | Uso |
|---|---|
| `pulse invite create --email persona@example.com` | Crear una invitación |
| `pulse user list` | Listar usuarios |
| `pulse user export persona@example.com export.json` | Exportar los datos de una persona a un archivo nuevo |
| `pulse user delete persona@example.com --confirmar persona@example.com` | Eliminar la cuenta y sus datos asociados |
| `pulse backup --keep 15 respaldo.db` | Crear un respaldo en caliente y rotar los anteriores |
| `pulse restore respaldo.db` | Restaurar un respaldo con el servidor detenido |
| `pulse parse correo.eml` | Probar el parser sin guardar movimientos |

La exportación contiene datos personales descifrados: entrégala por un canal seguro y elimina el archivo después. La supresión conserva el registro de seguridad hasta que vence su retención; los respaldos anteriores se eliminan al rotar. Si existía una conexión con Gmail, el usuario también puede retirar el acceso desde su cuenta de Google. Los textos públicos están en `web/public/`, con una página HTML por ruta y estilos compartidos en `legal.css`.

## Pruebas

| Comando | Qué cubre |
|---|---|
| `make test` / `make race` | Unitarias e integración de Go (API completa sobre SQLite real, reloj falso) |
| `make fuzz` | Fuzzing de cifrado, WebAuthn y cálculos del plan |
| `make web-test` | Tipos de TypeScript + Vitest (montos, fechas) |
| `make e2e` | Playwright en escritorio y móvil: registro, TOTP, passkey (autenticador virtual), gastos, plan, PWA sin conexión, accesibilidad (axe) en ambos temas, CSP sin violaciones |
| `make check` | Todo lo anterior (lo mismo que la CI) |

La suite E2E incluye páginas legales públicas, navegación con el service worker activo y accesibilidad en ambos temas. Cada ejecución utiliza puerto y datos propios dentro de `web/.e2e/`.

Para instalar Chromium antes de las pruebas E2E: `cd web && npx playwright install chromium` (en Linux CI: `--with-deps chromium`). Para revisar dependencias: `npm audit` en `web/` y `go mod verify` en la raíz. La CI también comprueba vulnerabilidades Go alcanzables con `govulncheck` y escanea la imagen con Trivy.

## Despliegue

Ver [`deploy/README.md`](deploy/README.md). Un push a `main` ejecuta las pruebas, construye y firma la imagen en GHCR y activa el despliegue del entorno `production`, sujeto a sus controles de GitHub. El servidor escucha por defecto en el puerto 8080.
