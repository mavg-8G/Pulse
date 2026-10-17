# Pulse — Plan de desarrollo

Registro de gastos personales multiusuario: categorías, cuentas (origen), fechas y plan de gastos (tope mensual por categoría). PWA instalable en escritorio y móvil, modo claro/oscuro, notificaciones push, ingreso manual y captura automática desde Gmail.

**Estado (16 oct 2026): fases 0–8 terminadas y probadas, incluidas sus aceptaciones en producción (despliegue, monitores, push en un teléfono físico y consumo real por Gmail); fases 10–13 implementadas con sus pruebas, a falta de las aceptaciones con datos reales; la 9 sigue en curso**. Están listos el rediseño de todas las vistas, las estadísticas y gráficas de líneas de Reportes, las proyecciones de la próxima semana/mes por categoría y total, CSV, gastos fijos, gastos sin conexión y métricas del modo sombra. Quedan pendientes las capturas de referencia de la fase 3 y las tareas de la fase 9. Ver §0 para los cambios respecto al plan original y [`CHANGELOG.md`](CHANGELOG.md) para la verificación de cada entrega.

**Ampliación (17 oct 2026):** fases 14, 15, 16, 18 y 19 implementadas para mejorar la comodidad de uso de las funciones existentes, con 80 pruebas unitarias, 90 escenarios funcionales E2E verificados entre la suite y sus repeticiones, y comparación local de rendimiento completada. La fase 19 conserva la aceptación pendiente con un lector de pantalla real. La fase 17 queda reservada para un futuro rediseño visual de escritorio y móvil, pendiente de definir. Alcance y resultados en [`docs/CALIDAD_DE_VIDA.md`](docs/CALIDAD_DE_VIDA.md).

### Próximos pasos

1. Comparar las proyecciones semanales y mensuales con el gasto real acumulado y revisar la precisión por banco antes de desactivar el modo sombra.
2. Incorporar nuevos bancos; establecer imágenes de referencia para detectar cambios visuales automáticamente.
3. Aceptaciones de las fases 10–13 (§12): probar el modo avión en tu teléfono, configurar el destino del respaldo externo y restaurarlo en una máquina limpia, y comprobar en unos meses que el error de la proyección elegida no supera al del ritmo actual.

4. Mejoras de calidad de vida: completar la aceptación con lector de pantalla (§12). La fase 17 se abordará más adelante como proyecto de rediseño visual.

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
| Backups cifrados | Respaldo en caliente con `VACUUM INTO`, cifrado externo con `age` y copia a Nextcloud; restauración automatizada en CI | Guía y procedimiento de restauración en `deploy/README.md` |
| Polling con `history.list` | `messages.list` con la consulta de remitentes bancarios (`from:(…) newer_than:Nd`); `historyId` solo para descartar avisos de Pub/Sub viejos | `history.list` devuelve ids de **todos** los correos nuevos; con la consulta filtrada en Google, Pulse nunca ve ni el id de un correo que no sea del banco. Sin historial que caduque, no hay "resincronización completa" que probar |
| Callback OAuth `GET /api/gmail/callback` | Google vuelve a la página `/ajustes/gmail`, que envía código y `state` con `fetch` a `POST /api/gmail/callback` | La cookie de sesión es `SameSite=Strict` y no viaja en la redirección desde Google; además el código no queda en logs de acceso |
| `state` y verificador PKCE guardados | `state` firmado con HMAC (usuario + sesión + caducidad 10 min); verificador = HMAC(nonce) con clave del servidor | Nada que guardar ni limpiar; el verificador nunca viaja en la URL |
| Modo sombra (sin detalle) | Activado por defecto: todo lo importado queda "Por revisar". Apagado: lectura segura con regla de comercio, o categoría del diccionario si el banco alcanza 20 revisiones y 98 % de aciertos | La precisión se mide con las correcciones de monto, fecha, tipo y cuenta |
| Gemini como fallback | Solo para remitentes bancarios con plantilla desconocida, activación por usuario con step-up, texto redactado (también el nombre en el asunto), resultado validado y siempre por revisar | Mínimo dato posible a un tercero |
| Push "nuevo gasto" | Aviso "Nuevo gasto por revisar" (o "Gasto registrado") para consumos de las últimas 24 h, con la preferencia de alertas de presupuesto y "ocultar montos" | La primera sincronización (90 días) no inunda de avisos |

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

