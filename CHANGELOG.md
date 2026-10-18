# Registro de cambios de Pulse

Cambios relevantes de Pulse. Las entradas «Sin publicar» describen cambios posteriores a la versión declarada `0.2.0`; sus fechas corresponden al registro del trabajo y no a una publicación o etiqueta de GitHub. Las secciones históricas conservan el contexto de cada etapa.

La guía de uso y desarrollo está en [`README.md`](README.md); el roadmap y las aceptaciones pendientes, en [`PLAN.md`](PLAN.md).

## Sin publicar — rediseño visual, fase 17 (17 oct 2026)

- Nueva identidad en modo oscuro y claro: fondos `#0A0E13` / `#F3F5F7`, acento verde señal (`#3DDC97` / `#0B7A53`) y estados con contraste AA. Tipografía Hanken Grotesk para la interfaz y JetBrains Mono para los importes, servidas desde el propio origen.
- Navegación superior en escritorio (Inicio, Movimientos, Plan, Reportes, Cuentas, Categorías, Ajustes y Mi cuenta) y barra inferior en tablet y móvil con botón central «Nuevo gasto». Las áreas seguras de iPhone y Android se respetan arriba y abajo. Se retiran la barra lateral y la ruta de migas.
- Superficies planas sin degradados pastel; tarjetas, listas, formularios, hojas, avisos y Reportes comparten radios, bordes y sombras. Los campos usan 16 px para que iOS no amplíe la página al enfocarlos.
- Movimiento: trazo de bienvenida bajo la barra superior, entrada escalonada, barras que crecen, gráficas que se revelan, contador animado del gasto del mes, esqueletos con destello y respuesta al pasar el cursor y al presionar. Todo se apaga con «reducir movimiento» o con la pausa de Ajustes.
- Vite ya no incrusta las fuentes como `data:` (lo bloqueaba la CSP). El manifiesto, `theme-color` y los íconos de la PWA usan la paleta nueva.

## Sin publicar — calidad de vida, fases 14, 15, 16, 18 y 19 (17 oct 2026)

- Ajustada la protección de cambios para consultar el estado del formulario antes de pintar, sin esperar a que se vuelva a registrar el bloqueo de navegación. El aviso de cierre se activa solo mientras hay cambios o un guardado en curso.
- Estabilizadas las pruebas E2E: recargan las cuentas creadas por API, esperan la lectura que actualiza el total tras editar y la finalización de las rutas antes de usar Atrás. La prueba de notificaciones admite los parámetros que conservan el mes de Plan.
- Movimientos conserva contexto al volver: mes, filtros, búsqueda en memoria, filas cargadas y posición. Plan conserva su mes; las selecciones de cada vista se aíslan por persona y se limpian al cerrar sesión. Reportes recupera la posición y descarta filtros que ya no existen.
- Formulario de gasto con errores junto a los controles, foco en el primer dato inválido, cuenta inicial recordada y cifrada, bloqueo de envíos simultáneos y protección de cambios al cerrar o usar Atrás. Una edición en conflicto permite cargar la versión actual tras confirmar el descarte.
- Corregida la clave de idempotencia al encolar una escritura cuya respuesta se perdió: el reenvío usa la misma clave. Los resultados de importar varios correos y los rechazos de la cola permanecen disponibles mientras se revisan.
- Mensajes comunes para desconexión, sesión vencida, conflictos, espera temporal y fallo del servidor; cancelar la reautenticación se distingue de un fallo técnico. Reintentos de lectura en las vistas principales y en Ajustes de Gmail y respaldos.
- Búsqueda agrupada y consultas obsoletas cancelables; Reportes, proyecciones y conciliación se invalidan al cambiar gastos. «Mostrar más» pagina correctamente cuando supera las 200 filas. Reportes, Ajustes, Cuentas y Categorías se cargan como recursos separados.
- Etiquetas accesibles, retorno de foco, reflujo de controles y respeto del movimiento reducido en los enlaces internos de Reportes. La fase 17 permanece reservada para el rediseño visual. Procedimiento y aceptaciones pendientes en [`docs/CALIDAD_DE_VIDA.md`](docs/CALIDAD_DE_VIDA.md).
- Verificación: compilación correcta, 80 pruebas unitarias y 90 escenarios funcionales E2E verificados entre la suite y las repeticiones selectivas. La última revisión de calidad/diseño/Reportes/versión pasa 26 de 26, con axe en ambos temas y reflujo a 320 px con texto al 200 %. Medición local de 60 cargas con 0, 100 y 5 000 movimientos: todos los casos cumplen el presupuesto de tiempo y se reducen 3–4 lecturas API por carga. La preparación sin conexión comparte consultas con las vistas y una respuesta anterior a un cambio de persona no se guarda en su nuevo almacén. La aceptación con lector de pantalla real sigue pendiente.

