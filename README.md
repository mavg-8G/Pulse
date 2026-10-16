# Pulse

Registro de gastos personales: cuentas y tarjetas, categorías, plan de gastos mensual con avisos y proyección. PWA para escritorio y móvil, modo claro/oscuro. Multiusuario **solo por invitación**, con MFA y aislamiento de datos por usuario (ver [`PLAN.md`](PLAN.md) §4).

Estado: **fases 0–8 y 10–13 implementadas y fase 9 en curso**. Incluye el rediseño de todas las vistas, estadísticas y gráficas, proyección de fin de mes con regresión y selección automática del método, presupuestos flexibles, uso sin conexión con almacén cifrado, respaldo externo cifrado, CSV, conciliación, gastos fijos y métricas de revisión de Gmail. Quedan aceptaciones con datos y dispositivos reales, configuración de monitores y nuevos bancos; el roadmap está en [`PLAN.md`](PLAN.md). Los cambios se registran en [`CHANGELOG.md`](CHANGELOG.md).

## Funciones

- Cuentas, tarjetas, categorías y movimientos manuales o importados, con revisión de gastos y reglas por comercio.
- Plan mensual con proyección y umbrales de gasto; notificaciones Web Push, resúmenes y horario silencioso. Configuración en [`deploy/PUSH.md`](deploy/PUSH.md).
- Importación de correos `.eml` de Banco Pichincha, Produbanco y Diners/Titanium. Gmail opcional mediante OAuth, sincronización periódica o Pub/Sub y modo sombra; Gemini opcional para plantillas desconocidas. Reintento de correos pendientes tras actualizar un formato, sin duplicar los ya importados. Configuración en [`deploy/GMAIL.md`](deploy/GMAIL.md).
- Acceso por invitación, TOTP o passkeys, gestión de sesiones y cifrado de campos sensibles con claves por usuario.
- Presentación pública y páginas de privacidad y términos en español e inglés, accesibles sin sesión; exportación y supresión de cuentas mediante CLI, y retención del registro de seguridad de 365 días.
- Inicio con gasto mensual, barra de presupuesto, saldo disponible o exceso, referencia del día del mes y cierre estimado; temas claro/oscuro y control de animaciones con respeto al movimiento reducido.
- Diseño común en todas las vistas, con azul petróleo y acentos suaves en salvia, lavanda y azul grisáceo; navegación móvil con Reportes, tarjetas de cuentas/categorías y formularios adaptados a cada pantalla.
- Respaldos en caliente y respaldos programados cifrados con `age`: copia local en el servidor y destinos externos en Google Drive, S3 compatible o WebDAV, combinables. Incluye **Respaldar ahora** y descarga cifrada para admins con confirmación de identidad, estado en `/healthz` y aviso push si falla. La copia local conserva por defecto los 30 respaldos más recientes en `/backups` (montada desde `/opt/backup/pulse`); Drive usa `Backups/pulse` en Mi unidad. Configuración y autorización de Drive en [`deploy/README.md`](deploy/README.md).
- Reportes de 3, 6 o 12 meses: estadísticas de compras y devoluciones, líneas del total y por categoría, y proyecciones de la próxima semana o mes por categoría y total. Proyección de la próxima semana y el próximo mes con rango del 95 % simulado con tus días reales y método elegido por su error en periodos pasados. Proyección de fin de mes con valor y rango, elegida entre ritmo, regresión lineal, media móvil ponderada, día de la semana y perfil del mes según su error en meses cerrados; incluye gastos fijos por venir, excluye atípicos de forma visible y muestra su precisión. Gráficas del acumulado contra el mes anterior, categorías, día de la semana, mapa de calor y cuentas, todas con tabla y teclado. Exportación CSV con confirmación de identidad y conciliación con tus estados de cuenta.
- Plan con el detalle de en qué se gastó cada categoría (subcategorías y comercios) y Reportes con el gasto de cada cuenta por categoría.
- Presupuestos por categoría o por cuenta, con tope propio para un mes, umbrales de aviso a elección, arrastre del sobrante o exceso al mes siguiente y copia del plan del mes anterior.
- Gastos fijos mensuales y uso sin conexión: lectura de Inicio, Movimientos recientes y Plan, y cola cifrada de gastos nuevos, ediciones, borrados y gastos fijos que se envía sin duplicados al volver la conexión, con resolución de conflictos.

