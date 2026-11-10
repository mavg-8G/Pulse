# Revisión y plan intensivo de seguridad de Pulse

Fecha: 2026-11-10. Revisión base: `69b933da1a260f10550496bbb264f6758b51ffce`.

Seguimiento: las correcciones posteriores se registran en [SEGURIDAD-CORRECCIONES.md](SEGURIDAD-CORRECCIONES.md), y su aplicación operativa en [HARDENING.md](../deploy/HARDENING.md). El resto de este documento conserva la revisión inicial.

Estado: propuesta basada en lectura de código y comprobaciones locales parciales; no constituye un pentest completo ni certificación. No se modificaron controles de la aplicación ni se hicieron pruebas contra producción. No hay una vulnerabilidad crítica remota demostrada en esta revisión; tampoco evidencia suficiente para afirmar su ausencia.

## 1. Objetivo y criterio de seguridad

Ningún proyecto puede garantizar ser impenetrable. El objetivo es prevenir accesos y modificaciones no autorizados, limitar el alcance de un compromiso, detectarlo y recuperar el servicio con evidencia verificable.

Usar OWASP ASVS 5.0 nivel 2 como matriz de requisitos aplicables, con controles de nivel 3 seleccionados para identidad, claves, aislamiento, administración y recuperación. Esto es una meta de verificación, no una declaración de cumplimiento. Cada requisito debe tener responsable, prueba, evidencia y estado; justificar explícitamente los no aplicables.

Prioridad P1: abordar primero por impacto o dependencia. P2: endurecimiento y cobertura. P3: mejora operativa continua. Estas prioridades no equivalen a una puntuación CVSS: varios hallazgos requieren acceso previo o condiciones concretas.

## 2. Arquitectura actual y límites de confianza

```text
Navegador / PWA React
  ├─ Service worker: recursos estáticos; API por red
  └─ IndexedDB: datos y cola cifrados, CryptoKey persistida
          │ HTTPS, cookie de sesión
          ▼
Caddy ── red Docker externa «proxy» ──► monolito Go :8080
                                         ├─ API, autenticación y autorización
                                         ├─ WebAuthn / CBOR / COSE propios
                                         ├─ tareas Gmail, push, recurrentes y backups
                                         ├─ SQLite: datos de todos los usuarios
                                         └─ KEK de archivo → DEK por usuario
                                               │ conexiones salientes
                          Google OAuth/Gmail/PubSub, Gemini, push, destinos backup

GitHub Actions → construir/probar/escanear/firmar → GHCR
        → SSH con comando forzado → deploy.sh → Docker en VPS
```

Activos: datos financieros, contraseñas derivadas, semillas TOTP, códigos de recuperación, cookies, claves WebAuthn, tokens OAuth, KEK/DEK, respaldos, firma/procedencia de releases y auditoría.

Adversarios: atacante anónimo, usuario invitado malicioso, persona con contraseña o sesión robada, remitente de correo falsificado, script del mismo origen comprometido, persona con acceso al perfil del navegador, contenedor vecino, cuenta CI/deploy comprometida y atacante con acceso a BD o VPS.

Mantener el monolito y SQLite inicialmente. La arquitectura pequeña reduce componentes y credenciales; dividir en microservicios o migrar de BD no corrige por sí mismo autorización, claves ni seguridad del navegador. La segregación de backups o gestión de claves se puede introducir por límites de privilegio concretos.

## 3. Controles existentes que conviene conservar

- Registro por invitación y estados de sesión parciales hasta completar MFA.
- Cookies `__Host-`, Secure, HttpOnly y SameSite Strict en HTTPS; rotación al completar MFA y reautenticación para operaciones sensibles.
- PBKDF2-SHA256 con 600.000 iteraciones, sal y pepper; bloqueo progresivo, límites de intentos y consumo atómico de pasos TOTP/códigos de recuperación.
- CSP sin scripts inline, protección de origen para escrituras, respuestas API sin caché, límites de cuerpo y tiempos HTTP.
- Consultas parametrizadas y filtrado por usuario en las rutas examinadas; claves foráneas compuestas en datos financieros y pruebas de aislamiento existentes.
- AES-GCM con DEK por usuario y KEK separada de SQLite; respaldos externos con age.
- Validación de destinos push y transporte con defensa frente a SSRF.
- Imagen final distroless sin root, raíz de solo lectura, capacidades eliminadas y límites de CPU, memoria y procesos.
- Acciones CI fijadas a commits, análisis Go, Trivy, fuzzing, E2E, firma de imágenes y verificación de firma en despliegue.

