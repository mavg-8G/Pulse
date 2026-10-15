# Pulse — Plan de desarrollo

Registro de gastos personales multiusuario: categorías, cuentas (origen), fechas y plan de gastos (tope mensual por categoría). PWA instalable en escritorio y móvil, modo claro/oscuro, notificaciones push, ingreso manual y captura automática desde Gmail.

**Estado (14 oct 2026): fases 0–7 implementadas.** Faltan dos aceptaciones con datos reales: la entrega push en un teléfono físico (`deploy/PUSH.md`) y un consumo real llegando por Gmail con el proyecto de Google Cloud configurado (`deploy/GMAIL.md`). Los parsers se validaron con correos sintéticos reconstruidos de las capturas: hay que confirmarlos con `.eml` reales. Ver §0 para los cambios respecto al plan original.

Principios rectores:
1. **Backend mínimo** — un binario Go que sirve API + frontend, SQLite en un archivo, un contenedor detrás de Caddy en `pulse.espectro.uk`.
2. **Zero-trust** — la app maneja datos bancarios: nada se confía por estar "dentro" (ni la red, ni Caddy, ni el dispositivo, ni una sesión antigua). Cada petición se autentica, se autoriza por usuario y se registra.
3. **Registro cerrado** — solo entra quien tiene una clave de invitación.

---

## 0. Cambios respecto al plan original

| Plan | Implementado | Motivo |
|---|---|---|
| SQLite con `modernc.org/sqlite` | `mattn/go-sqlite3` (CGO, enlazado estático con musl en Docker) | Driver más probado; la imagen sigue siendo un binario estático sobre distroless |
| sqlc + goose | SQL escrito a mano en `internal/store` + migrador propio embebido | Menos dependencias y herramientas; cada consulta lleva `user_id` y hay tests de aislamiento por endpoint |
| argon2id | PBKDF2-HMAC-SHA256 (600 000 iteraciones, recomendación OWASP) + pimienta HMAC derivada de la KEK | Biblioteca estándar de Go; un volcado de la BD sin la KEK no permite atacar contraseñas. El formato lleva algoritmo e iteraciones: se puede migrar a argon2id rehaciendo el hash en el siguiente login |
| `go-webauthn/webauthn` y `pquerna/otp` | Verificación WebAuthn y TOTP propias con la biblioteca estándar | Sin dependencias externas; cubiertas con vectores del RFC 6238, un autenticador por software y fuzzing |
| Integridad multiusuario solo en código | Además, claves foráneas compuestas `(id, user_id)` en SQLite | Aunque el código fallara, la BD rechaza mezclar datos de usuarios |
| Backups cifrados | Pendiente (Fase 8) | Ver `deploy/README.md` para el respaldo manual mientras tanto |
| Polling con `history.list` | `messages.list` con la consulta de remitentes bancarios (`from:(…) newer_than:Nd`); `historyId` solo para descartar avisos de Pub/Sub viejos | `history.list` devuelve ids de **todos** los correos nuevos; con la consulta filtrada en Google, Pulse nunca ve ni el id de un correo que no sea del banco. Sin historial que caduque, no hay "resincronización completa" que probar |
| Callback OAuth `GET /api/gmail/callback` | Google vuelve a la página `/ajustes/gmail`, que envía código y `state` con `fetch` a `POST /api/gmail/callback` | La cookie de sesión es `SameSite=Strict` y no viaja en la redirección desde Google; además el código no queda en logs de acceso |
| `state` y verificador PKCE guardados | `state` firmado con HMAC (usuario + sesión + caducidad 10 min); verificador = HMAC(nonce) con clave del servidor | Nada que guardar ni limpiar; el verificador nunca viaja en la URL |
| Modo sombra (sin detalle) | Activado por defecto: todo lo importado queda "Por revisar". Apagado: solo se confirma solo lo que el parser leyó con seguridad **y** tiene una regla de comercio del usuario | Evita que un gasto mal clasificado cuente en el presupuesto sin que lo veas |
| Gemini como fallback | Solo para remitentes bancarios con plantilla desconocida, activación por usuario con step-up, texto redactado (también el nombre en el asunto), resultado validado y siempre por revisar | Mínimo dato posible a un tercero |
| Push "nuevo gasto" | Aviso "Nuevo gasto por revisar" (o "Gasto registrado") para consumos de las últimas 24 h, con la preferencia de alertas de presupuesto y "ocultar montos" | La primera sincronización (7 días) no inunda de avisos |

---

## 1. Decisiones de arquitectura

