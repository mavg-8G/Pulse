# Registro de cambios de Pulse

Este registro compara el proyecto actual con `_to_delete/pulse-fases-0-4.tar.gz` y recoge el trabajo documentado en esta conversación. El proyecto no tiene historial Git: no se puede confirmar la autoría ni reconstruir cada edición intermedia. La fecha corresponde al registro local, no a una publicación oficial.

## Sin publicar — CI y despliegue (14 oct 2026)

- Contenedor de la prueba de humo renombrado de `p` a `pulse-ci-smoke`, para evitar el rechazo de Docker observado en Actions. Logs y limpieza se ejecutan también cuando falla la prueba.

- Trivy 0.65.0 → 0.76.0: descarga directa desde GitHub Releases con reintentos, errores HTTP visibles y verificación de un SHA-256 fijado. Se conserva Trivy Action 0.36.0, actualizada previamente, y se omite su instalador automático.
- Acciones oficiales actualizadas a sus releases vigentes, conservando referencias por SHA completo: checkout 7.0.1, setup-go 7.0.0, setup-node 7.1.0, upload-artifact 7.0.2, CodeQL 4.38.3, Buildx 4.4.1, metadata 6.2.0, build-push 7.4.0, login 4.6.0 y cosign-installer 4.1.2.
- Go 1.27.2 y Node LTS 24.22.0 alineados entre CI y Docker mediante `go.mod`, `.node-version` y argumentos de construcción.
- Imágenes de compilación en Alpine 3.24 y ejecución en Distroless static Debian 13 nonroot; puerto declarado corregido a 8080.
- Cosign fijado a 3.1.3 en CI y en la guía de instalación del VPS. La actualización del ejecutable del VPS requiere aplicar allí las instrucciones documentadas.
- Dependencias Go compatibles actualizadas: `golang.org/x/crypto` 0.31.0 → 0.58.0 y `github.com/golang-jwt/jwt/v5` 5.2.1 → 5.3.1; versiones y sumas registradas.
- Lockfile del frontend sincronizado con la versión 0.2.0 del paquete.
- Contexto Docker excluye claves y datos locales, herramientas, binarios, archivos de entorno y artefactos. Scripts Bash conservan LF al clonar en Windows.
- Prueba de respaldo compatible con los permisos de Windows, manteniendo la comprobación POSIX 0600 en Linux y cerrando la base también cuando falla una aserción.
- Documentación de despliegue actualizada y referencias corregidas a `deploy/README.md`.

## Sin publicar — fases 6 y 7 (14 oct 2026)

### Fase 6: correos bancarios sin Gmail
- Lectores de notificaciones de Banco Pichincha, Produbanco y Diners/Titanium (`internal/mailparse`), con normalizador de montos, fechas en español y comercios, y clasificación de pagos, avisos, transferencias y avances.
- Importación (`internal/importer`): cuenta por banco + últimos dígitos (se crea si no existe) y categoría por reglas del usuario ▸ diccionario de comercios ▸ "Otros". Todo entra "Por revisar".
- **Movimientos → Importar correo** (.eml), confirmar con "usar siempre esta categoría", reglas visibles en Categorías.
- `pulse parse [--texto] [--titular "NOMBRE"] correo.eml` para probar correos reales sin base de datos (el texto sale anonimizado).
- Migración `0005_importacion` (origen `email`, reglas de comercio cifradas).

### Fase 7: Gmail en vivo
- Conexión OAuth con PKCE y `state` firmado atado a la sesión; exige confirmar identidad. Google vuelve a `/ajustes/gmail`.
- Sincronización cada 15 min (configurable) y, con Pub/Sub, en tiempo real vía `POST /api/gmail/webhook` con JWT de Google verificado.
- La búsqueda en Gmail está filtrada por remitente bancario: Pulse no lista ni descarga otros correos y no guarda ninguno.
- Modo sombra (por defecto), Gemini opcional para plantillas desconocidas (con step-up y texto redactado), reintentos con espera exponencial y conexión "sin acceso" con aviso cuando Google revoca el permiso.
- Push: "Nuevo gasto por revisar", alertas de presupuesto también para gastos de Gmail y avisos de seguridad de Gmail/Gemini (integrados en el servicio push existente).
- **Ajustes → Correo del banco (Gmail)**: conectar, estado, sincronizar ahora, modo sombra, Gemini, desconectar (revoca en Google).
- Migración `0006_gmail`; configuración `PULSE_GOOGLE_*`, `PULSE_GMAIL_*`, `PULSE_GEMINI_*`; guía `deploy/GMAIL.md`.
- Pruebas: Google falso en Go (`internal/gmail/gmailtest`) y en Node para E2E (`web/e2e/fake-google.mjs`); fuzzing de JWT y `state`.

### Verificación
- `go vet` y `go test -race ./...`: todo pasa.
- Vitest: 41 pruebas. Playwright: 36 escenarios (escritorio y móvil), sin omitidos.

## Sin publicar — 2026-10-14