La presencia de estos controles en el repositorio no acredita su configuración efectiva en el VPS o en GitHub.

## 4. Hallazgos y acciones

| ID / prioridad | Evidencia y alcance | Acción propuesta y prueba de aceptación |
|---|---|---|
| S01 / P1 | `internal/passkey/passkey.go:140,149,199`: se pide `userVerification: preferred` y se exige UP, pero no UV. Una credencial sin PIN/biometría puede satisfacer también el step-up. En el login sigue existiendo la contraseña previa: no es un bypass completo de autenticación. | Exigir UV en operaciones de alta seguridad, especialmente step-up y administradores; solicitar `required` y verificar el bit UV en el servidor. Definir compatibilidad del segundo factor y recuperación antes del cambio. Pruebas con respuesta firmada UP=1/UV=0 rechazada donde se exige UV, UV=1 aceptada, challenge/origen/RP incorrectos y replay rechazados. |
| S02 / P1 | `web/src/lib/vault.ts:59,95,100` y `web/src/lib/auth.tsx:60`: la clave persistida se puede usar al reabrir el perfil; un fallo de red habilita vista offline de la última persona sin nuevo desbloqueo. Los siete días dependen de metadatos y reloj locales. | Hacer offline opt-in por dispositivo; ofrecer modo sin persistencia para equipos compartidos, bloqueo por inactividad y desbloqueo criptográfico real. Diseñar envoltura de clave con secreto local independiente o PRF WebAuthn donde se soporte; no basta una pantalla de PIN que deje la clave utilizable. Distinguir autorización del servidor de estado offline en frontend. Probar inicio offline, cambio de usuario, varias pestañas, expiración, revocación al reconectar y logout. |
| S03 / P1 | `internal/mailparse/eml.go`, `banks.go`, `internal/gmail/syncer.go`: la clasificación usa From; no se encontró validación de autenticidad del remitente antes de importar o autoconfirmar. Formato y confianza del parser no prueban origen bancario. | Separar autenticidad del correo de confianza de extracción. Definir cómo obtener/verificar autenticación de entrega confiable; no aceptar un `Authentication-Results` arbitrario insertado en un .eml. Si no hay evidencia confiable, mantener pendiente y no autoconfirmar. Pruebas con From falsificado, cabeceras fabricadas, reenvíos y duplicados. No atribuir al archivo manual garantías de entrega de Gmail. |
| S04 / P1 | `.github/workflows/ci.yml`: gosec usa `-no-fail` y no es dependencia de publicación; npm audit omite dev y no corre en el job nocturno; ZAP baseline examina páginas públicas sin sesión. | Clasificar resultados y bloquear hallazgos nuevos de alta confianza/impacto; analizar también herramientas de build y npm en la revisión periódica. DAST autenticado por rol en staging con datos sintéticos. Excepciones con motivo, responsable y caducidad. Probar que un fallo relevante impide publicar/desplegar. |
| S05 / P1 | `internal/store/audit.go:28`: se descarta el error del INSERT; el comentario promete registrarlo, pero no lo hace. El registro reside además en la misma BD. | Propagar o informar fallos con logging y métrica sin datos secretos; usar contexto acotado independiente para eventos críticos si se cancela la solicitud. Definir transacción/outbox para cambios críticos y exportación a destino independiente con acceso restringido. Inyectar fallo de persistencia y verificar aviso; acordar qué operaciones deben fallar si no hay evidencia durable. |
| S06 / P2 | `internal/crypto/crypto.go:168` usa AAD usuario+campo; `internal/store/transactions.go:76,109` reutiliza identificadores como `tx.notes` en todas las filas. El comentario de crypto promete impedir copia entre filas, pero el AAD actual no lo hace. Requiere capacidad de modificar almacenamiento. | Formato versionado con AAD usuario+entidad+ID+campo; migración compatible, por lotes y verificable. Prueba: copiar ciphertext al mismo campo de otra fila del mismo usuario debe fallar. Este control no protege importes/fechas en claro ni resuelve un VPS comprometido. |
| S07 / P1 diseño, P2 ejecución | `internal/crypto/crypto.go`: una KEK deriva wrap, lookup, pepper y ajustes. No se encontró mecanismo de rotación con identificadores de clave. Un proceso comprometido con acceso a claves puede descifrar campos. | Inventario y separación de propósitos; diseñar key IDs, rotación de KEK con reenvoltura de DEK y estrategia propia para lookup/pepper que no deje cuentas inaccesibles. Considerar KMS según el riesgo operativo. Ensayar pérdida, compromiso, rotación interrumpida y restauración; custodiar KEK e identidad age fuera del VPS con procedimiento probado. |
| S08 / P2 | `internal/auth/ratelimit.go` conserva buckets en memoria sin máximo explícito; `internal/api/h_security.go:55` verifica contraseña sin limitador específico. Límites por IP no acotan trabajo total ante muchas IP. | Límites globales de concurrencia/coste para hashes e importaciones, límites por usuario/operación y máximo de buckets. Presupuestos de filas, rango, bytes y tiempo para reportes/exportaciones. Medir latencia bajo carga sin agotar SQLite/256 MB y verificar recuperación. Evitar que la defensa permita bloquear cuentas trivialmente. |
| S09 / P2 | `deploy/docker-compose.yml` usa red externa compartida `proxy`; `deploy/README.md` agrega deploy al grupo docker, cuyo alcance privilegiado ya reconoce la guía. Solo se revisaron plantillas. | Verificar red real y proxies confiables; preferir red dedicada Caddy–Pulse, restringir SSH y salida por necesidad. Reducir privilegio de deploy mediante mecanismo root controlado o alternativa evaluada; el comando forzado es defensa existente, no eliminarlo. Probar acceso desde contenedor vecino y bloqueo de imagen no firmada. |
| S10 / P2 | `Dockerfile` usa tags para bases; ZAP también usa tag. CI prueba una construcción y vuelve a invocar el build para publicar desde caché. La firma garantiza identidad del workflow, no revisión del código. | Fijar digest de bases/herramientas con actualización controlada. Promover el mismo artefacto probado, vincular digest a pruebas/SBOM/procedencia y comprobarlo antes de desplegar. Revisar protección de main, CODEOWNERS para seguridad/workflows y permisos de production. Verificar estado real de esas protecciones: no se pudo inspeccionar aquí. |
| S11 / P2 | `deploy/deploy.sh`: el respaldo previo solo se crea si el contenedor está sano; los respaldos previos al despliegue son .db locales, distintos de los age externos. | Rechazar actualización de instalación existente no sana hasta diagnóstico o respaldo verificable; distinguir arranque inicial. Proteger y rotar respaldos locales, exigir copia externa recuperable antes de migraciones irreversibles y ensayar rollback sin perder escrituras posteriores. No presentar rollback como reversión inocua de datos. |