| Capa | Elección | Por qué |
|---|---|---|
| Backend | **Go 1.23+, `net/http` de la stdlib** | Sin framework; binario estático pequeño |
| Base de datos | **SQLite** (WAL) con `modernc.org/sqlite` (sin CGO) | Sin contenedores extra; un archivo `/data/pulse.db` |
| Acceso a datos | `database/sql` + **sqlc** | SQL explícito y tipado; toda query recibe `user_id` |
| Migraciones | `goose`, embebidas, corren al arrancar | |
| Jobs | Goroutines con `time.Ticker` | Escaneo Gmail, renovación `watch`, alertas, backups |
| Auth | Contraseña `argon2id` + **MFA obligatorio** (passkey/WebAuthn o TOTP) | `go-webauthn/webauthn`, `pquerna/otp` |
| Cifrado | AES-256-GCM (stdlib) con cifrado por sobre (clave maestra → clave por usuario) | Campos sensibles ilegibles aunque se filtre el `.db` |
| Push | Web Push VAPID (`SherClockHolmes/webpush-go`) | Payload cifrado extremo a extremo (RFC 8291) |
| Gmail | `google.golang.org/api/gmail/v1` + `x/oauth2` | Cada usuario conecta su propio Gmail |
| HTML de correos | `golang.org/x/net/html` → texto plano → regex | Parsers robustos a cambios de estilo |
| Parser IA | Gemini — **desactivado por defecto**, opt-in por usuario | Enviar correos bancarios a un tercero contradice zero-trust |
| Backups | `VACUUM INTO` + cifrado con `filippo.io/age` | Ningún backup sale del servidor en claro |
| Frontend | React + Vite + TS, TanStack Query, Tailwind, `vite-plugin-pwa` | Embebido en el binario con `embed.FS` |
| Deploy | 1 contenedor endurecido en `/opt/docker/pulse`, red `proxy`, Caddy → `pulse.espectro.uk` | |

> Moneda base USD; `currency` por cuenta para el futuro.

---

## 2. Estructura del repositorio

```
Pulse/
├── cmd/pulse/main.go          # subcomandos: serve | invite | user | genkeys | healthcheck
├── internal/
│   ├── api/                   # handlers + middleware (auth, MFA, CSRF, rate limit, audit)
│   ├── auth/                  # argon2id, sesiones, WebAuthn, TOTP, invitaciones, step-up
│   ├── crypto/                # cifrado por sobre (KEK → DEK por usuario), helpers AES-GCM
│   ├── store/                 # sqlc + queries/*.sql
│   ├── migrations/
│   ├── gmail/                 # OAuth, history.list, watch, webhook Pub/Sub
│   ├── parser/                # parsers por banco + Gemini opt-in
│   │   └── testdata/          # correos .eml ANONIMIZADOS
│   ├── budget/
│   ├── push/
│   ├── audit/                 # bitácora de eventos de seguridad
│   └── jobs/
├── web/                       # React + Vite (PWA) → dist embebido
├── deploy/                    # Dockerfile, docker-compose.yml, Caddyfile.snippet
├── sqlc.yaml
├── Makefile
└── .env.example
```

---

## 3. Usuarios, registro e invitaciones

### 3.1 Flujo de registro
1. El admin genera una invitación **desde el servidor** (no hay endpoint web de admin expuesto):
   ```
   docker exec pulse /pulse invite create --email persona@gmail.com --ttl 72h
   → Código: PLS-7K3Q-M9XW-T2HV-8RJD   (válido 72 h, un solo uso)
   ```
2. La persona entra a `https://pulse.espectro.uk/registro`, pone email + contraseña + código.
3. Antes de ver cualquier dato **debe registrar un segundo factor** (passkey recomendada, TOTP como alternativa) y guardar sus 10 códigos de recuperación.
4. Recién entonces se crea la sesión completa.

### 3.2 Reglas de las invitaciones
- 100 bits aleatorios (`crypto/rand`), mostrados una sola vez; en BD solo se guarda su **SHA-256**.
- Un solo uso, caducan (por defecto 72 h), opcionalmente atadas a un email.
- Comparación en tiempo constante; el endpoint de registro tiene rate limit estricto (5 intentos/hora por IP).
- Respuestas genéricas: no revelan si el código existe, caducó o si el email ya está registrado.
- Subcomandos: `invite create | list | revoke`, `user list | disable | reset-mfa`.

### 3.3 Roles
- `admin` (tú) y `user`. El admin **no puede ver los gastos** de otros usuarios desde la app: solo gestiona invitaciones y cuentas vía CLI. Los datos de cada usuario están cifrados con su propia clave.

---

## 4. Seguridad zero-trust

### 4.1 Identidad y sesiones
- **Contraseña**: `argon2id` (m=64 MiB, t=3, p=2), mínimo 12 caracteres, comparada contra lista embebida de contraseñas comunes.
- **MFA obligatorio** para todos. Passkey (WebAuthn, resistente a phishing) como método principal; TOTP como respaldo; códigos de recuperación hasheados.
- **Sesiones**: token aleatorio de 256 bits en cookie `__Host-pulse` (`Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`); en BD solo su hash.
  - Expiración por inactividad: 7 días en dispositivo PWA registrado, 12 h en navegador común; absoluta: 30 días.
  - Rotación del token al iniciar sesión y al elevar privilegios.
  - Lista de sesiones activas con dispositivo/IP/última actividad y botón "cerrar".
- **Step-up (re-autenticación con passkey/TOTP en los últimos 5 min)** para: conectar/desconectar Gmail, exportar CSV, cambiar contraseña o MFA, borrar cuenta, activar Gemini.
- **Bloqueo progresivo**: tras 5 fallos, espera exponencial por cuenta y por IP; aviso al usuario.
- **Aviso de nuevo inicio de sesión** por push (y correo si se añade SMTP más adelante).

### 4.2 Autorización y aislamiento entre usuarios
- Toda query de sqlc lleva `WHERE user_id = ?`; no existen queries "por id" sin `user_id`.
- Los IDs públicos son UUIDv7/aleatorios, no autoincrementales.
- Recurso de otro usuario → **404** (no 403), para no confirmar que existe.
- Tests de aislamiento: un test por endpoint que intenta leer/editar datos de otro usuario y debe fallar.

