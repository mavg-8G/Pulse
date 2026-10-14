# Verificación de la fase 5 — 14 de octubre de 2026

Implementado: Web Push con VAPID, suscripciones por dispositivo, preferencias, alertas de presupuesto, resúmenes semanales/mensuales, avisos de seguridad, privacidad de montos y horario silencioso. La cola y las suscripciones se guardan cifradas y sobreviven a reinicios; hay reintentos y limpieza de suscripciones caducadas.

## Resultados

| Comprobación | Resultado |
|---|---|
| Compilación del binario Go y frontend (`tsc` + Vite) | Correcta |
| Pruebas Go sobre SQLite real, con `-race` | Todos los paquetes pasan |
| `go vet ./...` | Correcto |
| Vitest | 39 pruebas pasan |
| Playwright, escritorio y móvil | 28 pruebas pasan, sin pruebas omitidas |
| `govulncheck` | 0 vulnerabilidades alcanzables por el código |
| `npm audit --omit=dev --audit-level=high` | Pasa el umbral; 0 alertas altas/críticas |
| Prueba de humo del ejecutable final | `/healthz`, frontend y script push: 200; `/api/me` sin sesión: 401; CSP presente |

Las pruebas de push verifican cifrado Web Push mediante descifrado en un receptor HTTP de prueba, respuestas 201/404/410/429/500, espera exponencial, `Retry-After`, privacidad al entregar, silencio entre días, deduplicación tras reinicios, concurrencia, aislamiento, resúmenes, revocación y expiración de sesiones. Un test crea un gasto real por API y verifica el push al cruzar el 80 %.

Playwright sustituye el proveedor externo y la interfaz de notificaciones del sistema. Se ejecutan el listener del service worker, la consulta de identidad y la navegación reales, y se comprueba que una entrega de otra cuenta o posterior al logout no se muestra. Chromium headless en esta máquina no permite mostrar notificaciones nativas del sistema.

## Entorno Windows

Se utilizó Go 1.27.2 y un compilador C portátil Zig 0.15.2 en `.tools/` para CGO/SQLite. npm utiliza los certificados de confianza de Windows mediante `NODE_OPTIONS=--use-system-ca`, sin desactivar TLS.

El detector de carreras requirió `CGO_LDFLAGS=-lapi-ms-win-core-synch-l1-2-0` y el ejecutor local `.tools/race-exec.mjs`, que quita ASLR únicamente del ejecutable temporal de prueba para que ThreadSanitizer funcione con Zig en Windows. El ejecutable de aplicación conserva sus opciones normales de seguridad. La CI de Linux usa `go test -race` directamente.

El runner E2E ahora funciona en Windows y limpia sus propios procesos al terminar. Los datos de pruebas se aíslan en `web/.e2e/`; el entorno local se prepara en `.dev/`.

## Uso y comprobaciones pendientes

Ejecutable compilado: `bin/pulse.exe`. Para arrancar el programa local, desde la raíz del proyecto:

```powershell
./scripts/dev.ps1
```

El script compila ambas partes, genera las claves si faltan, muestra una invitación y sirve la aplicación en `http://localhost:8080`.

Para producción, sigue `deploy/PUSH.md`: configura VAPID, despliega sobre HTTPS, suscribe un dispositivo y comprueba la recepción física del aviso del 80 %. Esa comprobación no se ha realizado desde esta máquina. No se ejecutó Docker porque no está instalado/disponible en este entorno.

La auditoría conserva dos alertas moderadas de React Router 6. La actualización compatible aplicada resolvió las alertas altas; resolver las moderadas requiere una migración a React Router 7. La auditoría del módulo Go también registra vulnerabilidades en dependencias que el código no alcanza, según `govulncheck`.

## Rediseño visual — 14 de octubre de 2026

Inicio usa el ancho del escritorio con un resumen y gráfico de gasto diario, estado del presupuesto, indicadores del mes, movimientos y categorías. Los estados sin gastos muestran acciones útiles y categorías existentes, sin inventar actividad. Cuentas y Categorías se pueden abrir directamente desde la barra lateral, que también muestra el gasto del mes y el acceso a la cuenta. Ajustes aprovecha dos columnas en pantallas grandes.

El logo tiene un doble latido, el estado vacío muestra un trazo de pulso animado y el marcador de hoy tiene una onda suave. Hay transiciones de entrada y respuesta al pulsar botones. El control superior permite pausar el movimiento y conserva la preferencia del dispositivo; `prefers-reduced-motion` desactiva las animaciones.