1. **Inicio** — gasto del mes, presupuesto, saldo disponible o exceso, promedio diario, cierre estimado, últimos movimientos y pendientes de revisión.
2. **Movimientos** — filtros, búsqueda, edición.
3. **Por revisar**, dentro de Movimientos — importados pendientes: confirmar / corregir / descartar; al corregir categoría ofrece "aplicar siempre a este comercio".
4. **Plan** — presupuestos con progreso y proyección, y gestión de gastos fijos mensuales.
5. **Reportes** — estadísticas y líneas de 3/6/12 meses, distribución por categoría y cuenta, proyecciones de la próxima semana o mes por categoría y total, CSV (step-up) y conciliación.
6. **Cuentas** (con institución y sufijo de tarjeta) y **Categorías**.
7. **Ajustes** — tema claro/oscuro/sistema, animaciones, Gmail, notificaciones y privacidad.
8. **Mi cuenta** — contraseña, passkeys, TOTP, códigos de recuperación, sesiones activas, actividad de seguridad y cierre de sesión.
9. **Registro** (con código de invitación), **Entrar** (contraseña + passkey/TOTP), portada y páginas públicas bilingües de presentación, privacidad y términos.

Responsive (navegación inferior en móvil, lateral en escritorio). PWA con app shell en caché; **los datos de la API no se cachean** en el service worker, y los gastos offline pendientes de enviar se guardan en IndexedDB solo hasta sincronizar. Al cerrar sesión se limpian caché e IndexedDB.