### 4.3 Cifrado de datos en reposo
- **Clave maestra (KEK)** de 32 bytes en un Docker secret (`/run/secrets/pulse_kek`), nunca en la BD ni en los backups.
- **Clave por usuario (DEK)** aleatoria, guardada cifrada con la KEK.
- Campos cifrados con la DEK (AES-256-GCM, nonce aleatorio, `user_id` como dato asociado):
  - refresh token de Gmail, secreto TOTP
  - comercio (`merchant`), descripción, notas
- En claro (necesarios para sumar y filtrar en SQL): monto, fecha, categoría, cuenta, estado. Sin el comercio, estos datos dicen poco.
- La búsqueda por comercio se hace en memoria tras descifrar (volúmenes personales: miles de filas, no millones).
- **No se guarda el correo**: se procesa en memoria y se descarta. Solo queda lo extraído + `SHA-256(gmail_message_id)` para deduplicar.
- Borrar la DEK de un usuario = borrado criptográfico de sus datos sensibles.

### 4.4 Red y perímetro (no confiar en la red)
- Caddy solo termina TLS; **la app no confía en ninguna cabecera** salvo `X-Forwarded-For` cuando la conexión viene de la IP de Caddy en la red `proxy`.
- Contenedor sin puertos publicados; solo alcanzable por Caddy.
- CSRF: `SameSite=Strict` + verificación de `Origin`/`Sec-Fetch-Site` en toda petición que no sea GET.
- Cabeceras desde el binario: CSP estricta (`default-src 'self'`; sin `unsafe-inline`), `frame-ancestors 'none'`, HSTS con preload, `Permissions-Policy`, `Cross-Origin-Opener-Policy: same-origin`.
- Webhook de Pub/Sub: se valida el JWT OIDC de Google (firma, `aud` = URL del webhook, `email` = cuenta de servicio esperada) antes de leer el cuerpo.
- `/api/ingest/email` (atajo n8n): token por usuario (hash en BD) + firma HMAC del cuerpo con marca de tiempo (rechaza > 5 min); se puede desactivar por completo con `INGEST_ENABLED=false`.

### 4.5 Gmail con mínimo privilegio
- Scope `gmail.readonly` (no existe uno más estrecho que permita leer contenido).
- Solo se consultan mensajes que cumplan la query de remitentes bancarios (§5.2); el resto del buzón nunca se descarga.
- El usuario ve en Ajustes qué remitentes se leen y la fecha del último acceso; puede desconectar y Pulse revoca el token en Google.

### 4.6 Terceros
- **Gemini desactivado por defecto**. Si un usuario lo activa (con step-up), antes de enviar se eliminan nombre del titular, números de tarjeta y cualquier secuencia de ≥ 6 dígitos.
- Notificaciones push: opción "ocultar montos en pantalla de bloqueo" (el texto dice "Nuevo gasto registrado").

### 4.7 Auditoría
- Tabla `audit_log` (solo inserción): login OK/fallido, MFA, cambios de contraseña/MFA, sesiones revocadas, conexión de Gmail, exportaciones, invitaciones creadas/usadas, activación de Gemini.
- El usuario ve su propia actividad de seguridad en Ajustes.
- Logs de aplicación (`slog` JSON) sin montos, comercios, tokens ni cuerpos de correo.

### 4.8 Contenedor y cadena de suministro
- Imagen `distroless/static:nonroot`, sistema de archivos `read_only`, `cap_drop: ALL`, `no-new-privileges`, `tmpfs` para `/tmp`, límites de memoria/CPU.
- Secretos como Docker secrets (archivos), no variables de entorno.
- CI: `go vet`, `staticcheck`, `govulncheck`, `gosec`, `npm audit`, Trivy sobre la imagen; dependencias fijadas (`go.sum`, `package-lock.json`).

---

## 5. Captura automática desde Gmail

### 5.1 Flujo

```
Gmail (de cada usuario) ──(push o polling)──► gmail.Sync(user)
   ──► query por remitentes bancarios ──► HTML → texto plano (en memoria)
   ──► parser del banco ──(falla y Gemini activado)──► Gemini (datos redactados)
   ──► normalizar monto / fecha / comercio / sufijo de tarjeta
   ──► cuenta por (institución + sufijo), categoría por merchant_rules
   ──► INSERT transaction (comercio cifrado) ──► descartar el correo
   ──► push + budget.Check()
```

### 5.2 Parsers iniciales (de los correos de ejemplo)

| | Banco Pichincha | Produbanco | Diners Club (Titanium Visa) |
|---|---|---|---|
| Remitente | `servicios@tarjetasbancopichincha.com` | `bancaenlinea@produbanco.com` | `servicios@titanium.com.ec` |
| Asunto | (por confirmar) | `Consumo Tarjeta de Crédito por USD 4.34` | `Notificación de Consumos` |
| Detección | texto "Has realizado un consumo" | asunto empieza con "Consumo Tarjeta de Crédito" | texto "Realizaste una compra con tu" |
| Monto | `Valor  $ 17,68` → coma decimal | `Valor: USD 4.34` → punto decimal | `Valor  8,12` → coma decimal, sin símbolo |
| Comercio | `Establecimiento  CARL S ALBAN BORJA` | `Establecimiento: PEDIDOSYA GROCERIES` | `Establecimiento  DLC* UBER EATS` |
| Tarjeta | `Tarjeta usada  989` (**3 dígitos**) | `Visa Produbanco XXX7828` | `Tarjeta terminada en 666` (**3 dígitos**) |
| Fecha | `2026-10-12 18:24` (ISO) | `8/Octubre/2026 16:25` (**mes en español**) | `2026-06-20 14:31` (ISO, label arriba del valor) |
| Tipo | `TARJETA DE CRÉDITO` | `Tarjeta de Crédito` | `TITANIUM Visa` (crédito) |

