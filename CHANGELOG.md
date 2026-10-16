# Registro de cambios de Pulse

Cambios relevantes de Pulse. Las entradas «Sin publicar» describen cambios posteriores a la versión declarada `0.2.0`; sus fechas corresponden al registro del trabajo y no a una publicación o etiqueta de GitHub. Las secciones históricas conservan el contexto de cada etapa.

La guía de uso y desarrollo está en [`README.md`](README.md); el roadmap y las aceptaciones pendientes, en [`PLAN.md`](PLAN.md).

## Sin publicar — lectura de Produbanco y recuperación de correos pendientes (15 oct 2026)

- Corregidas las fechas mes/día/año con un día mayor que 12: `10/15/2026` ahora se interpreta como 15 de octubre. La resolución por cercanía al envío se limita a las fechas numéricas ambiguas de Produbanco, sin intercambiar fechas ISO ni meses escritos.
- Promociones con descuentos, ofertas de crédito y recordatorios comerciales, y acuses como **Agradecemos su pago**, se clasifican como mensajes que no son gastos. No generan alertas de formato ni llamadas a Gemini. Los asuntos transaccionales tienen prioridad para conservar los consumos que requieran revisión.
- Las falsas alarmas antiguas de promociones y pagos se filtran antes de limitar la vista previa a 20 elementos. Ajustes muestra el total de pendientes y aclara que la fecha corresponde a la detección.
- Nuevo botón **Reintentar correos pendientes**: revisa hasta 90 días y vuelve a analizar únicamente los fallos entre los mensajes ya vistos, manteniendo las huellas de deduplicación y el aislamiento por usuario. Las alertas resueltas desaparecen; las que siguen fallando se conservan.
- Mensaje de conexión de Gmail alineado con la búsqueda inicial de 90 días. Cuando una lectura agota el tiempo de espera, el aviso indica que se puede reintentar conservando los gastos registrados.
- Comprobación directa de los tres `.eml` facilitados: consumos de USD 1,97 y USD 20,00 del 15 de octubre y USD 20,00 del 9 de octubre leídos correctamente. Nuevos casos de referencia derivados del HTML real, con destinatario, tarjeta y comercios anonimizados.
- README ampliado con el procedimiento de recuperación, la diferencia entre sincronizar y reintentar, los límites de búsqueda y de operaciones, y el diagnóstico de archivos `.eml` con `pulse parse`.
- Verificación: compilación del frontend, 42 pruebas Vitest, pruebas Go de mailparse/Gmail/importer/store/API en ejecución secuencial y `go vet` correctos. Los 8 escenarios E2E de Gmail quedaron verificados entre la ejecución completa (6 correctos) y la repetición de los dos flujos de conexión/reintento en escritorio y móvil. La ejecución local con `-race` quedó limitada por ThreadSanitizer en Windows (error de reserva de memoria); CI lo ejecuta en Linux.

## Sin publicar — rediseño visual de todas las vistas (15 oct 2026)

- Nueva paleta de azul petróleo, azul grisáceo, salvia y lavanda con superficies suaves, modo oscuro azul profundo y contraste ajustado para textos pequeños.
- Navegación lateral oscura, cabeceras y controles renovados. La barra móvil incluye Reportes y el botón Nuevo gasto pasa a la cabecera.
- Inicio con indicadores de colores suaves; Movimientos con filtros agrupados; cuentas, categorías y presupuestos en tarjetas; Ajustes y Mi cuenta organizados en secciones distribuidas en columnas en escritorio y apiladas en móvil.
- Reportes, tablas, gráficas, formularios y diálogos comparten la nueva apariencia. Se conserva el color elegido por el usuario para las cuentas y categorías, con menor saturación en su presentación.
- Portada y acceso con una ilustración de ejemplo compartida, y páginas legales, iconos instalables y color del navegador actualizados con la misma identidad.
- Los avisos de éxito de Gmail esperan a que se actualice el estado de la conexión y su contador de importados, para presentar un resultado consistente al terminar la operación.
- Verificación: compilación, TypeScript y 41 pruebas Vitest correctos; capturas y accesibilidad de todas las vistas en escritorio/móvil y claro/oscuro. La suite completa pasó 57/62 casos inicialmente; los cinco restantes quedaron verificados en repeticiones selectivas, con navegación real en la revisión visual y esperas explícitas de las respuestas de OAuth en Gmail.