## Sin publicar — Reportes en iPhone (17 oct 2026)

- El selector de periodo y los botones Filtros y Exportar CSV ya no se salen de su tarjeta en pantallas angostas: en móvil los periodos usan etiquetas cortas (Mes, 3 m, 6 m, 12 m, Elegir; el nombre accesible sigue siendo el completo) y pasan a otra fila si el texto está ampliado.
- Nueva prueba E2E a 390, 375 y 320 px y con texto al 125 %: ningún control de la tarjeta queda fuera de ella y la página no se desplaza a lo ancho. Falla sin el arreglo; con él, Reportes y diseño 14 de 14 y 69 pruebas Vitest.

## Sin publicar — rediseño de Reportes y predicción de gastos (16 oct 2026)

- **Reportes** se reordena en Resumen, Conclusiones, Evolución, Categorías, Comercios, Cuentas y Proyecciones, con navegación entre secciones, esqueleto de carga, error con reintento y estado vacío.
- Periodo de 1, 3, 6 o 12 meses o un rango de meses; comparación con el periodo anterior de igual duración o con el mismo periodo del año pasado. Filtros por categoría y cuenta (las fusionadas cuentan como su principal) que aplican a todas las secciones, a las proyecciones y al CSV, y se conservan en la URL y en la sesión.
- Resumen con gasto neto, compras, devoluciones y promedio diario, cada uno con la diferencia en importe y, solo si el periodo comparado está completo y tiene gasto, en porcentaje. Conclusiones calculadas con las cifras del servidor: categorías y comercios que más subieron o bajaron, concentración del gasto y cambio del promedio diario.
- Gráficas de líneas que ocupan el ancho disponible, con leyenda que oculta series, valores del punto tocado (también con flechas del teclado), línea del periodo comparado, proyección punteada y rango sombreado. En móvil, filtros plegables y detalle progresivo.
- API: `GET /api/reports/summary` acepta `category`, `account` y `compare=year` y devuelve `period`, `previous` (con cobertura completo/parcial/sin_datos), `series`, `category_changes` y `merchants` (los 8 con más gasto). `projection` y `export.csv` aceptan los mismos filtros (las proyecciones de semana y mes van en el resumen). Todo por usuario y en centavos; una categoría o cuenta ajena devuelve 400.
- **Predicción**: nuevos métodos (promedio robusto de Huber, Holt con tendencia amortiguada, Holt-Winters diario con estacionalidad semanal, combinación) evaluados con validación temporal. Un método más complejo solo reemplaza al más simple si su error medio es al menos 10 % menor y gana en más de la mitad de los periodos. Los gastos fijos programados se suman una vez y no se extrapolan; el total es la suma de las categorías. Nuevos campos `typical_error_pct`, `improvement_pct`, `coverage`, `confidence` y `warnings`; sin meses cerrados el cierre del mes usa lo gastado + fijos + promedio reciente con «Confianza baja».
- Evaluación reproducible en [`docs/PREDICCIONES.md`](docs/PREDICCIONES.md): en el cierre de mes el error baja mucho frente al ritmo del mes (por ejemplo, 75 % → 7 % con gastos fijos mensuales); en semana y 30 días el promedio diario sigue siendo difícil de superar y la selección nueva queda a menos de 1,6 puntos de él.
- Verificación: `gofmt`, `go vet` y `go test ./...` correctos, con pruebas nuevas de los modelos, la regla de selección, la coherencia total/categorías, la estimación prudente, filtros y comparación por API, aislamiento entre usuarios y zonas horarias (Guayaquil y Tokio). 69 pruebas Vitest. Suite E2E completa en escritorio y móvil: 78 de 78 (Reportes 8 de 8), y revisión visual en ambos temas y anchos.

## Sin publicar — fusión lógica de cuentas (16 oct 2026)