Implicaciones para el diseño:
- **Sufijo de tarjeta variable** (3 o 4 dígitos): el campo es `card_suffix` y se compara por "termina en". La cuenta se identifica por `institution + card_suffix`.
- **Normalizador de montos** que acepta `$`, `USD`, coma o punto decimal y separadores de miles (`1.234,56` y `1,234.56`), según una regla por banco — nunca adivinando. Resultado en centavos enteros.
- **Fechas** en hora local `America/Guayaquil`; diccionario de meses en español (enero…diciembre, con y sin tilde/mayúsculas).
- **Limpieza de comercio** para las reglas de categoría: quitar prefijos de pasarela (`DLC*`, `DL*`, `PAYPAL *`, `SQ *`), espacios dobles, mayúsculas → "Uber Eats".
- Las etiquetas y los valores pueden venir en celdas de tabla separadas: el HTML se convierte a texto con un salto de línea por celda y la regex busca la etiqueta y el **siguiente** valor no vacío.
- **Descartar** del mismo remitente: pagos a la tarjeta, avances/diferidos (marcar para revisión), reversos (registrar como monto negativo vinculado), avisos de seguridad o publicidad.

Interfaz común:
```go
type Parser interface {
    Name() string
    Match(from, subject, text string) bool
    Parse(msg Email) (Expense, error)   // Expense{AmountCents, Currency, Merchant, CardSuffix, OccurredAt, Kind}
}
```
Cada parser con tests de tabla sobre `.eml` reales **anonimizados** (nombre del titular reemplazado, sufijos de tarjeta cambiados).

### 5.3 Modos de lectura
1. **Tiempo real**: `users.watch` por usuario → Pub/Sub → `POST /api/gmail/webhook` (JWT verificado) → `history.list` desde el último `history_id`. Renovación diaria del `watch`.
2. **Polling de respaldo**: ticker cada 15 min (`GMAIL_POLL_INTERVAL`), siempre activo.
3. **Atajo n8n** (opcional, solo para probar): Gmail Trigger → `POST /api/ingest/email` firmado con HMAC.

Query de Gmail (configurable por usuario):
```
from:(servicios@tarjetasbancopichincha.com OR bancaenlinea@produbanco.com OR servicios@titanium.com.ec) newer_than:2d
```

### 5.4 OAuth
- Proyecto en Google Cloud; app en estado **In production** sin verificar (en *Testing* el refresh token caduca a los 7 días). Aviso "Google no ha verificado esta app" una vez por usuario; límite de 100 usuarios, suficiente con registro por invitación.
- Parámetro `state` firmado + PKCE en el flujo OAuth.

---

## 6. Modelo de datos (SQLite)

```
users              id, email, password_hash, role(admin|user), dek_enc, timezone, theme,
                   gemini_enabled, disabled_at, created_at
invites            id, code_hash, email?, created_by, expires_at, used_at, used_by
sessions           token_hash, user_id, device_label, ip, user_agent, mfa_at,
                   created_at, last_seen_at, expires_at
webauthn_credentials id, user_id, credential_id, public_key, sign_count, name, created_at
totp               user_id, secret_enc, confirmed_at
recovery_codes     user_id, code_hash, used_at
audit_log          id, user_id?, event, ip, user_agent, meta_json, created_at
accounts           id, user_id, name, type(bank|credit_card|debit_card|cash|wallet),
                   institution, card_suffix, currency, color, icon, archived
categories         id, user_id, name, parent_id, color, icon, archived
transactions       id, user_id, account_id, category_id, amount_cents, currency,
                   occurred_at, merchant_enc, description_enc, notes_enc,
                   source(manual|gmail), kind(purchase|refund|cash_advance),
                   status(confirmed|pending_review), message_hash UNIQUE(user_id, message_hash),
                   parser, confidence, created_at, updated_at
budgets            id, user_id, category_id, amount_cents, alert_thresholds, starts_on, active
budget_alerts      budget_id, month, threshold
merchant_rules     id, user_id, pattern_enc, match, category_id, account_id
gmail_connections  user_id, email, refresh_token_enc, history_id, query,
                   watch_expires_at, last_sync_at, status
ingest_tokens      user_id, token_hash, created_at, revoked_at
push_subscriptions id, user_id, endpoint, p256dh, auth, device_label, hide_amounts
notifications      id, user_id, type, payload_enc, sent_at, read_at
```

Montos en centavos enteros. `PRAGMA foreign_keys=ON; journal_mode=WAL; secure_delete=ON`.

---

## 7. Plan de gastos y notificaciones

**Presupuesto**: tope mensual por categoría (subcategorías suman a la padre), progreso, restante y proyección a fin de mes.

| Notificación | Cuándo |
|---|---|
| Gasto detectado | Al importar un correo |
| Pendientes de revisión | Agrupado, máx. 1 al día |
| Umbral de presupuesto | 50 / 80 / 100 %, una vez por mes y umbral |
| Proyección de exceso | Semanal |
| Resumen | Semanal y mensual |
| Seguridad | Nuevo inicio de sesión, MFA cambiado, Gmail desconectado |