## Sin publicar — estadísticas y proyecciones en Reportes (15 oct 2026)

- Indicadores de gasto neto, promedio diario, compra promedio y variación entre meses cerrados; detalle del número de compras y las devoluciones.
- Gráfica de líneas del total y una categoría seleccionable, con selector de valores exactos por mes, desplazamiento accesible con teclado y tabla alternativa. Las selecciones se conservan al cambiar entre 3, 6 y 12 meses. El mes en curso se identifica como parcial y los movimientos futuros quedan fuera de los indicadores.
- Proyecciones por categoría principal y total para la próxima semana calendario o el próximo mes, con gráfica acumulada punteada, tabla y explicación de la base de cálculo. Se usan hasta 90 días completos, con mínimo de 7 y aviso de historial limitado antes de 28.
- Agregación que respeta la zona horaria, suma subcategorías, descuenta devoluciones y mantiene el total proyectado igual a la suma de las categorías. Consulta de analítica sin descifrar comercios ni descripciones.
- La base de las proyecciones es independiente del periodo elegido en Reportes. Las categorías con neto negativo se proyectan en cero y el total suma sus estimaciones redondeadas. Sin migraciones adicionales de base de datos.
- Verificación: compilación del frontend, 41 pruebas Vitest, pruebas y `go vet` de API/store, y 6 E2E de Reportes en escritorio/móvil correctos. Incluyen accesibilidad en claro/oscuro, historial insuficiente, cambios de periodo, proyecciones, CSV y conciliación. Capturas revisadas en escritorio y móvil.

## Sin publicar — correos de Produbanco y cuentas duplicadas (15 oct 2026)

- Normalización del HTML de correos: los saltos del código fuente se tratan como espacios antes de separar etiquetas, para leer importes como `USD` y `20.00` aunque estén en líneas distintas.
- Fechas numéricas ambiguas resueltas con la interpretación más cercana al envío del correo. Nuevos casos de referencia de Produbanco cubren mes/día, HTML con saltos, quoted-printable y base64.
- El importador reutiliza una cuenta sin dígitos de la misma institución cuando el correo no incluye sufijo de tarjeta, prefiriendo una cuenta activa. Evita crear otra cuenta por cada notificación sin dígitos.
- Migración `0010_unir_cuentas_repetidas`: reúne cuentas sin dígitos con usuario, institución, nombre y tipo iguales, conserva preferentemente la activa más antigua y reasigna movimientos, gastos fijos y conciliación. Conserva un total por mes en caso de conflicto; no toca cuentas sin institución ni cuentas con sufijo. La unión requiere restaurar un respaldo para deshacerse.
- Pruebas de selección de cuentas y migración con conservación de movimientos, totales y claves foráneas. README actualizado con el comportamiento de importación y el alcance de la migración.
- Verificación: formato de los archivos Go modificados, `go vet` y todos los paquetes Go con detector de carreras correctos, incluidos los nuevos archivos de referencia del parser y la migración de cuentas repetidas.

## Sin publicar — reportes, gastos fijos y mejoras de Gmail (15 oct 2026)