S01 admite una política de segundo factor de posesión en el login, pero esa decisión debe ser explícita; el step-up usado para cambiar factores o descargar backups necesita especial atención. Evaluar biblioteca WebAuthn mantenida frente al coste de auditar la implementación propia CBOR/COSE, sin introducir una migración apresurada.

El cifrado offline con CryptoKey no exportable limita la exportación por la API Web Crypto; no impide que un script del mismo origen la use ni garantiza confidencialidad frente a acceso al dispositivo. La protección frente a XSS y dependencias comprometidas sigue siendo esencial.

## 5. Ejecución por entregas

Orden recomendado; las duraciones se estimarán después de resolver política offline y operación de producción.

### Entrega A — Línea base y pruebas reproducibles

- [ ] Ejecutar Go con CGO y toolchain C en Linux/contenedor, race, tests frontend y E2E; archivar resultados por commit.
- [ ] Generar inventario de endpoints, roles (anónimo/enroll/mfa/full/admin), datos y operaciones sensibles.
- [ ] Crear matriz ASVS aplicable y modelo de amenazas de los flujos anteriores.
- [ ] Completar auditoría de dependencias, herramientas de build, imagen y secretos del historial con resultados redactados.
- [ ] Revisar configuración efectiva de VPS/Caddy, firewall, Docker, GitHub y destinos de backup mediante evidencia de solo lectura.

Salida: inventario, pruebas ejecutables y backlog de riesgos con responsable. Un escaneo que no ejecuta correctamente queda como desconocido, nunca como limpio.

### Entrega B — Identidad, abuso y trazabilidad

- [ ] S01: política UV y step-up robusto, con ruta de recuperación probada para usuarios existentes.
- [ ] S05: errores de auditoría observables y evidencia durable para operaciones críticas.
- [ ] S08: límites de coste/concurrencia y rate limiting de cambios de contraseña y otras rutas costosas.
- [ ] Matriz negativa completa para APIs: A no lee/modifica/borra/exporta recursos de B; usuario no admin no accede a funciones admin; sesiones parciales no acceden a finanzas.
- [ ] Casos de concurrencia para alta MFA, último factor, recovery, challenges y revocación; revisar contador WebAuthn con actualizaciones atómicas.