---

## 8. Pantallas

1. **Inicio** — gastado del mes, restante, últimos gastos, alertas, "+ Gasto".
2. **Movimientos** — filtros, búsqueda, edición.
3. **Revisar** — importados pendientes: confirmar / corregir / descartar; al corregir categoría ofrece "aplicar siempre a este comercio".
4. **Plan** — presupuestos con progreso y proyección.
5. **Reportes** — por categoría, cuenta y mes; exportar CSV (step-up).
6. **Cuentas** (con institución y sufijo de tarjeta) y **Categorías**.
7. **Ajustes** — tema claro/oscuro/sistema, Gmail, notificaciones, **Seguridad** (passkeys, TOTP, códigos de recuperación, sesiones activas, actividad reciente).
8. **Registro** (con código de invitación) y **Login** (contraseña + passkey/TOTP).

Responsive (navegación inferior en móvil, lateral en escritorio). PWA con app shell en caché; **los datos de la API no se cachean** en el service worker, y los gastos offline pendientes de enviar se guardan en IndexedDB solo hasta sincronizar. Al cerrar sesión se limpian caché e IndexedDB.

---

## 9. API

```
POST   /api/auth/register            (email, password, invite_code)
POST   /api/auth/login               → requiere segundo paso
POST   /api/auth/mfa/totp | /api/auth/mfa/webauthn/{begin,finish}
POST   /api/auth/step-up/...         POST /api/auth/logout
GET    /api/me                       PATCH /api/me
GET    /api/security/sessions        DELETE /api/security/sessions/{id}
GET    /api/security/activity
POST|DELETE /api/security/passkeys   POST /api/security/totp   POST /api/security/recovery-codes
CRUD   /api/accounts  /api/categories  /api/budgets  /api/merchant-rules
GET    /api/transactions?...         POST/PATCH/DELETE /api/transactions[/{id}]
POST   /api/transactions/{id}/confirm
GET    /api/reports/summary          GET /api/reports/export.csv   (step-up)
GET    /api/gmail/connect (step-up)  GET /api/gmail/callback   DELETE /api/gmail
POST   /api/gmail/webhook            (JWT OIDC Google)
POST   /api/ingest/email             (token + HMAC, desactivable)
GET    /api/push/vapid-key           POST|DELETE /api/push/subscribe
GET    /healthz
GET    /*                            → frontend embebido
```

Todo bajo `/api` (excepto auth pública, webhook y healthz) exige sesión con MFA completado.

---

## 10. Despliegue (Docker + Caddy)

### 10.1 Dockerfile

```dockerfile
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.23-alpine AS api
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pulse ./cmd/pulse

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=api /out/pulse /pulse
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/pulse", "serve"]
```

### 10.2 `/opt/docker/pulse/docker-compose.yml`

```yaml
services:
  pulse:
    build: .
    image: pulse:latest
    container_name: pulse
    restart: unless-stopped
    env_file: .env                     # solo configuración no secreta
    secrets: [pulse_kek, session_key, vapid_private, google_client_secret, pubsub_audience]
    volumes:
      - ./data:/data
    read_only: true
    tmpfs: [/tmp]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    mem_limit: 256m
    cpus: 1.0
    networks: [proxy]
    healthcheck:
      test: ["CMD", "/pulse", "healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3

secrets:
  pulse_kek:            { file: ./secrets/pulse_kek }
  session_key:          { file: ./secrets/session_key }
  vapid_private:        { file: ./secrets/vapid_private }
  google_client_secret: { file: ./secrets/google_client_secret }
  pubsub_audience:      { file: ./secrets/pubsub_audience }

networks:
  proxy:
    external: true
```

`./secrets/` con permisos `600`, propietario root, fuera de cualquier backup del volumen `data`. `/pulse genkeys --out ./secrets` los genera la primera vez. **Guarda una copia de `pulse_kek` en tu gestor de contraseñas**: sin ella los datos cifrados y los backups son irrecuperables.

### 10.3 Caddyfile

```caddy
pulse.espectro.uk {
    encode zstd gzip
    reverse_proxy pulse:8080 {
        header_up -X-Forwarded-User      # nunca reenviar identidades del cliente
    }
    log {
        output file /data/logs/pulse.access.log
        format json
    }
}
```

Las cabeceras de seguridad las pone el binario (una sola fuente de verdad). DNS: registro A `pulse` → IP del VPS. Opcional: Fail2ban con un filtro sobre los fallos de login del `audit_log`.

### 10.4 `.env` (no secreto)

```
PULSE_BASE_URL=https://pulse.espectro.uk
PULSE_DB_PATH=/data/pulse.db
PULSE_TRUSTED_PROXY=172.18.0.0/16
VAPID_PUBLIC_KEY=...
VAPID_SUBJECT=mailto:<tu correo>
GOOGLE_CLIENT_ID=...
GMAIL_PUBSUB_TOPIC=projects/<proyecto>/topics/pulse-gmail
GMAIL_PUBSUB_SA=pulse-push@<proyecto>.iam.gserviceaccount.com
GMAIL_POLL_INTERVAL=15m
INGEST_ENABLED=false
TZ=America/Guayaquil
```