- Nueva página **Reportes** (`/reportes`) con evolución de 3, 6 o 12 meses, distribución por categoría y cuenta, tabla alternativa al gráfico y exportación CSV del periodo. La exportación exige confirmar identidad, queda auditada y protege los textos frente a fórmulas de hojas de cálculo.
- Conciliación por cuenta y mes: permite introducir el total del estado de cuenta y compararlo con los movimientos registrados. Usa meses calendario en la zona horaria del usuario, no ciclos de corte de tarjetas.
- Gastos fijos mensuales desde **Nuevo gasto → Repetir cada mes**, listado y eliminación en **Plan**. Registro de vencidos al arrancar y cada hora, ajuste al último día de meses cortos y deduplicación de cada cobro.
- Nuevos gastos manuales sin conexión: cola temporal en IndexedDB, reenvío al recuperar la red o abrir la app y creación idempotente por identificador del cliente. La cola se limpia al cerrar sesión; ediciones y gastos fijos siguen requiriendo red.
- Métricas de revisión de Gmail por banco: al confirmar se distingue entre lectura correcta y corrección de monto, día, tipo o cuenta. Con el modo sombra apagado, una lectura con suficiente confianza y categoría del diccionario puede confirmarse sola desde 20 revisiones y 98 % de aciertos.
- Detector de plantillas bancarias no reconocidas: guarda asunto cifrado y huella, muestra avisos de los últimos 30 días en Ajustes → Gmail y envía un aviso a los administradores por banco y día sin incluir el asunto.
- Primera sincronización y retención de huellas de Gmail ampliadas de 60 a 90 días, manteniendo el límite de 1.000 mensajes por sincronización. Políticas públicas en ambos idiomas actualizadas con el plazo de 90 días, el almacenamiento temporal de gastos sin conexión y los asuntos cifrados de formatos no reconocidos.
- Migración `0009_reportes_recurrentes` para revisiones, avisos de plantillas, conciliación y gastos recurrentes; nuevos endpoints y pruebas de aislamiento, exportación, repetición, idempotencia y precisión.
- CI incorpora OWASP ZAP baseline contra el contenedor con bloqueo de alertas medias/altas. README y plan actualizados con las funciones de las fases 8–9 y las aceptaciones manuales pendientes.
- Verificación: compilación, 41 pruebas Vitest, formato Go, `go vet`, todos los paquetes Go con detector de carreras y validación del workflow con `actionlint` correctos. En E2E pasaron 55 de 56 en la suite completa; el escenario de invitación inválida agotó su espera inicial y pasó al repetirlo de forma aislada. ZAP contra Docker queda para la ejecución en CI.

## Sin publicar — Inicio, Mi cuenta y sincronización de Gmail (15 oct 2026)

- Inicio concentra el gasto y el presupuesto en una tarjeta: barra de uso, saldo disponible o exceso, estado del plan, referencia del día del mes, promedio diario, cierre estimado y movimientos por revisar. Se retiran el trazo diario y el anillo de presupuesto anteriores, junto con el componente `PulseTrace`.
- Nueva ruta `/ajustes/cuenta` para sesiones, contraseña, passkeys/TOTP, códigos de recuperación, actividad de seguridad y cierre de sesión. El perfil lateral y el avatar de la cabecera abren **Mi cuenta**; **Ajustes** conserva apariencia, Gmail, notificaciones y privacidad.
- Estilos del panel y listas de preferencias simplificados; pruebas de acceso, passkeys, push, PWA y accesibilidad adaptadas a la separación de Mi cuenta y Ajustes.
- Primera sincronización de Gmail ampliada de 7 a 60 días y de 5 a 10 páginas (hasta 1.000 mensajes por sincronización), alineada con la retención de huellas de deduplicación. Actualizados el mensaje de conexión, el ejemplo de la política en inglés, el plan y la prueba de sincronización.
- README puesto al día con la landing y `/entrar`, navegación, Mi cuenta, configuración de Google por un admin, alcance de la fase 8, respaldos externos y comprobaciones de salud. Corregido el alcance de `make check`: el fuzzing se ejecuta por separado.
- Verificación: compilación del frontend, 41 pruebas Vitest, 52 E2E de escritorio/móvil, `go vet` y todos los paquetes Go con detector de carreras correctos.

## Sin publicar — fase 8 y Gmail desde Ajustes (15 oct 2026)

- **Gmail se configura desde la app**: un admin pega en Ajustes → Gmail → *Configuración del servidor* el ID y el secreto de OAuth (y, opcionalmente, el tema/cuenta de Pub/Sub y la clave de Gemini). Se guardan cifrados con una subclave de la KEK (migración `0008_ajustes_servidor`), se aplican en caliente sin reiniciar y nunca se devuelven. La pantalla muestra la URI de redirección y el endpoint del webhook a copiar en Google Cloud. Exige rol admin y step-up; queda en la actividad de seguridad. Las variables de entorno siguen funcionando y lo guardado en Ajustes tiene prioridad; *Borrar* vuelve a ellas.
- `/healthz` responde 503 si el esquema de la base no coincide con el binario e informa si Gmail y push están activos.
- `deploy/backup-offsite.sh`: respaldo en caliente cifrado con `age` y subido a Nextcloud por WebDAV, sin dejar copias en claro; guía de restauración en una máquina limpia.
- CI: restaura el respaldo en una base vacía, comprueba que conserva los datos y que el contenedor arranca sano.
- Pruebas: configuración por admin (permisos, step-up, validación, cifrado en reposo, conservación del secreto, persistencia tras reinicio, vuelta a variables de entorno), reemplazo en caliente de la sincronización y `/healthz` con esquema atrasado; E2E de admin y de usuario sin rol admin en escritorio y móvil.