Salida: pruebas de abuso reproducibles y cero hallazgos de alta prioridad abiertos en estos flujos, o aceptación documentada del riesgo residual.

### Entrega C — Navegador, correo y confidencialidad

- [ ] S02: política offline y desbloqueo implementados sin pérdida silenciosa de cola; migración y limpieza entre pestañas.
- [ ] S03: autenticidad de origen y autoconfirmación segura; pruebas de correos falsificados.
- [ ] S06: cifrado ligado a registro, migración con backup y comprobación de recuperación.
- [ ] Revisar todo uso de HTML/URLs/DOM, CSP, SW y almacenamiento; explorar Trusted Types en modo reporte antes de exigirlo.
- [ ] Revisar redacción enviada a Gemini con corpus sintético adversarial, cuotas y límites; conservar salida como no confiable, sin autoconfirmación derivada de IA.

Salida: datos locales bloqueados según política, mensajes sin procedencia confiable pendientes y migración criptográfica reversible mediante respaldo validado.

### Entrega D — Releases, servidor y recuperación

- [ ] S04/S10: bloqueos efectivos de CI, DAST autenticado, análisis de secretos y publicación del artefacto probado por digest.
- [ ] S09: aislamiento de red, permisos mínimos y revisión de privilegios de deploy.
- [ ] S07: rotación ensayada con cuentas y respaldos de prueba antes de tocar claves reales.
- [ ] S11: actualizaciones con backup verificado y procedimiento ante instancia no sana.
- [ ] Restauración externa age en entorno limpio con descifrado de datos, login/MFA y consulta financiera; no basta `healthz`.
- [ ] Acordar y medir RPO/RTO; propuesta inicial para evaluar: pérdida máxima de 24 h y recuperación en 4 h. Reducir RPO si la tolerancia del usuario lo exige.

Salida: evidencia de recuperación y rechazo de releases no autorizadas, con alertas comprobadas.

### Entrega E — Validación independiente y operación

- [ ] Pentest autenticado en staging, por roles, con corpus y datos sintéticos; revalidar cada corrección.
- [ ] Simular robo de sesión, revocación OAuth, fallo de backup y pérdida/rotación de clave.
- [ ] Alertar ante fallos de auditoría, picos de autenticación, cambios MFA/admin, backup atrasado y escaneo periódico fallido.
- [ ] Definir SLA de remediación y revisión mensual de accesos/dependencias; simulacro de restauración trimestral como propuesta inicial.

## 6. Verificación realizada y límites

- Lectura selectiva de API, autenticación/MFA, passkeys, cifrado, almacenamiento financiero, esquema, importación/Gmail/Gemini, offline/SW, contenedor, CI y despliegue.
- `go test ./internal/auth ./internal/crypto ./internal/passkey ./internal/api ./internal/store ./internal/gmail ./internal/mailparse`: los paquetes auth, crypto, passkey y mailparse reportaron éxito. Integraciones de BD no pudieron ejecutarse correctamente por `CGO_ENABLED=0`; pruebas JWT con servidor local fallaron por restricciones de sockets. El proceso terminó con error y un problema adicional al limpiar un ejecutable temporal. No atribuir estos resultados a defectos funcionales de la app.
- `npm --prefix web audit --json`: no obtuvo resultados por `ENOTFOUND registry.npmjs.org`. No se han confirmado ni descartado CVE de estas dependencias.
- No se ejecutó en esta revisión la suite completa, race, E2E, fuzzing prolongado, gosec, govulncheck, Trivy ni DAST autenticado. La configuración de CI se leyó, no se comprobó su última ejecución.
- No se inspeccionaron VPS, claves reales, permisos efectivos de GitHub, configuración de Google ni políticas reales de almacenamiento. No se hizo escaneo activo de producción.

## 7. Referencias primarias

- OWASP ASVS 5.0: https://asvs.owasp.org/ y https://github.com/OWASP/ASVS/tree/v5.0.0
- W3C WebAuthn, verificación de usuario y banderas del autenticador: https://www.w3.org/TR/webauthn-3/
- OWASP HTML5 Security Cheat Sheet, almacenamiento local e IndexedDB: https://cheatsheetseries.owasp.org/cheatsheets/HTML5_Security_Cheat_Sheet.html

Primer lote recomendado: reproducir la línea base en entorno con CGO, reforzar step-up WebAuthn, corregir la pérdida silenciosa de auditoría y añadir pruebas negativas de autorización. Resolver en paralelo de planificación la política offline y la procedencia del correo, porque condicionan cambios visibles para el usuario.