### 10.5 Backups
- Ticker diario: `VACUUM INTO` → cifrado con `age` (clave pública en config, privada solo en tu gestor) → `/data/backups/pulse-YYYYMMDD.db.age`, 14 días de retención.
- Copia externa cifrada a Nextcloud.
- Prueba de restauración mensual (documentada en `deploy/RESTORE.md`).

### 10.6 Actualizar
`git pull && docker compose build && docker compose up -d` → luego CI que publica en GHCR firmada (cosign) y el VPS solo hace `pull`.

---

## 11. Estrategia de pruebas

Regla general: **ninguna fase se cierra con la CI en rojo**, y todo bug encontrado se corrige escribiendo primero el test que lo reproduce.

### 11.1 Niveles de prueba

| Nivel | Herramienta | Qué cubre |
|---|---|---|
| Unitarias (Go) | `go test -race`, tests de tabla | Crypto, auth, normalizadores, parsers, presupuestos |
| Fuzzing (Go) | `go test -fuzz` nativo | Normalizador de montos y fechas, HTML→texto, parsers: nunca deben entrar en pánico ni devolver montos negativos/absurdos con entradas raras |
| Golden files | `testdata/*.eml` → `*.golden.json` | Cada correo real anonimizado tiene su resultado esperado; si un cambio altera la salida, el test falla |
| Integración (Go) | `httptest` + SQLite real en directorio temporal | Endpoints completos: auth, CRUD, aislamiento entre usuarios, migraciones |
| Fakes de servicios externos | servidores `httptest` | Gmail API (`history.list`, `watch`, errores 401/429/500), Pub/Sub (JWT firmados localmente), Web Push (respuestas 201/404/410), Gemini |
| Reloj falso | interfaz `Clock` inyectada | Expiración de sesiones e invitaciones, cambio de mes en presupuestos, renovación del `watch`, tickers — sin `time.Sleep` |
| Frontend unitario | Vitest + Testing Library + MSW | Formato de montos, formularios, tema, estados de error de la API |
| End-to-end | Playwright contra la imagen Docker real | Flujos completos en escritorio y viewport móvil, claro y oscuro |
| Accesibilidad | `@axe-core/playwright` | Contraste en ambos temas, etiquetas, navegación con teclado |
| Seguridad | `gosec`, `govulncheck`, `staticcheck`, `npm audit`, Trivy, OWASP ZAP baseline | Vulnerabilidades en código, dependencias, imagen y respuestas HTTP |
| Humo post-deploy | script `deploy/smoke.sh` | `/healthz`, TLS válido, cabeceras de seguridad, `/api/me` → 401 sin sesión |

### 11.2 Metas de cobertura
- `internal/crypto`, `internal/auth`, `internal/parser`, `internal/budget`: **≥ 90 %**.
- Resto del backend: ≥ 75 %.
- Cada endpoint nuevo: test de "otro usuario no puede leer/editar/borrar esto" (obligatorio, plantilla reutilizable).

### 11.3 Pipeline de CI (GitHub Actions, en cada push y PR)
1. `gofmt`, `golangci-lint`, `staticcheck`, `eslint`, `tsc --noEmit`
2. `go test -race -cover ./...` + 30 s de fuzzing por objetivo
3. `vitest run`
4. Build de la imagen Docker
5. Playwright E2E contra el contenedor recién construido
6. `govulncheck`, `gosec`, `npm audit`, Trivy, ZAP baseline
7. (rama `main`) publicar imagen en GHCR

Además, un fuzzing largo (10 min por objetivo) programado cada noche, y `pre-commit` local con formato + lint + tests rápidos.

### 11.4 Anticipar errores en producción
- **Modo sombra para Gmail**: durante las primeras 2 semanas, todo gasto importado entra como `pending_review` aunque el parser esté seguro. Se mide cuántos corriges; solo se activa la auto-confirmación por banco cuando la precisión es ≥ 98 %.
- **Detector de plantillas cambiadas**: si llega un correo de un remitente bancario conocido y ningún parser lo entiende, se registra (asunto + hash, sin cuerpo) y se avisa por push al admin. Así un cambio de diseño del banco se detecta el mismo día.
- **Conciliación**: reporte mensual que compara la suma importada por tarjeta contra el total del estado de cuenta que ingresas a mano; la diferencia delata correos perdidos.
- **Alertas operativas**: Uptime Kuma sobre `/healthz` (que verifica BD, último sync de Gmail < 1 h y `watch` vigente); Beszel para memoria/CPU del contenedor.
- **Prueba de restauración** automática en CI: crear BD → backup cifrado → descifrar → restaurar → comparar.

---

## 12. Fases de desarrollo

Cada fase termina con sus tests en verde y un criterio de "terminado" verificable.

### Fase 0 — Cimientos y calidad ✅ (3–4 días)
Primero la red de seguridad, luego las funcionalidades.
- [x] Estructura Go + Vite, Makefile (`make dev | test | lint | build | e2e`)
- [x] SQLite + migrador propio + SQL a mano (ver §0); cada test crea una BD migrada en un directorio temporal
- [x] Interfaz `Clock`, logger `slog`, configuración validada al arrancar (falla rápido si falta un secreto)
- [x] Pipeline de CI escrito (`.github/workflows/ci.yml`) — se activa al subir el repo a GitHub
- [x] Dockerfile + compose + bloque de Caddy listos (`deploy/`)
- [ ] **Tú**: desplegar en el VPS siguiendo `deploy/README.md` y agregar el monitor de Uptime Kuma
- **Tests**: migraciones suben y bajan sobre BD vacía; configuración inválida → error claro; humo del contenedor en CI.
- **Terminado cuando**: un push dispara la CI completa en verde y `https://pulse.espectro.uk/healthz` responde.