## Sin publicar — landing y pulido visual (14 oct 2026)

- `/` sin sesión es una landing pública (qué es Pulse, cómo funciona, importación desde el banco, privacidad); el acceso pasa a `/entrar`. Con sesión, `/` sigue siendo el panel. Cerrar sesión lleva a `/entrar`.
- Revisión contra las Web Interface Guidelines: diálogo de confirmación propio en lugar de `confirm()` (foco inicial en "Cancelar"), hojas que se abren y cierran con transición y sin arrastrar el scroll de fondo, avisos con entrada suave, `autoFocus` solo con ratón, emails y códigos sin corrector ni mayúsculas automáticas, placeholders con "…", texto mínimo legible de 0.72rem, `text-wrap: balance` en títulos, estados hover/active coherentes y `theme-color` que sigue al tema elegido.
- Sistema de movimiento: curvas y duraciones comunes (`--ease-out`, `--t-fast/base/slow`) y un único ritmo de latido (`--beat`). El logo late una vez al cargar y al pasar el cursor; los trazos se dibujan una vez; con movimiento reducido o "Animaciones" en pausa no se anima nada.
- Etiquetas en minúsculas en lugar de MAYÚSCULAS espaciadas; Ajustes ordena Gmail y Privacidad en la rejilla de escritorio.
- Páginas públicas estáticas con la misma tipografía (fuentes servidas desde `/fonts`) y los mismos tokens que la app.

## Sin publicar — páginas bilingües y primera cuenta (14 oct 2026)

- Presentación pública de Pulse en `/acerca` y `/about`, con funciones, acceso por invitación y explicación del uso opcional de Gmail.
- Política de privacidad y términos en inglés (`/privacy` y `/terms`), con navegación entre idiomas y enlaces a las páginas en español. Textos españoles reorganizados y contacto dirigido a quien administra la instancia.
- Enlace **Qué es Pulse** desde la pantalla de acceso y estilos compartidos para la navegación, las acciones y la presentación pública.
- Rutas públicas de ambos idiomas excluidas del reemplazo de navegación del service worker. Pruebas de HTML público, enlaces y accesibilidad ampliadas a las seis páginas.
- README actualizado con las rutas públicas y el procedimiento para crear la primera cuenta administradora: dónde generar y copiar el código `PLS-`, vigencia, uso único, recuperación mediante una invitación nueva y diferencia entre el entorno local y el VPS.
- Verificación: compilación del frontend, 41 pruebas unitarias y 6 E2E de páginas públicas en escritorio/móvil correctos; incluyen accesibilidad en ambos temas y navegación con el service worker activo.

## Sin publicar — dependencias y documentación (14 oct 2026)