- **Ajustes → Cuentas y tarjetas**: cada cuenta tiene **Fusionar con…** para vincularla a una cuenta principal cuando son la misma tarjeta (por ejemplo, correos con y sin los últimos dígitos). Ninguna se borra y la lectura de correos sigue asignando gastos a cualquiera de las dos. **Separar** deshace el vínculo sin mover gastos.
- Una cuenta fusionada se lee como su principal: el filtro de Movimientos trae los gastos de ambas, Reportes y «Por cuenta» muestran una sola fila, el presupuesto por cuenta suma las dos, la conciliación suma los estados ingresados y el CSV usa el nombre de la principal. Las listas para elegir cuenta solo muestran las principales; no se crean presupuestos ni estados en una cuenta fusionada.
- Migración `0014_cuentas_vinculadas`: columna `accounts.merged_into` y triggers que rechazan vínculos entre personas distintas, consigo misma, ciclos o más de un nivel. Nuevos `POST`/`DELETE /api/accounts/{id}/merge`, registrados en la auditoría.
- La prueba de respaldo cifrado ya no fija la versión de esquema en el nombre esperado del archivo.
- Verificación: `gofmt`, `go vet` y `go test ./...` correctos, con pruebas de fusión por API (aislamiento entre usuarios, filtro, reportes, presupuesto, conciliación, separar, fusionar un grupo) y de los triggers de la base. 64 pruebas Vitest. Suite E2E completa en escritorio y móvil: 78 de 78, con la fusión y la separación revisadas con axe-core.

## Sin publicar — documentación de respaldos y limpieza del repositorio (16 oct 2026)

- README actualizado con la copia cifrada local, su retención por defecto, los destinos externos combinables y Google Drive en `Backups/pulse`. Incluye el comando `pulse gdrive-auth` y el enlace a su configuración.
- La caché incremental de TypeScript (`*.tsbuildinfo`) deja de versionarse y queda excluida en `.gitignore`; las compilaciones pueden regenerarla sin añadir cambios al repositorio.

## Sin publicar — respaldo local en el servidor y en Google Drive (16 oct 2026)

- Con solo `PULSE_BACKUP_AGE_RECIPIENTS`, cada respaldo deja una copia **cifrada** en la carpeta local `/backups` del contenedor (montada desde `/opt/backup/pulse` en el host) y conserva las 30 más recientes (`PULSE_BACKUP_LOCAL_KEEP`). `PULSE_BACKUP_LOCAL_DIR=off` la apaga.
- Nuevo destino externo `gdrive`: sube a `Backups/pulse` en Mi unidad (crea las carpetas que falten) con permiso OAuth `drive.file`, que solo da acceso a los archivos creados por Pulse. Subida reanudable, verificación del SHA-256 que calcula Drive, sin duplicados al reintentar y los respaldos que pasan de `PULSE_BACKUP_GDRIVE_KEEP` van a la papelera.
- `PULSE_BACKUP_TARGET` acepta varios destinos separados por comas (`gdrive,s3`). Si un destino falla, los reintentos solo repiten los que faltan.
- Nuevo comando `pulse gdrive-auth`: autorización única con PKCE y redirección a `127.0.0.1` (funciona por túnel SSH desde el VPS); guarda el refresh token en un archivo 0600 para usarlo como Docker secret.
- `docker-compose.yml` monta `/opt/backup/pulse:/backups` y trae comentados los secretos de Drive; guía en `deploy/README.md`.
- Verificación: `gofmt`, `go vet` y `go test ./...` correctos, con pruebas nuevas de la carpeta local (rotación, permisos, huella), de varios destinos, de Drive contra un servidor falso (carpetas, rotación, token revocado, archivo alterado, 503 reintentable, sin duplicados) y del flujo OAuth (state falso rechazado, PKCE).

## Sin publicar — rango del 95 % en Proyección de gastos, desglose por categoría y por cuenta (16 oct 2026)

- **Proyección de gastos** (próxima semana y próximo mes) deja de ser solo «promedio diario × días». Compara tres métodos: promedio diario, media móvil ponderada (vida media de 21 días) y día de la semana encogido hacia la media. Los evalúa proyectando hasta 12 periodos pasados iguales, con origen móvil cada 7 días y solo con lo que se sabía entonces, y elige el de menor error medio en los mismos periodos. Los gastos fijos programados se suman en su fecha y las compras atípicas no se repiten.
- Rango del 95 % por simulación (2 000 periodos armados con días reales del historial, remuestreando también el nivel de gasto para incluir la incertidumbre del promedio). Se muestra en las cifras, como banda en la gráfica acumulada y con su cobertura: cuántas veces el gasto real de los periodos pasados cayó dentro del rango. Es determinista para una misma entrada.
- La proyección de fin de mes usa la misma simulación cuando el método elegido no tiene rango propio, antes de recurrir al de la regresión.
- Los gastos fijos por venir se calculan para varios meses (`UpcomingRecurring` repite cada uno en su día, o en el último del mes si ese día no existe).
- **Plan → En qué se gastó**: cada categoría del mes con subcategorías, los 6 comercios que más suman (el resto agrupado), devoluciones y pendientes de revisar, más la lista de categorías sin gasto. Nuevo `GET /api/budgets/breakdown?month=`, que descifra los comercios solo para la respuesta y está disponible sin conexión cifrado como el resto del Plan.
- **Reportes → Por cuenta**: cada cuenta con su total, número de compras, reparto por categoría en una barra y una tabla al abrirla (`account_categories` y `account_purchases` en el resumen).
- Verificación: `gofmt`, `go vet` y `go test ./...` correctos, con pruebas nuevas de horizonte (gasto constante con rango exacto, poco historial, gastos fijos y atípicos, día de la semana, calibración del rango con gasto irregular ≥ 75 % en 12 periodos, comparación en los mismos periodos y cambio de horario), del desglose del Plan con aislamiento entre usuarios y del resumen por cuenta. 57 pruebas Vitest. Suite E2E completa en escritorio y móvil: 74 de 74, con el rango, la elección del método, el desglose del Plan y la vista por cuenta revisados con axe-core.