### Fase 1 — Identidad y seguridad ✅ (1,5 semanas)
- [x] Módulo `crypto`: KEK/DEK, AES-256-GCM con `user_id` como dato asociado
- [x] Invitaciones por CLI, registro, PBKDF2-SHA256 + pimienta (ver §0)
- [x] MFA obligatorio: passkeys (WebAuthn), TOTP, códigos de recuperación
- [x] Sesiones, step-up, rate limit, bloqueo progresivo, CSRF, cabeceras, `audit_log`
- **Tests**:
  - Crypto: ida y vuelta; texto alterado, nonce reutilizado o `user_id` distinto → error; fuzzing.
  - Invitaciones: caducada, usada, revocada, email distinto, fuerza bruta → rechazo genérico y rate limit.
  - Sesiones con reloj falso: expiración por inactividad y absoluta, rotación, revocación inmediata.
  - Sin MFA completado ningún endpoint de datos responde; step-up caducado → 401.
  - CSRF: POST sin `Origin` o con origen ajeno → 403.
  - E2E Playwright: registro con invitación → TOTP (código generado en el test) → login; passkey con el **autenticador virtual WebAuthn** de Chromium.
- **Terminado cuando**: puedes registrarte con una invitación en producción, entrar con passkey y ver tu actividad de seguridad.

### Fase 2 — Núcleo de datos y API ✅ (1 semana)
- [x] Cuentas (institución + sufijo de tarjeta), categorías (jerarquía + set inicial), gastos manuales
- [x] Listado con filtros, búsqueda (descifrado en memoria), paginación
- **Tests**:
  - **Aislamiento**: por cada endpoint, el usuario B no puede ver/editar/borrar datos de A (→ 404).
  - Montos: centavos enteros, rechaza negativos (salvo reversos), decimales de más, montos absurdos.
  - Fechas y zona horaria: un gasto a las 23:30 del día 31 cae en el mes correcto en `America/Guayaquil`.
  - Borrar una categoría con gastos → se reasigna a "Otros", nunca deja huérfanos.
  - Campos sensibles se guardan cifrados (el test lee la fila cruda y verifica que no es texto plano).
- **Terminado cuando**: la API completa pasa los tests de integración y de aislamiento.

### Fase 3 — Frontend PWA ✅ (1,5 semanas)
- [x] Layout responsive (navegación inferior en móvil, lateral en escritorio)
- [x] Tema claro / oscuro / sistema
- [x] Inicio, Movimientos, formulario de gasto en 3 toques, Cuentas, Categorías, Ajustes → Seguridad
- [x] Manifest + service worker (app shell; la API nunca se cachea), limpieza al cerrar sesión
- **Tests**:
  - Vitest: lectura y formato de montos, fechas en zona local.
  - Playwright en viewport de escritorio y de iPhone/Android: crear, editar y borrar un gasto; filtros.
  - Pendiente: regresión visual con capturas de referencia (se genera la referencia la primera vez que corra la CI).
  - axe-core sin violaciones en ambos temas.
  - PWA: manifest válido, service worker registrado, la app carga sin conexión (shell) y tras cerrar sesión no queda ningún dato en caché/IndexedDB.
- **Terminado cuando**: la instalas en tu teléfono y en el escritorio y registras gastos manuales desde ambos.

### Fase 4 — Plan de gastos ✅ (1 semana)
- [x] Presupuestos por categoría, progreso, restante, proyección a fin de mes
- [x] Pantalla Plan e indicadores en Inicio
- **Tests**:
  - Cálculos con reloj falso: día 1, día 15, último día; meses de 28/30/31 días; año bisiesto.
  - Subcategorías suman a la padre; reversos restan.
  - Umbrales 50/80/100 % se disparan **una sola vez** por mes, incluso si se edita o borra un gasto y se vuelve a cruzar.
  - Proyección sin gastos o con un único gasto no divide entre cero.
- **Terminado cuando**: ves tu plan del mes con proyección correcta comparada contra un cálculo manual.

### Fase 5 — Notificaciones (1 semana)
- [x] VAPID, suscripción por dispositivo, alertas de presupuesto, resúmenes, avisos de seguridad, "ocultar montos", horario silencioso
- [x] Cola cifrada persistente, reintentos con espera y limpieza de suscripciones 404/410
- [ ] Aceptación en producción: recibir en un teléfono físico el aviso del 80 % (configuración en `deploy/PUSH.md`)
- **Tests**:
  - Servidor push falso: 201 OK; 404/410 → la suscripción se borra; 429/500 → reintento con espera.
  - Horario silencioso y "ocultar montos" respetados.
  - Ticker de resúmenes con reloj falso: el domingo y el día 1 envía exactamente una vez.
  - E2E: permiso concedido en Playwright, el service worker recibe el push y abre la pantalla correcta al tocarlo.
- **Terminado cuando**: recibes en el teléfono la alerta del 80 % de una categoría de prueba.