## Navegación

| Página | Qué encontrarás |
|---|---|
| `/` sin sesión | Presentación pública de Pulse |
| `/entrar` y `/registro` | Acceso y creación de cuenta por invitación |
| `/` con sesión | Resumen del mes, presupuesto, últimos movimientos y distribución por categoría |
| `/movimientos` | Registro, búsqueda, edición e importación de gastos |
| `/plan` | Presupuestos, seguimiento del gasto por categoría y gastos fijos |
| `/reportes` | Estadísticas, líneas mensuales, proyecciones semanales/mensuales por categoría y total, CSV y conciliación |
| `/ajustes` | Apariencia, Gmail, notificaciones y privacidad |
| `/ajustes/cuenta` | Mi cuenta: sesiones, contraseña, segundo factor, actividad de seguridad y cierre de sesión |
| `/ajustes/cuentas` y `/ajustes/categorias` | Organización de cuentas, tarjetas y categorías |

El perfil de la barra lateral y el avatar de la cabecera abren **Mi cuenta**. Las preferencias generales permanecen en **Ajustes**. En móvil, la barra inferior reúne Inicio, Movimientos, Plan, Reportes y Ajustes; **Nuevo gasto** está en la cabecera.

## Apariencia

Todas las vistas comparten una paleta de azul petróleo con acentos suaves en salvia, lavanda y azul grisáceo. Incluye los temas claro y oscuro, la portada, el acceso, las páginas legales y los iconos de la PWA. Cuentas, categorías y presupuestos se presentan en tarjetas; Ajustes y Mi cuenta agrupan sus controles en secciones que se distribuyen en columnas en escritorio y se apilan en móvil.

Los colores personalizados de cuentas y categorías se conservan y se presentan con menor saturación. Las ilustraciones de portada y acceso muestran datos de ejemplo identificados como tales. Las animaciones respetan la preferencia de movimiento reducido y se pueden pausar desde Ajustes.

El pie de las vistas con sesión muestra **Un gasto a la vez. V2026-10-16 01:18:00 · abc1234** (ejemplo). La fecha identifica la construcción del servidor y se convierte de UTC a tu zona horaria; el código final es su commit abreviado. Se consulta al servidor cada minuto mientras la app está visible, al volver a ella y al recuperar la conexión. Si no puede confirmar la versión, conserva la última consultada con la indicación **sin verificar**. Una compilación sin marca de construcción se identifica como **Versión de desarrollo**.

## Conectar Gmail

Un administrador configura el **ID de cliente OAuth** y el **secreto** en **Ajustes → Correo del banco (Gmail) → Configuración del servidor (admin)**. La pantalla muestra la URI de redirección y el webhook que debes registrar en Google Cloud. También permite configurar Pub/Sub y Gemini. Guardar requiere confirmar la identidad; las credenciales se cifran y se aplican sin reiniciar. Lo guardado en la app tiene prioridad sobre las variables de entorno; al borrarlo se vuelve a ellas.

Después, cada persona conecta su Gmail desde Ajustes. La primera sincronización consulta notificaciones bancarias de los **últimos 90 días**, con un límite de **1.000 mensajes por sincronización**. Las huellas para evitar duplicados se conservan 90 días y los avisos de nuevos gastos se limitan a consumos de las últimas 24 horas. El modo sombra deja los importados **Por revisar**.

Al sincronizar o cambiar la conexión, el aviso de éxito aparece después de actualizar el estado y el contador de importados en pantalla.

**Ajustes → Gmail** muestra los aciertos por banco según tus revisiones: corregir monto, día, tipo o cuenta cuenta como una corrección; cambiar la categoría o el comercio no. Con el modo sombra apagado, también se pueden confirmar automáticamente gastos con categoría del diccionario cuando el banco alcanza al menos **20 revisiones y 98 % de aciertos**, siempre que la lectura tenga suficiente confianza. Revisa los resultados reales antes de desactivar el modo sombra.

Si un correo bancario no se reconoce, Pulse registra su asunto cifrado y una huella, sin guardar el cuerpo. Los avisos de los últimos 30 días aparecen en Ajustes → Gmail y se notifica por push a los administradores, como máximo una vez por banco y día.