- Paleta principal reemplazada por terracota y crema; modo oscuro con fondos café y acentos cobre claro.
- Favicon, iconos instalables y colores de la PWA actualizados para coincidir con la nueva paleta.
- Casillas de selección y color inicial de las cuentas nuevas alineados con el acento terracota.
- Control de animaciones trasladado de la cabecera a **Ajustes → Apariencia → Animaciones**. Mantiene la preferencia del dispositivo y respeta la reducción de movimiento del sistema.
- Generador de iconos compatible con rutas de Windows; los iconos adaptativos toman el color del SVG principal.

## 0.2.0 — 2026-10-14

`0.2.0` es la versión declarada actualmente en `web/package.json`.

### Cambios de versión y configuración detectados

- **Frontend:** versión `0.1.0` → `0.2.0` en `web/package.json`.
- **Go:** requisito `1.24` → `1.27` en `go.mod` y `web/go.mod`. El segundo archivo sigue siendo un módulo auxiliar para excluir `web/node_modules` del recorrido de las pruebas Go.
- **Node en Docker:** imagen de compilación `node:22-alpine` → `node:24-alpine`.
- **Requisitos documentados:** README actualizado de Go 1.24+/Node 22 a Go 1.27+/Node 24.
- **Puerto declarado en Docker:** `EXPOSE 8080` → `EXPOSE 18080`. Esta instrucción describe el puerto de la imagen; por sí sola no cambia dónde escucha el servidor.

### Interfaz y experiencia de uso implementadas en esta conversación

- Inicio reorganizado para aprovechar el ancho del escritorio: resumen del gasto, gráfico diario, presupuesto, indicadores, últimos movimientos y distribución por categoría.
- Barra lateral con grupos de navegación, acceso directo a cuentas y categorías, sus contadores, resumen mensual y perfil.
- Estados vacíos con acciones para registrar el primer gasto y configurar cuentas, categorías y presupuestos, sin inventar actividad financiera.
- Ajustes distribuidos en dos columnas en pantallas grandes; adaptación de Inicio a móvil y a los temas claro y oscuro.
- Logo con doble latido, onda de pulso decorativa, onda suave en el marcador de hoy y transiciones en páginas y botones.
- Control para pausar las animaciones, con preferencia persistente y respeto a `prefers-reduced-motion`.
- Corrección del trazo del gráfico para que se vea completo después de su animación de entrada.
- Corrección de la consulta de cuentas de la barra lateral para evitar una lista desactualizada en el formulario de gastos.

### Notificaciones: fase 5 implementada en esta conversación

- Suscripciones Web Push por dispositivo y generación de claves VAPID mediante `pulse vapid`.
- Avisos de presupuesto y seguridad, y resúmenes semanales y mensuales.
- Preferencias de privacidad de montos y horario silencioso.
- Cola cifrada con deduplicación, reintentos y eliminación de suscripciones inválidas.
- Integración con la sesión: cerrar, revocar o expirar una sesión impide nuevos envíos; el service worker verifica la identidad antes de mostrar un aviso.
- Controles de notificaciones en Ajustes y documentación de despliegue en `deploy/PUSH.md`.

### Desarrollo y verificación

- Arranque local en Windows mediante `scripts/dev.ps1`, con preparación de claves y uso de Zig como compilador C para SQLite/CGO cuando está disponible.
- Pruebas E2E portátiles con puerto y carpeta de datos propios por ejecución; cierre exclusivo del servidor de pruebas.
- Nuevas comprobaciones visuales y funcionales de escritorio/móvil, temas, pausa persistente, movimiento reducido, accesibilidad y ausencia de desbordamiento horizontal.
- Capturas de referencia en `artifacts/ui/`, excluidas del control de versiones mediante `.gitignore`.
- Resultados de las comprobaciones realizadas documentados en `VERIFICATION.md`: compilación correcta, 39 pruebas unitarias del frontend y 30 escenarios E2E verificados entre la suite general y las ejecuciones focalizadas. Las pruebas del rediseño no utilizaron la base de datos personal.

### Observaciones pendientes detectadas al registrar los cambios

- `web/package-lock.json` todavía declara `0.1.0`, mientras `web/package.json` declara `0.2.0`.
- Docker y README usan Node 24, pero la CI sigue configurada con Node 22.
- Docker declara el puerto 18080, mientras el servidor usa 8080 por defecto y Caddy apunta a `pulse:8080`. Conviene confirmar el puerto deseado y alinear la configuración.
- Los parsers bancarios y la conexión con Gmail (fases 6–7) siguen pendientes.
- La entrega de notificaciones a un teléfono físico sigue requiriendo una comprobación con el proveedor y dispositivo reales.

Estas observaciones se registran sin modificar las versiones ni la configuración elegidas.

## 0.1.0 — Base de referencia

La copia `pulse-fases-0-4.tar.gz` contiene las fases 0–4: identidad por invitación y MFA, cuentas y categorías, registro de gastos, frontend PWA y plan mensual con cálculos de umbrales y proyección. Declara frontend 0.1.0, Go 1.24 y Node 22 para compilar el frontend en Docker.