## Sin publicar — fases 10 a 13: sin conexión, presupuestos flexibles, respaldo externo y proyecciones (16 oct 2026)

**Fase 10, uso sin conexión**
- Cola cifrada en IndexedDB para crear, editar y borrar gastos, confirmar los de **Por revisar** y crear, cambiar o quitar gastos fijos sin red. Cada cambio lleva su `Idempotency-Key` y los cambios del mismo registro se combinan; reenviar tras perder la respuesta no duplica.
- Concurrencia optimista: gastos y gastos fijos tienen `version`; ediciones y borrados envían `If-Match` y un 409 `version_conflict` devuelve el registro actual. La app pide elegir entre **Usar la mía** y **Conservar la del servidor**; si se borró en otro dispositivo, permite volver a crearlo o descartar. Migración `0011_sin_conexion`.
- Lectura sin conexión de Inicio, Movimientos recientes, Plan, cuentas, categorías y gastos fijos. Se guardan cifrados con AES-256-GCM, con una clave no exportable y datos asociados `pulse|usuario|almacén|clave`, en una base por persona. Solo se abre la de la última persona y durante un máximo de 7 días. Todo se borra al cerrar sesión, que avisa si quedan cambios sin enviar y no se permite sin red.
- Indicador en la cabecera con lo pendiente, los conflictos o «Sin conexión». El envío es único en curso, se reintenta al volver la red y cada 30 s, y avisa de lo enviado. Recargar la cola durante un envío ya no puede reenviar un cambio entregado.

**Fase 11, presupuestos flexibles**
- Presupuestos por categoría o por cuenta (los de cuenta no suman al total del plan), tope propio para un mes concreto y umbrales de aviso por presupuesto, que se disparan una sola vez por mes aunque cambie el tope.
- Arrastre opcional del sobrante o exceso al mes siguiente desde un mes elegido, y **Copiar plan de <mes>** al mes siguiente. Migración `0012_presupuestos_flexibles`, que conserva presupuestos y avisos existentes.

**Fase 12, respaldo externo cifrado**
- Servicio dentro del binario elegido frente a Litestream o una base PostgreSQL: no añade procesos y nunca envía datos sin cifrar. Hace una instantánea con `VACUUM INTO`, la cifra con `age` usando solo claves públicas (la privada no está en el servidor) y la sube a S3 compatible (firma SigV4 propia, sin SDK) o WebDAV. Reintenta con espera creciente y conserva solo metadatos en `backup_runs` (migración `0013_respaldos`).
- **Respaldar ahora** y **Descargar copia cifrada** en Ajustes, solo para admins y con confirmación de identidad; muestran fecha, tamaño, huella y resultado, y quedan en la actividad de seguridad. `/healthz` informa `backup` (`ok`, `stale`, `failing`…) sin revelar destino ni fechas, y se envía un aviso push a los admins si falla o se queda antiguo.
- `pulse restore --identity clave.txt [--sha256 huella] respaldo.db.age` descifra en un directorio temporal junto a la base y restaura. Configuración con variables `PULSE_BACKUP_*` y secretos en archivos, política IAM de solo escritura y guía de restauración en `deploy/README.md`.