Las promociones reconocidas y los acuses de pago de tarjeta (por ejemplo, **Agradecemos su pago**) se ignoran: no crean gastos, alertas de formato ni consultas a Gemini. Las falsas alarmas antiguas de esos asuntos también se excluyen del listado. Se muestra el total real de pendientes y una vista previa de los 20 más recientes; la fecha indica cuándo se detectó el fallo.

La guía completa de Google Cloud, OAuth, Pub/Sub, Gemini y configuración por variables de entorno está en [`deploy/GMAIL.md`](deploy/GMAIL.md).

### Recuperar correos pendientes después de actualizar Pulse

1. Abre **Ajustes → Correo del banco (Gmail)** y comprueba que la conexión figure como **Conectado**. Si aparece **Sin acceso**, usa **Reconectar**.
2. En el aviso de correos pendientes, pulsa **Reintentar correos pendientes**. **Sincronizar ahora** mantiene la lectura habitual; el botón de reintento también vuelve a analizar fallos anteriores que se habían marcado como leídos.
3. Revisa los gastos recuperados en **Movimientos**. La alerta desaparece si el correo se importa, ya estaba importado o se identifica como un mensaje que no es gasto. Los formatos que siguen sin entenderse permanecen en el listado.

El reintento busca en los últimos **90 días**, con un máximo de **1.000 mensajes** por ejecución. Conserva las huellas de los correos ya procesados y no duplica gastos importados por Gmail o por archivo `.eml`.

Si aparece un aviso de demasiados intentos, espera un minuto antes de repetir: la conexión y las lecturas manuales comparten un límite por usuario. Si la lectura agota el tiempo de espera, puedes reintentar; los gastos ya registrados se conservan.

Para comprobar un correo que sigue fallando, puedes importar su archivo `.eml` desde Movimientos. También puedes analizarlo desde la terminal con el binario de Pulse, sin guardar movimientos:

```bash
pulse parse "correo-del-banco.eml"
```

El resultado indica si es un gasto reconocido (`expense`), un mensaje ignorado (`ignored`), un formato no reconocido (`unrecognized`) o un remitente no soportado (`unknown`). Los acuses de pago de tarjeta y las promociones reconocidas se ignoran; los consumos pendientes conservan su aviso.

## Importación bancaria y cuentas repetidas

La lectura de correos de Produbanco admite saltos de línea dentro del HTML, incluso entre la moneda y el monto, y fechas en formato mes/día/año como `10/15/2026` (15 de octubre). Para fechas numéricas ambiguas de Produbanco, como `10/09/2026`, elige entre día/mes y mes/día la interpretación más cercana a la fecha de envío. Las fechas ISO o con el mes escrito no se intercambian. Esta corrección se aplica al analizar nuevas importaciones o reintentar pendientes; no cambia las fechas de movimientos ya guardados.

Si el correo no trae los últimos dígitos de la tarjeta, el importador reutiliza una cuenta sin dígitos de la misma institución, prefiriendo una activa a una archivada. Si no existe, crea una. Las cuentas con dígitos siguen buscando coincidencia por sufijo e institución.

La migración `0010_unir_cuentas_repetidas` reúne las cuentas del mismo usuario que tienen institución, nombre y tipo iguales y no tienen dígitos. Conserva preferentemente una activa y, dentro de esa preferencia, la más antigua; reasigna sus movimientos y gastos fijos y traslada los totales de conciliación. Si coinciden varios totales para el mismo mes, conserva uno, sin sumarlos. Las cuentas sin institución o con dígitos no se unen.

Esta unión se aplica al arrancar. Bajar la migración no vuelve a separar las cuentas: para recuperar el estado anterior hace falta restaurar un respaldo previo. El despliegue crea ese respaldo cuando la instancia actual está sana; para una actualización manual, crea también un respaldo antes de arrancar la nueva versión.

## Reportes, conciliación y gastos fijos