La identidad visual usa azul petróleo con acentos suaves en salvia, lavanda y azul grisáceo. Las vistas claro/oscuro, formularios, tablas, gráficas y páginas públicas comparten estilos; los colores personalizados de cuentas y categorías se conservan con menor saturación al mostrarse.

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
POST   /api/gmail/connect (step-up)  POST /api/gmail/callback  DELETE /api/gmail
POST   /api/gmail/webhook            (JWT OIDC Google)
POST   /api/ingest/email             (token + HMAC, desactivable)
GET    /api/push/vapid-key           POST|DELETE /api/push/subscribe
GET    /healthz
GET    /api/version                  → fecha UTC de construcción y commit, público y sin caché
GET    /*                            → frontend embebido
```

Todo bajo `/api` (excepto auth pública, webhook y los metadatos públicos de `/api/version`) exige sesión con MFA completado. `/healthz` también es público.

`/api/reports/summary` incluye las estadísticas, series mensuales y proyecciones; la ampliación no añade un endpoint ni una migración. Las proyecciones usan hasta 90 días completos de historial, independientemente del rango mensual solicitado.

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
- [x] **Tú**: desplegar en el VPS siguiendo `deploy/README.md` y agregar el monitor de Uptime Kuma
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
- [x] Inicio, Movimientos, formulario de gasto, Cuentas, Categorías, Ajustes y Mi cuenta (seguridad y sesiones)
- [x] Rediseño de todas las vistas con azul petróleo y acentos suaves, temas claro/oscuro coordinados, tarjetas y formularios adaptables; portada, acceso, páginas legales e iconos PWA incluidos
- [x] Cinco accesos en la barra móvil (Inicio, Movimientos, Plan, Reportes y Ajustes) y Nuevo gasto en la cabecera
- [x] Manifest + service worker (app shell; la API nunca se cachea), limpieza al cerrar sesión
- **Tests**:
  - Vitest: lectura y formato de montos, fechas en zona local.
  - Playwright en viewport de escritorio y de iPhone/Android: crear, editar y borrar un gasto; filtros.
  - Capturas y comprobación de accesibilidad y desbordamiento horizontal de todas las vistas en escritorio/móvil y claro/oscuro (`web/e2e/design.spec.ts`).
  - Pendiente: establecer capturas de referencia y una comparación automática de píxeles; las capturas actuales se revisan visualmente.
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

### Fase 5 — Notificaciones ✅ (1 semana)
- [x] VAPID, suscripción por dispositivo, alertas de presupuesto, resúmenes, avisos de seguridad, "ocultar montos", horario silencioso
- [x] Cola cifrada persistente, reintentos con espera y limpieza de suscripciones 404/410
- [x] Aceptación en producción: recibir en un teléfono físico el aviso del 80 % (configuración en `deploy/PUSH.md`)
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
- **Aceptación con `.eml` reales** ✅: probada y terminada (16 oct 2026). Produbanco se comprobó con tres consumos reales, incluidos HTML con saltos y fechas mes/día/año.

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
- **Aceptación** ✅ (16 oct 2026): un consumo real con tu tarjeta aparece en Pulse en menos de 1 minuto (Pub/Sub) o 15 minutos (polling). Configuración en `deploy/GMAIL.md`.

### Fase 8 — Producción endurecida ✅ (1 semana)
- [x] Contenedor `read_only`, sin capacidades, Docker secrets, límites de recursos
- [x] Backups cifrados con `age` + copia a Nextcloud (`deploy/backup-offsite.sh`)
- [x] `/healthz` enriquecido (esquema, Gmail, push) · [x] **Tú**: monitores en Uptime Kuma y Beszel
- [x] Detector de plantillas cambiadas: un correo de un banco conocido que ningún parser entiende se guarda (asunto cifrado + hash, sin cuerpo), se lista en Ajustes → Gmail y avisa por push a los admins (uno por banco y día)
- [x] Conciliación en Reportes: lo registrado por cuenta y mes contra el total del estado de cuenta que ingresas (mes calendario, no ciclo de corte)
- [x] Imagen firmada en GHCR; `deploy/smoke.sh` tras cada despliegue
- [x] Credenciales de Google (OAuth, Pub/Sub, Gemini) configurables por un admin desde Ajustes, cifradas y aplicadas en caliente
- **Tests**:
  - [x] Restauración automática de backup en CI (base vacía → `restore` → arranque sano con los datos).
  - [x] ZAP baseline sin alertas medias/altas (job `image`, contra el contenedor recién construido); [x] Trivy sin CVEs críticas.
  - [x] El contenedor arranca con sistema de archivos de solo lectura (prueba en CI).
  - [x] Smoke test contra producción después de cada deploy (`deploy.sh` → `smoke.sh`, con rollback si falla).
- **Terminado cuando**: restauras un backup en una máquina limpia y todo funciona; los monitores están en verde.

### Fase 9 — Estabilización y mejoras (continuo)
- [x] Métricas del modo sombra: cada gasto importado anota si se confirmó tal cual o hubo que corregir monto, fecha, tipo o cuenta; Ajustes → Gmail muestra los aciertos por banco
- [x] Auto-confirmación por banco: con el modo sombra apagado, un banco con ≥ 20 revisiones y ≥ 98 % de aciertos se confirma solo si la categoría salió del diccionario (antes solo con regla de comercio)
- [x] Reportes (por mes, categoría y cuenta), exportación CSV con step-up, gastos fijos mensuales y gastos sin conexión (IndexedDB hasta enviar; id del cliente para no duplicar)
- [x] Estadísticas de gasto neto, promedio diario, compra promedio, compras y devoluciones, y variación entre meses cerrados; gráficas de líneas del total y categoría con valores exactos y tabla
- [x] Proyecciones para la próxima semana calendario y el próximo mes por categoría principal y total, basadas en hasta 90 días completos; mínimo 7 días y aviso antes de 28, devoluciones descontadas y subcategorías incluidas
- [x] Periodo de Reportes independiente del historial de proyección; selecciones conservadas entre 3/6/12 meses y movimientos futuros excluidos de los indicadores
- [x] Pruebas de analítica para aislamiento entre usuarios, zona horaria, cambios de horario, meses bisiestos, historial insuficiente y redondeo; E2E de estadísticas y proyecciones en escritorio/móvil
- [x] Avisos de éxito de Gmail después de actualizar el estado y el contador de importados; pruebas OAuth esperan explícitamente la respuesta del servidor
- [x] Corregida la lectura de fechas mes/día/año de Produbanco con correos reales; promociones y acuses de pago excluidos de las alertas y de Gemini
- [x] Reintento de correos pendientes desde Ajustes → Gmail, conservando la deduplicación; total de alertas independiente de la vista previa de 20 y retirada de alertas resueltas
- [x] Fecha de construcción y commit del servidor en el pie, convertidos a la zona del usuario; metadatos grabados en el binario y consultables por API y CLI para verificar la versión del VPS
- [ ] Contrastar las proyecciones con los resultados reales de las próximas semanas y meses antes de ajustar el método de estimación
- [ ] **Tú**: revisar en unas semanas los aciertos reales por banco antes de apagar el modo sombra
- [ ] Más bancos (débito, transferencias) siguiendo el mismo proceso: `.eml` → golden → fuzz → sombra

### Fase 10 — Uso sin conexión más completo ✅ (pruebas; falta la aceptación en tu teléfono)
- [x] Cola cifrada en IndexedDB para ediciones y borrados de gastos, con `Idempotency-Key` por cambio y `version`/`If-Match` (migración `0011`)
- [x] Gastos fijos (crear, editar, eliminar) y categoría asignada en "Por revisar" también sin conexión
- [x] Lectura sin conexión: último Inicio, Movimientos recientes y Plan cifrados (AES-GCM, clave no exportable, una base por persona, 7 días máximo) y borrados al cerrar sesión
- [x] Resolución de conflictos (usar la mía / conservar la del servidor / volver a crear si se borró); indicador de pendientes y reintento al recuperar la red y cada 30 s
- [ ] **Tú**: modo avión real en tu teléfono (editar, borrar y volver a la red)
- **Tests**: edición sin conexión → reconexión → un solo cambio aplicado; borrado concurrente en otro dispositivo; cola vacía tras cerrar sesión; aislamiento entre usuarios en el mismo navegador; E2E con la red cortada en escritorio y móvil.
- **Terminado cuando**: en modo avión puedes ver tus movimientos recientes, editar y borrar gastos, y todo se sincroniza sin duplicados al volver la red.

### Fase 11 — Presupuestos más flexibles ✅
- [x] Sobrante o exceso que pasa al mes siguiente, activable por categoría y desde un mes elegido
- [x] Topes por mes específico (por ejemplo, diciembre distinto) y tope por cuenta además del de categoría (migración `0012`)
- [x] Umbrales de aviso configurables por presupuesto (antes fijos en 50, 80 y 100 %) y copia del plan del mes anterior
- **Tests**: arrastre con reloj falso entre meses de 28/30/31 días y año bisiesto; los umbrales se disparan una sola vez por mes aunque se cambie el tope; subcategorías y reversos siguen sumando a la categoría principal; migración que conserva los presupuestos actuales.
- **Terminado cuando**: un sobrante de un mes aparece en el siguiente y los avisos respetan los umbrales que elegiste.

### Fase 12 — Respaldo en base de datos externa y respaldo manual ✅ (pruebas; falta configurar el destino real)
Decisión (16 oct 2026): copia cifrada antes de salir del servidor. Servicio dentro del binario (`internal/backup`): `VACUUM INTO` → cifrado `age` con claves públicas (la privada no está en el servidor) → S3 compatible o WebDAV. Se descartaron Litestream (replica el WAL sin cifrar del lado del cliente y añade otro proceso) y PostgreSQL (duplicaría el modelo y expondría datos a otra base).
- [x] Respaldo continuo a un almacenamiento externo (`PULSE_BACKUP_INTERVAL`, cada 24 h por defecto), siempre cifrado; nada sale en claro
- [x] Botón **Respaldar ahora** en Ajustes (admin, con step-up): genera el respaldo cifrado, lo envía al destino y muestra fecha, tamaño, huella y resultado (migración `0013`)
- [x] Descarga del respaldo cifrado (step-up), estado en `/healthz` y aviso push si el último respaldo falla o es antiguo
- [x] Guía de restauración (`pulse restore --identity`) en `deploy/README.md`
- [ ] **Tú**: crear el bucket con la política de solo escritura, generar la clave `age` fuera del servidor, configurar las variables y restaurar en una máquina limpia
- [ ] CI: las pruebas de respaldo ya corren con `go test ./...`; revisar el workflow (no está en la carpeta del proyecto) por si filtra paquetes o necesita `age` para la prueba de restauración
- **Tests**: ida y vuelta con datos conocidos (respaldar → base vacía → restaurar → comparar); destino caído → reintento con espera y aviso; respaldo cifrado sin datos legibles; solo un admin con step-up puede lanzarlo; automatizado en CI.
- **Terminado cuando**: restauras desde la base externa en una máquina limpia y el respaldo manual deja constancia en la actividad de seguridad.

### Fase 13 — Reportes con más gráficas y proyecciones inteligentes ✅ (pruebas; falta contrastar con meses reales)
- [x] Proyección del gasto hasta fin de mes con **regresión lineal** sobre el acumulado diario, con intervalo de predicción del 95 % y comparación contra el método actual (`internal/forecast`, `GET /api/reports/projection`)
- [x] Media móvil ponderada, ajuste por día de la semana y perfil del mes (≥ 3 meses cerrados); selección automática por menor error medio en hasta 6 meses cerrados
- [x] Proyección por categoría y total, incluyendo gastos fijos conocidos y por venir y excluyendo atípicos de forma explícita
- [x] Precisión: error de cada método contra el cierre real de cada mes, visible en Reportes
- [x] Más gráficas: acumulado del mes contra el anterior, barras por categoría con comparación, día de la semana, mapa de calor y evolución por cuenta; todas con tabla alternativa y teclado
- [x] Avisos de historial insuficiente y sin dividir entre cero
- [x] Proyección de la próxima semana y el próximo mes con selección por validación (promedio, media móvil, día de la semana), rango del 95 % simulado y su cobertura; desglose del Plan por categoría y comercios, y gasto por cuenta y categoría en Reportes
- [ ] **Tú**: revisar en unos meses que el error del método elegido sea ≤ al del ritmo actual con tus datos
- **Tests**: regresión contra resultados calculados a mano; mes con pocos días de datos; devoluciones y subcategorías; zona horaria y cambio de horario; meses bisiestos; aislamiento entre usuarios; E2E de las gráficas en escritorio/móvil y claro/oscuro con axe-core.
- **Terminado cuando**: la proyección de fin de mes muestra valor y rango, y su error contra el cierre real se puede consultar y es menor o igual que el del método actual.

**Estimación original**: 10–12 semanas a tiempo parcial; la fase 9 continúa según las aceptaciones y mejoras pendientes. Las fases 10 a 13 se incorporaron e implementaron el 16 oct 2026; quedan sus aceptaciones con datos y dispositivos reales.

---

### Fases 14–19 — Calidad de vida (implementación del 17 oct 2026)

**Objetivo:** reducir pasos, pérdida de contexto, incertidumbre y esperas en las funciones actuales. Estas fases no añaden nuevos módulos financieros ni cambian la identidad visual. Las correcciones de accesibilidad y comportamiento necesarias sí forman parte de ellas.

**Punto de partida revisado:** Reportes ya conserva selecciones en URL y sesión; Movimientos solo lee inicialmente el mes de la URL y mantiene búsqueda, filtros y límite en estado local. `TxForm` conserva los campos cuando falla un envío y ya usa teclado decimal; la capa sin conexión dispone de cifrado, idempotencia y control de versiones. `Sheet` usa un diálogo nativo y cierra con Escape, botón o fondo. Se ampliarán estas bases sin duplicarlas.

**Orden de ejecución:** 15 → 14 → 16 → 18 → 19. La numeración conserva los acuerdos de alcance; no obliga al orden de entrega. La 15 fija el contexto de navegación; la 14 protege la edición; la 16 unifica la recuperación; la 18 optimiza esos recorridos y la 19 cierra la revisión transversal. La 17 no bloquea ninguna entrega.

**Reglas comunes de ejecución y aceptación:**
- Revisar primero qué está resuelto y registrar los casos concretos que cambian, con su comportamiento anterior y esperado.
- Conservar aislamiento entre personas, cifrado local, limpieza al cerrar sesión, MFA y semántica de la cola sin conexión. No guardar comercios, notas, montos o borradores en texto plano ni en URLs.
- Cada fase se entrega por separado, con tareas implementadas, comprobaciones automatizadas pertinentes y aceptación manual registradas como estados distintos. No marcarla terminada solo por compilar.
- Reutilizar E2E y componentes existentes. Añadir pruebas de comportamiento donde haya riesgo de pérdida de datos, navegación incorrecta, duplicados o regresiones de accesibilidad; evitar pruebas que solo reproduzcan el código.
- Comprobar los recorridos afectados en escritorio y móvil, en claro y oscuro. Las verificaciones físicas que no pueda reproducir el navegador automatizado quedan explícitamente pendientes.
- Las aceptaciones abiertas de las fases 10, 12 y 13 siguen vigentes; no impiden desarrollar estas mejoras ni se consideran cerradas por ellas.

### Fase 14 — Registro y edición más ágiles (implementada y probada)

**Objetivo:** crear y corregir un gasto con menos fricción y sin perder una edición por un cierre accidental.

- [x] Levantar el recorrido de creación, edición y confirmación de «Por revisar»: foco inicial, tabulación, validación, guardado y retorno a la lista.
- [x] Recordar la última cuenta usada al crear un gasto, por persona y dispositivo, usando el almacenamiento cifrado existente. Prioridad: cuenta explícita del contexto, última cuenta válida y primera cuenta elegible. Al editar, conservar siempre la del movimiento; ignorar preferencias de cuentas archivadas o fusionadas que ya no sean elegibles.
- [x] Mejorar entrada y corrección del importe: conservar el texto mientras se escribe, aceptar los formatos ya soportados y enfocar el primer campo inválido sin borrar los demás.
- [x] Detectar cambios sin guardar y ofrecer «Seguir editando» o «Descartar cambios» al cerrar por botón, fondo, Escape o navegación interna. No avisar si nada cambió o si el guardado terminó; al cerrar la pestaña, usar el aviso nativo cuando el navegador lo permita.
- [x] Mantener el formulario durante errores y reautenticación; limpiar solo tras éxito confirmado o encolado seguro. No añadir persistencia de borradores entre recargas en esta fase.
- [x] Unificar bloqueo de acciones mientras se guarda o borra y conservar la identidad de la operación en reintentos de respuesta incierta, reutilizando la capa idempotente existente.
- **Validación:** doble clic/Enter, respuesta perdida tras guardar, error de validación, cancelación de step-up, cuenta recordada que deja de ser elegible, cierre con y sin cambios y cambio de persona en el mismo navegador. Verificar creación, edición y «Por revisar» con y sin conexión.
- **Terminado cuando:** una edición sobrevive a un error recuperable, un cierre accidental permite seguir editando y repetir el envío no genera otro gasto. La cuenta recordada nunca procede de otra persona.
- **Dependencia:** contexto y retorno definidos en la fase 15; los textos se armonizan después en la 16.

### Fase 15 — Navegación y contexto persistentes (implementada y probada)

**Objetivo:** abrir un movimiento, editarlo o visitar otra pantalla y volver al mismo contexto.

- [x] Definir una política común para Movimientos, Plan y Reportes: URL explícita válida → estado de la vista en la sesión de esa persona → valores iniciales del calendario del usuario. Los estados de cada pantalla son independientes; Reportes conserva su comportamiento existente.
- [x] Conservar mes, filtros, cantidad de movimientos cargados y posición de desplazamiento de Movimientos. Mantener búsqueda solo en memoria de la sesión autenticada, sin incluirla en la URL ni en almacenamiento persistente sin cifrar.
- [x] Sincronizar mes y filtros no sensibles con la URL y validar parámetros. Sustituir la entrada del historial durante escritura o ajuste de filtros; crear entradas solo para navegación deliberada. No provocar una entrada por pulsación.
- [x] Restaurar el contexto al usar Atrás/Adelante o volver de otra pantalla. Esperar a que estén los datos necesarios para restaurar el desplazamiento; si desapareció una fila, regresar a la posición válida más próxima.
- [x] Conservar el mes de Plan y los filtros existentes de Reportes al navegar; ignorar referencias que ya no sean válidas sin bloquear la página.
- [x] Definir el cierre de formularios: cerrar recupera el foco del elemento que los abrió; si ya no existe, usar un destino lógico de la lista. Una entrada directa sin historial vuelve a la pantalla correspondiente sin salir de Pulse.
- [x] Borrar el contexto al cerrar sesión o cambiar de persona. Distinguir «Limpiar filtros» de «Volver al mes actual» para evitar reinicios inesperados.
- **Validación:** filtrar → cargar más → desplazarse → editar → volver; navegar a Plan y regresar; Atrás/Adelante; enlace directo y recarga; parámetros inválidos; cuenta archivada/fusionada; sesión de otra persona. Comprobar también que una respuesta antigua no reemplace los resultados del filtro actual.
- **Terminado cuando:** el recorrido anterior conserva periodo, filtros, filas cargadas y posición sin contaminar otras vistas ni otra sesión. La búsqueda no aparece en la URL.

### Fase 16 — Mensajes y recuperación de errores (implementada y probada)

**Objetivo:** explicar qué ocurrió, qué se guardó y qué acción permite continuar.

- [x] Inventariar mensajes de formularios, cargas, importación, Gmail, respaldo y cola sin conexión; definir una tabla de textos y acciones por situación.
- [x] Distinguir validación, falta de red, sesión vencida, reautenticación cancelada, conflicto, límite temporal y fallo del servidor. Mostrar una instrucción útil sin exponer detalles internos ni datos sensibles.
- [x] Colocar errores de campo junto a su control y asociarlos de forma accesible; reservar el mensaje general para errores de la operación. Mantener los valores y evitar avisos repetidos de la misma causa.
- [x] Añadir reintento donde falte para consultas recuperables, conservando filtros y datos ya visibles. Para escrituras, reutilizar el mecanismo idempotente; no repetir automáticamente acciones destructivas fuera de ese mecanismo.
- [x] Unificar los estados «Guardado», «Pendiente de enviar», «Sincronizando» y «Requiere tu atención». Un cambio encolado nunca debe anunciarse como confirmado por el servidor.
- [x] Presentar conflictos con sus opciones existentes en lenguaje consistente. Cancelar una confirmación o step-up no debe mostrarse como fallo técnico.
- [x] Mostrar resultados parciales de importaciones de varios correos de forma legible y persistente mientras se revisan, sin depender de un aviso fugaz con todo el texto concatenado.
- **Validación:** simular desconexión, 401, 409, 429 y 5xx en los recorridos afectados; comprobar valores conservados, foco, mensajes accesibles, reintento sin duplicación y distinción entre éxito parcial y total.
- **Terminado cuando:** cada fallo recuperable ofrece una salida concreta y el estado mostrado corresponde a lo sucedido en el servidor o en la cola local.
- **Dependencia:** reutiliza la protección de edición de la 14 y el contexto de la 15.

### Fase 17 — Rediseño visual de escritorio y móvil (reservada; pospuesta)

Se definirá más adelante con el usuario. Incluirá la revisión visual de la aplicación para ambos tamaños, su alcance y criterios de aprobación. No se fijan ahora paleta, distribución, navegación ni tareas de implementación. Las fases 14, 15, 16, 18 y 19 conservan el diseño vigente y solo ajustan lo necesario para sus objetivos de uso y accesibilidad.

### Fase 18 — Rapidez y estabilidad visual (implementada y probada)

**Objetivo:** reducir esperas evitables y saltos de contenido en los recorridos cotidianos.

- [x] Registrar una línea base reproducible de Inicio, Movimientos, Plan y Reportes: duración de carga y cambio de filtro, peticiones y saltos de disposición. Usar datos ficticios de 0, 100 y 5 000 movimientos, misma compilación de producción y perfiles de red/equipo documentados.
- [x] Revisar invalidaciones después de guardar, confirmar o borrar. Actualizar las vistas afectadas —incluido Reportes— y evitar consultas repetidas de datos que no cambiaron. Conservar recálculos necesarios del servidor y la superposición de cambios locales.
- [x] Cancelar consultas obsoletas cuando la capa de lectura lo permita y agrupar la búsqueda mientras se escribe. Si una petición no se puede cancelar, impedir que sus resultados sustituyan los del contexto actual.
- [x] Mantener datos previos al actualizar el mismo contexto, con indicador discreto de actualización; al cambiar de periodo o filtro, no mostrar cifras anteriores como si fueran las nuevas.
- [x] Reservar espacio para indicadores, gráficas y estados de carga; evitar desmontar la página o reiniciar el desplazamiento durante una actualización.
- [x] Optimizar renderizados y carga de recursos solo donde las mediciones identifiquen un coste; conservar teclado, tablas alternativas, foco y movimiento reducido.
- [x] Comparar antes/después en las mismas condiciones. Fijar como presupuesto que las medianas de cinco ejecuciones por recorrido no empeoren más del 10 %; cualquier excepción debe quedar explicada. En el recorrido optimizado, demostrar la reducción concreta de tiempo, peticiones redundantes o saltos que motivó el cambio. Resultado: todos los casos cumplen; 3–4 lecturas API menos por carga y una consulta por búsqueda rápida. Tabla y límites de la medición en `docs/CALIDAD_DE_VIDA.md`.
- **Validación:** recorridos medidos con red normal y lenta, cambio rápido de filtros, guardado seguido de apertura de Reportes y reconexión con pendientes. Comprobar cifras finales y ausencia de estados mezclados; no limitar la aceptación al tiempo de carga.
- **Terminado cuando:** las mejoras tienen comparación reproducible, los datos permanecen correctos y las actualizaciones no desplazan ni vacían innecesariamente la vista.
- **Dependencia:** medir tras 15, 14 y 16 para optimizar el comportamiento final.

### Fase 19 — Consistencia y accesibilidad (implementada y probada; aceptación con lector pendiente)

**Objetivo:** que las mismas acciones se entiendan y funcionen igual en toda la aplicación.

- [x] Crear un inventario de términos y controles existentes: gasto/movimiento, cuenta/tarjeta, guardar/confirmar, cancelar/cerrar, borrar y estados de sincronización. Elegir cada término según la acción real y aplicarlo en las vistas afectadas.
- [x] Unificar formato de importes, devoluciones, fechas y periodos usando las utilidades actuales y la zona horaria del usuario, sin cambiar la precisión ni la interpretación de los datos.
- [x] Revisar nombres accesibles, etiquetas de formulario, ayuda y errores asociados, orden de tabulación, foco visible y su restauración al cerrar diálogos. Evitar que Escape descarte una edición protegida por la fase 14.
- [x] Revisar contraste de texto y controles según WCAG 2.2 AA, estados que dependan solo del color, texto ampliado al 200 % y reflujo a 320 px; aplicar correcciones puntuales con el diseño vigente.
- [x] Unificar confirmaciones destructivas con objeto y consecuencia explícitos; mantener la distinción entre borrar un gasto, quitar una repetición y separar cuentas.
- [x] Comprobar anuncios de éxito/error sin interrupciones duplicadas, preferencias de movimiento reducido y alternativas por tabla/teclado de las gráficas existentes.
- [ ] Ejecutar axe en las vistas afectadas y revisar manualmente los recorridos principales con teclado y un lector de pantalla. Registrar navegador, lector y resultado; axe por sí solo no acredita la aceptación.
- **Validación:** crear/editar un gasto, filtrar y volver, confirmar «Por revisar», modificar el Plan y consultar Reportes, en ambos temas y tamaños. Completar las comprobaciones automáticas con lectura y foco manuales.
- **Terminado cuando:** no quedan hallazgos de accesibilidad críticos o serios de axe en las vistas revisadas ni bloqueos manuales de teclado o lector; acciones equivalentes usan lenguaje y comportamiento consistentes.
- **Dependencia:** revisión transversal después de 18. Los problemas de accesibilidad detectados en fases anteriores se corrigen en su entrega, sin esperar a esta fase.

---

## 13. Pendientes para arrancar
1. **Correos originales (`.eml`)**: en Gmail → ⋮ → *Descargar mensaje*, 3–5 por banco (Pichincha, Produbanco, Diners/Titanium). Las capturas sirven para el diseño, pero la regex se escribe contra el HTML real. Incluir, si existen, ejemplos de pago a tarjeta, reverso y avance de efectivo para saber qué descartar.
2. **Asunto del correo de Banco Pichincha** (no se ve en la captura).
3. **¿Usas tarjetas de débito o transferencias** con notificación por correo? Serían parsers adicionales.