Validación: compilación de TypeScript/Vite correcta, 39 pruebas unitarias y 30 escenarios E2E comprobados en escritorio y móvil. La suite general verificó 28 escenarios; después de corregir la consulta de cuentas de la barra lateral, los seis escenarios focalizados de gastos e interfaz pasaron, incluyendo los dos flujos de gastos que faltaban. Se verificaron registro/edición/borrado, navegación, MFA, presupuestos, PWA, push, pausa persistente, movimiento reducido, ausencia de desbordamiento horizontal y accesibilidad en claro/oscuro. Cada ejecución E2E usa un puerto libre y una carpeta exclusiva, y cierra solamente su propio servidor.

Capturas con cuentas y gastos ficticios de prueba:

- `artifacts/ui/inicio-vacio-escritorio.png`
- `artifacts/ui/inicio-con-gastos-escritorio.png`
- `artifacts/ui/inicio-oscuro-escritorio.png`
- `artifacts/ui/inicio-vacio-movil.png`
- `artifacts/ui/inicio-con-gastos-movil.png`
- `artifacts/ui/inicio-oscuro-movil.png`

## Paleta cálida y control en Ajustes — 14 de octubre de 2026

Se reemplazó el acento azul/violeta por terracota, con superficies crema en claro y café en oscuro. Los colores del manifiesto, favicon, iconos instalables, controles nativos y color inicial de cuentas nuevas siguen la nueva paleta. El control de animaciones ahora está en Ajustes → Apariencia; conserva la pausa al navegar y recargar.

Compilación TypeScript/Vite y del ejecutable Go correctas. Los dos escenarios focalizados de interfaz pasaron (escritorio y móvil), incluyendo accesibilidad en Inicio vacío/con gastos, modo oscuro y Ajustes, ausencia del control en Inicio, cambio de pausa desde Ajustes, persistencia y movimiento reducido. Las capturas de Ajustes están en `artifacts/ui/ajustes-apariencia-escritorio.png` y `artifacts/ui/ajustes-apariencia-movil.png`.

## CI y versiones del despliegue — 14 de octubre de 2026

- `actionlint` 1.7.12: workflow válido. Sintaxis Bash de `scripts/install-trivy.sh` y `deploy/deploy.sh` correcta; `git diff --check` correcto.
- SHA completos de las once acciones distintas comprobados contra su metadata oficial. Versiones e imágenes oficiales disponibles verificadas: Go 1.27.2, Node LTS 24.22.0, Alpine 3.24, Distroless static Debian 13, Trivy 0.76.0 y Cosign 3.1.3.
- Descarga real del archivo Linux x86_64 de Trivy 0.76.0 verificada contra el SHA-256 fijado en el workflow; el archivo contiene el ejecutable esperado. El instalador evita el script mutable y añade reintentos y errores HTTP visibles.
- `go vet ./...` y `go test ./...`: todos los paquetes pasan con Go 1.27.2, `x/crypto` 0.58.0 y JWT 5.3.1. La prueba de respaldo comprueba 0600 en sistemas POSIX; Windows usa ACL.
- `govulncheck` 1.9.0: cero vulnerabilidades alcanzables y cero en paquetes importados. Informa GO-2026-5932 en `x/crypto/openpgp`, que Pulse no importa; no tiene versión corregida.
- TypeScript/Vite: compilación correcta. Vitest: 41 pruebas pasan con `--pool=forks --maxWorkers=1 --minWorkers=1`; el primer intento tuvo un fallo local al cerrar workers. Node local: 24.21.0; la versión fijada para CI y Docker es 24.22.0.
- `npm audit --omit=dev`: cero alertas altas/críticas; dos moderadas de React Router requieren migración a versión 7.

La primera comprobación Go coincidió con la regeneración de archivos embebidos; se repitió después de terminar el frontend. También se repitió la ejecución tras un bloqueo temporal de Windows al borrar un ejecutable de prueba. Los resultados anteriores corresponden a las ejecuciones finales correctas.

No se construyó ni escaneó la imagen completa localmente porque Docker no está disponible. No se ejecutó el workflow remoto, no se publicó una imagen y no se actualizó Cosign en el VPS. La guía indica actualizar el VPS a Cosign 3.1.3 antes de desplegar con el nuevo workflow.