En **Reportes**, elige 3, 6 o 12 meses para ver el gasto neto, el promedio diario, la compra promedio, el número de compras y las devoluciones. El cambio mensual compara los dos últimos meses cerrados para evitar comparar un mes parcial con uno completo. La gráfica de líneas muestra la evolución del total y permite compararlo con una categoría, consultar valores exactos por mes mediante un selector y abrir una tabla. El gráfico admite desplazamiento con teclado y conserva las selecciones al cambiar de periodo. Se conserva la distribución por categoría y cuenta. Los indicadores incluyen los movimientos hasta el momento de la consulta, sin adelantar gastos con fecha futura.

Las **proyecciones** estiman el gasto de la próxima semana calendario (lunes a domingo) o del próximo mes calendario, tanto total como por categoría principal, incluyendo sus subcategorías. Usan el promedio diario de compras menos devoluciones desde el primer movimiento dentro de los últimos 90 días completos hasta ayer, incluyendo los días sin movimientos. Requieren al menos 7 días de historial y muestran un aviso si hay menos de 28. Las categorías con saldo negativo se estiman en cero; el total suma las estimaciones redondeadas de las categorías. La línea punteada representa el gasto acumulado estimado y la tabla detalla los importes por categoría. Cambiar el rango de 3/6/12 meses no cambia la base de las proyecciones. No anticipan gastos extraordinarios ni suman por separado los gastos fijos, que ya pueden estar representados en el historial.

**Exportar CSV** descarga los movimientos del periodo tras confirmar tu identidad; la descarga queda en la actividad de seguridad y los campos de texto se protegen frente a la interpretación como fórmulas en hojas de cálculo.

En **Conciliación**, selecciona un mes e introduce el total del estado de cuenta de cada cuenta o tarjeta. Pulse lo compara con lo registrado y muestra la diferencia. La comparación usa el **mes calendario de tu zona horaria**, por lo que puede diferir de un estado de tarjeta con otra fecha de corte.

Para crear un gasto fijo, abre **Nuevo gasto** y marca **Repetir cada mes este día (gasto fijo)**. Puedes ver los próximos cobros y quitar la repetición en **Plan → Gastos fijos**. El servidor registra los vencidos al arrancar y los revisa cada hora; si el día no existe en un mes, usa el último día de ese mes. Quitar la repetición conserva los movimientos ya registrados.

## Uso sin conexión

Después de entrar con conexión, Pulse guarda en el navegador lo último de Inicio, Movimientos recientes y Plan, además de cuentas, categorías y gastos fijos. Sin red puedes verlo, y también crear, editar o borrar gastos, confirmar los de **Por revisar** y crear, cambiar o quitar gastos fijos. La cabecera muestra cuántos cambios faltan por enviar; se envían solos al volver la conexión (y cada 30 s mientras quede alguno), o con **Reintentar ahora**.

- **Cifrado y aislamiento**: todo se guarda en IndexedDB cifrado con AES-256-GCM, con una clave no exportable propia de cada cuenta y en una base separada por persona. Sin conexión solo se abre lo de la última persona que entró en ese navegador, durante 7 días como máximo. Al cerrar sesión se borra todo (si quedan cambios sin enviar, la app avisa antes), y sin red no se puede cerrar sesión, para no dejarla abierta en el servidor.
- **Sin duplicados**: cada cambio lleva su propio `Idempotency-Key`; reenviarlo tras perder la respuesta no lo aplica dos veces. Varios cambios del mismo registro se combinan en uno.
- **Conflictos**: las ediciones y los borrados llevan la versión que se vio (`If-Match`). Si el registro cambió en otro dispositivo, la app pregunta si usar tu versión o conservar la del servidor; si lo borraron allí, puedes volver a crearlo con tus cambios o descartarlos.
- La caché del service worker sigue sin guardar respuestas de la API. Si el servidor rechaza un cambio de forma definitiva (datos inválidos), la app avisa y lo descarta.

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

En Linux/macOS, instala primero las dependencias del frontend y luego inicia ambos servidores en terminales separadas. Ejecuta estos comandos desde la raíz del proyecto:

```bash
npm --prefix web ci
make dev                      # API en :8080
npm --prefix web run dev       # otra terminal: frontend en :5173 con proxy a la API
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
| Landing principal, sin sesión | `/` | — |
| Política de privacidad | `/privacidad` | `/privacy` |
| Términos y condiciones | `/terminos` | `/terms` |

Las páginas `/acerca`, `/about` y las páginas legales sirven contenido HTML legible sin ejecutar la aplicación y permiten cambiar de idioma. La landing de `/` forma parte de la app React y, con sesión, da paso al panel de gastos. La pantalla de acceso enlaza a **Qué es Pulse**, **Privacidad** y **Términos**. El service worker conserva las rutas estáticas públicas y no las sustituye por la aplicación.

## Administración y privacidad

Ejecuta la CLI con las mismas variables de configuración y claves que el servidor. La [guía de despliegue](deploy/README.md) incluye los comandos equivalentes en Docker y el procedimiento de restauración.

| Comando | Uso |
|---|---|
| `pulse invite create --email persona@example.com` | Crear una invitación |
| `pulse user list` | Listar usuarios |
| `pulse version` | Consultar la fecha UTC de construcción y el commit del binario |
| `pulse gdrive-auth --client-id ID --client-secret-file archivo --out archivo` | Autorizar Google Drive y guardar el token para los respaldos cifrados; ver la [guía de despliegue](deploy/README.md) |
| `pulse user export persona@example.com export.json` | Exportar los datos de una persona a un archivo nuevo |
| `pulse user delete persona@example.com --confirmar persona@example.com` | Eliminar la cuenta y sus datos asociados |
| `pulse backup --keep 15 respaldo.db` | Crear un respaldo en caliente y rotar los anteriores |
| `pulse restore respaldo.db` | Restaurar un respaldo con el servidor detenido |
| `pulse restore --identity clave.txt [--sha256 huella] respaldo.db.age` | Descifrar y restaurar un respaldo externo (la clave privada de `age` nunca está en el servidor) |
| `pulse parse correo.eml` | Probar el parser sin guardar movimientos |

La exportación contiene datos personales descifrados: entrégala por un canal seguro y elimina el archivo después. La supresión conserva el registro de seguridad hasta que vence su retención; los respaldos anteriores se eliminan al rotar. Si existía una conexión con Gmail, el usuario también puede retirar el acceso desde su cuenta de Google. Los textos públicos están en `web/public/`, con una página HTML por ruta y estilos compartidos en `legal.css`.

## Pruebas

| Comando | Qué cubre |
|---|---|
| `make test` / `make race` | Unitarias e integración de Go (API completa sobre SQLite real, reloj falso) |
| `make fuzz` | Fuzzing de cifrado, WebAuthn y cálculos del plan |
| `make web-test` | Tipos de TypeScript + Vitest (montos, fechas) |
| `make e2e` | Playwright en escritorio y móvil: registro, TOTP, passkey (autenticador virtual), gastos, plan, PWA sin conexión, accesibilidad (axe) en ambos temas, CSP sin violaciones |
| `make check` | Formato y vet, detector de carreras, pruebas del frontend y E2E; el fuzzing se ejecuta aparte con `make fuzz` |

La suite E2E incluye landing pública, páginas legales, Mi cuenta, configuración de Google por un admin, estadísticas y proyecciones de Reportes, exportación con confirmación de identidad, conciliación, gastos fijos y envío tras recuperar la conexión. La revisión del diseño recorre las vistas públicas y privadas en escritorio/móvil y claro/oscuro, comprueba accesibilidad con axe y ausencia de desbordamiento horizontal, y genera capturas para inspección. Cada ejecución utiliza puerto y datos propios dentro de `web/.e2e/`; las capturas no constituyen todavía una comparación automática contra imágenes de referencia.

El escenario de proyecciones de Reportes toma las fechas del calendario del usuario informado por la API, para mantener 14 días completos de historial aunque el equipo de pruebas esté en UTC. Comprueba los importes semanales por categoría y total, el importe mensual de la API y la presentación de las proyecciones en pantalla.

Para ejecutar solo las pruebas del rediseño y Reportes desde la raíz:

```bash
npm --prefix web run e2e -- design.spec.ts reportes.spec.ts
```

Para reproducir la comprobación de Reportes en las dos zonas horarias verificadas, en Linux/macOS:

```bash
TZ=UTC npm --prefix web run e2e -- reportes.spec.ts
TZ=America/Guayaquil npm --prefix web run e2e -- reportes.spec.ts --grep 'estadísticas'
```

En PowerShell, establece la zona horaria del proceso antes de cada ejecución:

```powershell
$env:TZ = 'UTC'
npm.cmd --prefix web run e2e -- reportes.spec.ts
$env:TZ = 'America/Guayaquil'
npm.cmd --prefix web run e2e -- reportes.spec.ts --grep 'estadísticas'
```

La primera ejecución cubre los seis casos de Reportes; la segunda comprueba las proyecciones en escritorio y móvil. El ejemplo usa USD 154,00 netos durante 14 días completos y espera USD 77,00 para la próxima semana: USD 63,00 en Comida y USD 14,00 en Servicios. Así se comprueban los resultados esperados además del texto y las gráficas.

Para instalar Chromium antes de las pruebas E2E: `cd web && npx playwright install chromium` (en Linux CI: `--with-deps chromium`). Para revisar dependencias: `npm audit` en `web/` y `go mod verify` en la raíz. La CI también comprueba vulnerabilidades Go alcanzables con `govulncheck`, escanea la imagen con Trivy y ejecuta OWASP ZAP baseline contra las páginas públicas del contenedor, bloqueando alertas de riesgo medio o alto.

## Despliegue

Ver [`deploy/README.md`](deploy/README.md). Un push a `main` ejecuta las pruebas, construye y firma la imagen en GHCR y activa el despliegue del entorno `production`, sujeto a sus controles de GitHub. El servidor escucha por defecto en el puerto 8080.

### Comprobar la actualización del VPS

Después de subir cambios, revisa la ejecución correspondiente en [GitHub Actions](https://github.com/mavg-8G/Pulse/actions/workflows/ci.yml): el push inicia el proceso y el despliegue debe terminar correctamente antes de comprobar la nueva versión.

Consulta el endpoint público de metadatos (sin datos de usuarios) o el binario del contenedor:

```bash
curl -fsS https://pulse.espectro.uk/api/version
# Desde la consola del VPS:
sudo docker exec pulse /pulse version
```

Ambos devuelven `built_at` en UTC y `revision` con el commit completo. La fecha queda grabada en el binario al construirlo y se conserva al reiniciar el contenedor. CI pasa la misma marca y revisión a la imagen probada y a la publicada; las construcciones con `make build`, `make docker` y `scripts/dev.ps1` también incorporan estos datos. El endpoint responde con `Cache-Control: no-store` y la PWA lo consulta por red. Si aparece el aviso **Hay una versión nueva de Pulse**, pulsa **Actualizar** para cargar también el nuevo frontend.

Compara `revision` con el commit que esperas desplegar. Si acabas de subir el commit actual de tu copia local, puedes obtener su identificador completo con:

```bash
git rev-parse HEAD
```

El pie muestra los primeros siete caracteres de esa revisión y convierte `built_at` a tu zona horaria. La fecha indica cuándo se construyó el servidor; la coincidencia del commit permite confirmar qué cambios está sirviendo el VPS. Si el pie indica **sin verificar**, muestra el último dato consultado y todavía no confirma la versión actual: recupera la conexión y espera la siguiente consulta o vuelve a la pestaña.

`/healthz` comprueba la base y que el esquema coincida con el binario; devuelve 503 si falla esa comprobación e informa si Gmail y push están activos. Con el respaldo externo activado, también informa su estado (`"backup": "ok"`, `"stale"` o `"failing"`). La guía de despliegue explica cómo configurarlo con variables de entorno y secretos en archivos, la política de solo escritura del almacenamiento, la restauración en una máquina limpia y la configuración de monitores; `deploy/backup-offsite.sh` sigue disponible.

La migración `0009_reportes_recurrentes` añade las revisiones de importación, avisos de plantillas, totales de conciliación y gastos recurrentes; se aplica al arrancar como las migraciones anteriores. El despliegue debe conservar la base y la KEK para mantener acceso a los campos cifrados.

El rediseño y las estadísticas/proyecciones ampliadas no requieren una migración adicional. La migración `0010_unir_cuentas_repetidas` se describe en la sección de importación bancaria. Las migraciones `0011_sin_conexion` (versiones y claves de cambio), `0012_presupuestos_flexibles` (conserva los presupuestos y avisos existentes) y `0013_respaldos` (historial de respaldos) se aplican al arrancar.