**Fase 13, proyecciones y gráficas**
- Nuevo `GET /api/reports/projection` con proyección de fin de mes por varios métodos: ritmo actual (referencia), regresión lineal sobre el acumulado con intervalo de predicción del 95 %, media móvil ponderada, ajuste por día de la semana y perfil del mes. Se elige automáticamente el de menor error medio en hasta 6 meses cerrados evaluados el mismo día del mes, y la precisión por mes queda visible.
- Proyección por categoría principal y total. Suma los gastos fijos por venir, trata los cobros fijos como conocidos y excluye de forma visible los atípicos (escala logarítmica, Q3 + 3·IQR y al menos 10 veces la mediana). Avisos de historial insuficiente y sin divisiones entre cero.
- Gráficas nuevas: acumulado del mes contra el anterior con banda de rango, barras por categoría frente al mes anterior, distribución por día de la semana, mapa de calor de 91 días y evolución por cuenta. Todas tienen tabla alternativa y navegación con teclado.
- Corregida la prueba E2E de Reportes, que fallaba entre las 19:00 y las 24:00 de Guayaquil al calcular fechas con el reloj UTC del runner.

**Privacidad**: la política (v1.1, 16 oct 2026) describe el almacén cifrado del navegador y el respaldo externo cifrado.

- Verificación: `gofmt`, `go vet` y `go test -race ./...` correctos, incluidos los paquetes nuevos `forecast` (regresión calculada a mano, pocos días, devoluciones, bisiestos, cambio de horario, atípicos) y `backup` (ida y vuelta cifrada, identidad ajena o archivo alterado rechazados, destino caído con reintento y aviso, firma S3 contra los vectores de botocore). Compilación del frontend y 57 pruebas Vitest con IndexedDB simulado. Suite E2E completa en escritorio y móvil: 72 de 74 a la primera; las 2 restantes (el selector «Tope mensual» de `plan.spec.ts`, ambiguo con el nuevo campo de tope del mes) pasan tras corregirlo. Incluye sin conexión con la red cortada, conflictos, aislamiento entre personas, presupuestos flexibles, respaldo con S3 simulado y proyección con axe-core en ambos temas. Las pruebas E2E de Reportes calculan las fechas en la zona de la cuenta.
## Sin publicar — versión del servidor visible en el pie (16 oct 2026)

- El pie añade la fecha y hora de la construcción con precisión de segundos, convertida de UTC a la zona del usuario, y el commit abreviado. Permite identificar la versión que responde desde el VPS.
- Consulta por red cada minuto con la app visible, al volver a la pestaña y al recuperar la conexión; si falla, muestra la última versión consultada como **sin verificar**. Las compilaciones sin marca se identifican como desarrollo.
- Nuevo `GET /api/version` público con solo `built_at` y `revision`, sin caché, y comando `pulse version` para inspeccionar el mismo dato directamente en el contenedor.
- CI genera una marca UTC por construcción y la pasa junto al SHA a la imagen probada y a la publicada. La prueba de humo de la imagen comprueba ambos valores. Docker, Makefile y el script de desarrollo de Windows incorporan la marca al binario; reiniciar el servicio no cambia su fecha.
- README ampliado con la comprobación del despliegue en GitHub Actions y la comparación del commit esperado con `/api/version`, el contenedor y el pie. Aclara qué representa la fecha de construcción y cómo interpretar **sin verificar**.
- Verificación: compilación y TypeScript, 45 pruebas Vitest, pruebas Go de buildinfo/API/CLI y `go vet` correctos; workflow validado con `actionlint`. Los 14 escenarios E2E de versión, diseño y PWA quedaron verificados entre la ejecución completa y las repeticiones selectivas, incluido el footer a 320 px y ambos temas. La revisión visual espera el panel y la autenticación antes de capturar cada vista. La construcción de la imagen y su prueba de humo se ejecutan en CI.

## Sin publicar — guía de comprobación de Reportes (16 oct 2026)

- README ampliado con los comandos para reproducir las pruebas de Reportes en UTC y en Guayaquil, tanto en Linux/macOS como en PowerShell.
- Documentados los datos y resultados esperados del escenario de proyecciones: 14 días de historial, USD 154,00 netos y USD 77,00 semanales, desglosados por categoría.

## Sin publicar — fechas de las pruebas de Reportes en CI (15 oct 2026)

- Reproducido el fallo que esperaba USD 77,00 en la proyección semanal: cuando el equipo en UTC ya estaba en el día siguiente respecto a Guayaquil, los movimientos de prueba generaban 13 días de historial en lugar de 14.
- Las fechas del escenario E2E se calculan desde el último día completo informado por la API y se envían como horas locales del usuario. Se conserva la expectativa de USD 77,00 y se comprueban también los 14 días de historial, las proyecciones por categoría y el total mensual.
- Verificación: TypeScript correcto; los 6 E2E de Reportes pasan con el equipo en UTC y el escenario de proyecciones también pasa en escritorio y móvil con `TZ=America/Guayaquil`.

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