### Fase 6 — Parsers de correos, sin conexión a Gmail ✅ (1 semana)
Se construyen y prueban solo con archivos `.eml`, antes de tocar la API de Gmail.
- [x] HTML → texto, normalizador de montos/fechas/comercios
- [x] Parsers Banco Pichincha, Produbanco y Diners/Titanium
- [x] Clasificación: compra, pago de tarjeta, aviso de seguridad, estado de cuenta, transferencia, avance (para revisión manual), plantilla desconocida. Reversos: pendiente de tener un correo real
- [x] Asignación de cuenta (institución + sufijo; se crea si no existe) y categoría (`merchant_rules` ▸ diccionario de comercios ▸ "Otros")
- [x] Importar `.eml` a mano desde Movimientos; "usar siempre esta categoría" al confirmar; `pulse parse` para probar correos reales sin BD
- **Tests**: golden files por banco (incluye latin-1, miles, plantilla nueva, asunto que no coincide con el cuerpo), casos de borde del normalizador, fuzzing de montos, fechas y EML, redacción para Gemini, migración que conserva datos, E2E de importar → revisar → aprender → confirmar.
- **Pendiente de aceptación**: pasar tus `.eml` reales por `pulse parse`.

### Fase 7 — Integración Gmail en vivo ✅ (1,5 semanas)
- [x] OAuth con PKCE + `state` firmado y atado a la sesión (con step-up), revocación en Google al desconectar
- [x] Polling cada `PULSE_GMAIL_POLL_INTERVAL` (15 min) con consulta filtrada por remitente; deduplicación por HMAC del id de Gmail y del Message-ID
- [x] Pub/Sub push con JWT verificado (firma, emisor, audiencia, vigencia, cuenta de servicio); renovación del `watch` en sus últimas 24 h
- [x] Bandeja "Por revisar" + aprendizaje de categoría; Gemini opt-in con step-up
- [x] **Modo sombra** activado por defecto
- [x] Avisos push: gasto nuevo, presupuesto, y de seguridad (Gmail conectado/desconectado/sin acceso, Gemini activado)
- **Tests** (Google falso en Go y en Node para E2E):
  - 401 de Gmail → se pide otro token y se reintenta una vez; `invalid_grant`/401 persistente → conexión rota, aviso y sin más intentos; 429/5xx → espera exponencial (1, 2, 4… min, máx. 6 h) respetando `Retry-After`.
  - El mismo mensaje por push y por polling a la vez (6 sincronizaciones concurrentes con `-race`) → **un** gasto y una descarga.
  - JWT de Pub/Sub: firma inválida o de otra clave, `alg` none/HS256, `aud` incorrecto, caducado, emitido en el futuro, cuenta de servicio distinta, email sin verificar, kid desconocido → 401 sin procesar nada; rotación de claves.
  - La BD tras sincronizar no contiene en claro el correo, el email de la cuenta, el comercio ni los tokens.
  - Dos usuarios: los correos de A nunca crean gastos en B; una cuenta de Gmail no se puede conectar a dos usuarios.
  - `state` de otro usuario, de otra sesión, caducado o alterado → rechazado (también con fuzzing). PKCE incorrecto o código reutilizado → rechazado.
  - E2E (escritorio y móvil): conectar con step-up → Google falso → volver → sincronizar → gasto por revisar → desconectar con revocación.
- **Pendiente de aceptación**: un consumo real con tu tarjeta aparece en Pulse en menos de 1 minuto (Pub/Sub) o 15 minutos (polling). Configuración en `deploy/GMAIL.md`.

### Fase 8 — Producción endurecida (1 semana)
- [x] Contenedor `read_only`, sin capacidades, Docker secrets, límites de recursos
- [x] Backups cifrados con `age` + copia a Nextcloud (`deploy/backup-offsite.sh`)
- [x] `/healthz` enriquecido (esquema, Gmail, push) · [ ] **Tú**: monitores en Uptime Kuma y Beszel
- [ ] Detector de plantillas cambiadas y reporte de conciliación
- [x] Imagen firmada en GHCR; `deploy/smoke.sh` tras cada despliegue
- [x] Credenciales de Google (OAuth, Pub/Sub, Gemini) configurables por un admin desde Ajustes, cifradas y aplicadas en caliente
- **Tests**:
  - [x] Restauración automática de backup en CI (base vacía → `restore` → arranque sano con los datos).
  - ZAP baseline sin alertas medias/altas; Trivy sin CVEs críticas.
  - [x] El contenedor arranca con sistema de archivos de solo lectura (prueba en CI).
  - Smoke test contra producción después de cada deploy.
- **Terminado cuando**: restauras un backup en una máquina limpia y todo funciona; los monitores están en verde.

### Fase 9 — Estabilización y mejoras (continuo)
- [ ] Revisar métricas del modo sombra y activar auto-confirmación por banco (≥ 98 %)
- [ ] Reportes, exportación CSV, gastos recurrentes, gastos offline con sincronización
- [ ] Más bancos (débito, transferencias) siguiendo el mismo proceso: `.eml` → golden → fuzz → sombra

**Duración estimada total**: 10–12 semanas a tiempo parcial.

---

## 13. Pendientes para arrancar
1. **Correos originales (`.eml`)**: en Gmail → ⋮ → *Descargar mensaje*, 3–5 por banco (Pichincha, Produbanco, Diners/Titanium). Las capturas sirven para el diseño, pero la regex se escribe contra el HTML real. Incluir, si existen, ejemplos de pago a tarjeta, reverso y avance de efectivo para saber qué descartar.
2. **Asunto del correo de Banco Pichincha** (no se ve en la captura).
3. **¿Usas tarjetas de débito o transferencias** con notificación por correo? Serían parsers adicionales.