- Dependencias directas del frontend actualizadas a las versiones estables actuales y lockfile regenerado: React/React DOM 19.3.1, React Router 7.18.4, SimpleWebAuthn 14.0.0 y TanStack Query 5.104.2.
- Herramientas actualizadas: TypeScript 7.0.2, Vite 8.3.4, plugin React 6.1.2, plugin PWA 2.0.0, Vitest 5.0.4, jsdom 30.1.2, Playwright 1.64.1 y axe 4.13.0; también se actualizan Testing Library, tipos y OTPAuth.
- Configuración de pruebas adaptada a `vitest/config` y eliminadas las opciones de transición de React Router 6, ya incorporadas en React Router 7.
- Dependencias directas Go comprobadas contra el registro oficial: ya estaban en sus versiones vigentes. Módulo sincronizado con `go mod tidy` y sumas verificadas.
- README ampliado con funciones actuales, estructura de Gmail/importación, desarrollo reproducible con `npm ci`, comandos de administración, páginas legales y flujo de despliegue. Changelog corregido para distinguir cambios actuales de observaciones históricas ya resueltas.
- Comprobación de permisos del test de exportación adaptada a Windows, que usa ACL; se mantiene la validación 0600 en sistemas POSIX.
- Verificación: instalación reproducible con `npm ci`, compilación del frontend, 41 pruebas Vitest, 42 E2E de escritorio/móvil, `go vet`, pruebas Go con detector de carreras y sumas de módulos correctas. `npm outdated` no reporta dependencias pendientes; `npm audit` pasa de 7 paquetes afectados a 0 vulnerabilidades, incluidas las dependencias de desarrollo. `govulncheck`: 0 vulnerabilidades alcanzables; detecta 1 en un módulo requerido cuyo código afectado no se invoca.

## Sin publicar — privacidad y términos (14 oct 2026)

- Páginas públicas `/privacidad` y `/terminos` con contenido HTML legible sin ejecutar JavaScript y temas claro/oscuro. Documentan el tratamiento de datos, los derechos previstos en la LOPDP de Ecuador y el RGPD, y la declaración de Uso Limitado para `gmail.readonly`. Enlazadas desde el acceso, el registro (aceptación), Ajustes y la conexión de Gmail.
- `pulse user export <email> <archivo.json>` (acceso y portabilidad) y `pulse user delete <email> --confirmar <email>` (supresión).
- Registro de seguridad: retención de 12 meses con purga automática. Migración `0007_retencion_auditoria`: el registro sigue siendo inalterable salvo para borrar filas vencidas.
- El service worker ya no sustituye `/privacidad` y `/terminos` por la app.

## Sin publicar — CI y despliegue (14 oct 2026)

- Validación de `VPS_SSH_KEY` con OpenSSH antes de conectarse, sin imprimir la clave; mensaje explícito si el secret no contiene una privada completa y sin contraseña. Guía de Windows para comprobar y copiar el archivo correcto.
- Preparación SSH del despliegue: clave ED25519 del secret asociada al host/puerto configurado, validación de campos y formato, rechazo de claves ambiguas y compatibilidad con saltos de línea de Windows. Se conserva la verificación estricta; la guía explica cómo verificar la huella y corregir los secrets de `production`.
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

### Interfaz y experiencia de uso

- Inicio reorganizado para aprovechar el ancho del escritorio: resumen del gasto, gráfico diario, presupuesto, indicadores, últimos movimientos y distribución por categoría.
- Barra lateral con grupos de navegación, acceso directo a cuentas y categorías, sus contadores, resumen mensual y perfil.
- Estados vacíos con acciones para registrar el primer gasto y configurar cuentas, categorías y presupuestos, sin inventar actividad financiera.
- Ajustes distribuidos en dos columnas en pantallas grandes; adaptación de Inicio a móvil y a los temas claro y oscuro.
- Logo con doble latido, onda de pulso decorativa, onda suave en el marcador de hoy y transiciones en páginas y botones.
- Control para pausar las animaciones, con preferencia persistente y respeto a `prefers-reduced-motion`.
- Corrección del trazo del gráfico para que se vea completo después de su animación de entrada.
- Corrección de la consulta de cuentas de la barra lateral para evitar una lista desactualizada en el formulario de gastos.

### Notificaciones: fase 5

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

### Observaciones históricas de esta etapa

- La versión del lockfile, Node en CI y el puerto de Docker presentaban discrepancias; quedaron resueltas en la entrada «CI y despliegue».
- Los parsers bancarios y Gmail estaban pendientes; quedaron implementados en las fases 6–7 descritas arriba.

La entrega de notificaciones a dispositivos físicos sigue siendo una comprobación manual independiente de los E2E.

## 0.1.0 — Base de referencia

La copia `pulse-fases-0-4.tar.gz` contiene las fases 0–4: identidad por invitación y MFA, cuentas y categorías, registro de gastos, frontend PWA y plan mensual con cálculos de umbrales y proyección. Declara frontend 0.1.0, Go 1.24 y Node 22 para compilar el frontend en Docker.
