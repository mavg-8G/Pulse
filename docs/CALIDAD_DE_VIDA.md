# Calidad de vida — fases 14, 15, 16, 18 y 19

Entrega del 17 de octubre de 2026. La fase 17 sigue reservada para el rediseño visual de escritorio y móvil.

## Comportamiento

| Área | Comportamiento de la entrega |
|---|---|
| Movimientos | Conserva mes, cuenta, categoría, filas cargadas y posición al volver. Búsqueda solo en memoria de la sesión autenticada; mes/filtros/límite en la URL y en la sesión de cada persona. Limpiar filtros conserva el mes; volver al mes actual es otra acción. |
| Plan y Reportes | Plan conserva su mes. Reportes mantiene sus selecciones previas y recupera el desplazamiento. Las vistas tienen contextos independientes. |
| Formulario de gasto | Errores asociados al campo y foco en el primero inválido; conserva lo escrito tras un fallo. Ofrece seguir editando o descartar al cerrar y al navegar con cambios sin guardar. El cierre de pestaña usa el aviso del navegador cuando está disponible. |
| Cuenta inicial | Al crear: cuenta del contexto, última cuenta elegible guardada cifrada y primera cuenta disponible. Editar conserva la cuenta del registro, incluso si está archivada o vinculada. |
| Guardado | Bloquea envíos simultáneos y conserva la clave idempotente al reenviar el mismo contenido. Si una respuesta se pierde, la cola reutiliza la clave original. Un cambio encolado se anuncia como pendiente del servidor. |
| Conflicto en línea | Una edición que encuentra otra versión permite cargarla tras confirmar que se descartan los cambios locales. La resolución de conflictos sin conexión conserva las opciones existentes. |
| Importación | Resultado por archivo, incluido su nombre, en una sección persistente que recibe el foco. Se puede cerrar después de revisarla; una nueva importación reemplaza el resultado anterior. |
| Actualización | Inicio, Movimientos y Plan distinguen carga inicial y actualización; las consultas fallidas ofrecen reintento. Cuentas, Categorías, Gmail y respaldos también ofrecen reintentar su lectura. Reportes no presenta las cifras de otro periodo bajo los filtros nuevos. |
| Rendimiento | Búsqueda agrupada tras 250 ms; cancelación de consultas obsoletas de Movimientos y Reportes; invalidación de Reportes, proyecciones y conciliación al modificar gastos. La preparación sin conexión comparte las consultas de las vistas para evitar lecturas duplicadas. Lectura por páginas de hasta 200 filas al mostrar más; hasta 5 000 visibles por vista. |
| Recursos | Reportes, Ajustes, Cuentas y Categorías se cargan por separado al abrirse. El service worker conserva los recursos estáticos necesarios para su uso sin conexión; sigue sin guardar respuestas de API. |
| Accesibilidad | Etiquetas estables, errores descritos por sus controles, retorno de foco al cerrar diálogos, destino accesible del enlace de salto, reflujo del formulario y respeto del movimiento reducido en los enlaces de Reportes. |

No se guardan borradores entre recargas. Los cambios todavía no guardados pertenecen al formulario abierto. La preferencia de cuenta utiliza el almacén cifrado ya existente; si el almacenamiento no está disponible, el formulario sigue funcionando con la primera cuenta elegible.

## Lenguaje de acciones y estados

| Situación | Mensaje o acción |
|---|---|
| Escritura confirmada por el servidor | Gasto guardado / actualizado / confirmado / borrado, según la acción. |
| Escritura guardada en la cola local | Explica que está en este dispositivo y falta enviarla; nunca equivale a confirmación del servidor. |
| Cola en proceso | Sincronizando… |
| Conflicto o rechazo | Requiere tu atención; el panel conserva la explicación de los cambios rechazados mientras se revisa. |
| Validación | Explica el dato que falta o debe corregirse junto al control. |
| Falta de red | Revisa tu internet e inténtalo de nuevo; los cambios en cola mantienen su estado. |
| Sesión vencida (401) | Vuelve a entrar para continuar. Se limpia el contexto en memoria de la sesión anterior. |
| Otra versión (409) | Revisa la versión de otro dispositivo antes de volver a guardar. |
| Límite temporal (429) | Espera un momento antes de volver a intentarlo. |
| Fallo del servidor (5xx) | Inténtalo de nuevo en unos momentos; no se publica el detalle técnico del servidor. |
| Step-up cancelado | Confirmación cancelada; no se repite la escritura ni se presenta como fallo técnico. |
| Borrar gasto | Confirmación con el comercio y el importe del registro que se va a borrar. |
| Devolución | Nombre del tipo `refund` en el formulario y en los conflictos; se conserva la representación de importes y cálculos existentes. |

## Verificación reproducible

Desde la raíz:

```powershell
npm.cmd --prefix web run build
npm.cmd --prefix web test -- --pool=forks --maxWorkers=1
npm.cmd --prefix web run e2e -- calidad.spec.ts gastos.spec.ts importar.spec.ts sin-conexion.spec.ts
```

El servidor E2E necesita Go y un compilador C configurado como el resto de la suite. Cada ejecución crea su propio servidor y base con datos ficticios. `calidad.spec.ts` bloquea el service worker para que los fallos HTTP simulados lleguen a las rutas de Playwright; las pruebas PWA y sin conexión existentes verifican por separado el comportamiento con service worker.

Cobertura nueva: conservar contexto al volver, búsqueda privada, cambios de persona, cierre protegido, Atrás con cambios, errores de campo, fallo de guardado con datos conservados, envío doble, cuenta recordada, respuesta perdida con la misma clave, error de lectura con reintento, búsqueda agrupada, accesibilidad en ambos temas y formulario a 320 px con texto al 200 %. La prueba de más de 200 filas comprueba límites y offsets válidos, ausencia de duplicados y conservación de la suma del periodo.

Para medir rendimiento de forma explícita:

```powershell
$env:PULSE_PERF_LABEL = 'after'
npm.cmd --prefix web run e2e -- quality-perf.spec.ts --project=escritorio
Remove-Item Env:PULSE_PERF_LABEL
```

La medición usa Chromium, 1 280 × 800, compilación de producción, red local sin limitación y movimiento reducido. Crea 0, 100 y 5 000 movimientos y mide cinco cargas completas por vista de Inicio, Movimientos, Plan y Reportes. Guarda tiempos, peticiones API y CLS en `artifacts/quality/perf-<etiqueta>.json`. Es una comparación local, no una medición del VPS ni del teléfono físico. La suite ordinaria omite esta medición costosa si no se define la variable.

La referencia `before` se tomó antes de cambiar las vistas y dividir sus recursos; ya incluía la preparación de identidad, cancelación e idempotencia de esta entrega. Su primera instrumentación registraba varios observadores CLS por documento: para compararla se divide cada CLS por el índice global de muestra más uno. Tiempos y peticiones no requieren corrección. La medición final registra un único observador por documento.

## Resultados de la entrega

- Compilación TypeScript/Vite y generación del service worker correctas; `git diff --check` sin errores.
- 80 pruebas unitarias correctas. Incluyen la cuenta recordada tras rotar la caché, rechazo de una respuesta de la persona anterior, preparación sin conexión sin lecturas duplicadas, cancelación, mensajes y paginación sobre 200 filas.
- 90 escenarios funcionales E2E verificados en escritorio y móvil entre la suite completa y las repeticiones selectivas que confirmaron las correcciones. La última revisión de calidad, diseño, Reportes y pie pasó 26 de 26. Se verificó aparte acceso y sincronización, incluida la última preparación compartida de consultas.
- Axe sin hallazgos críticos o serios en las vistas revisadas, en ambos temas. Reflujo comprobado a 320 px con texto al 200 %, sin desbordamiento del documento ni del diálogo; capturas inspeccionadas y conservadas localmente en `artifacts/quality/`. La comprobación usa el ancho real de 320 px, ya que en emulación móvil `innerWidth` puede crecer al desbordarse la página.
- La búsqueda rápida de «supermercado» produce una sola consulta tras la escritura. Una respuesta perdida de una edición se reenvía con la misma clave y no crea un conflicto ni incrementa la versión dos veces.

Medianas de cinco cargas por vista; cada ejecución contiene 60 muestras. Todos los casos cumplen el presupuesto de no empeorar más del 10 % frente a la referencia. Estos tiempos describen este entorno local y no acreditan la latencia del VPS o del teléfono físico.

| Movimientos de prueba | Vista | Antes (ms) | Después (ms) | Lecturas API antes → después | CLS antes → después |
|---:|---|---:|---:|---:|---:|
| 0 | Inicio | 428 | 144 | 12 → 9 | 0,0130 → 0,0130 |
| 0 | Movimientos | 494 | 184 | 13 → 10 | 0,0066 → 0,0009 |
| 0 | Plan | 314 | 133 | 15 → 11 | 0,0286 → 0,0283 |
| 0 | Reportes | 627 | 306 | 15 → 12 | 0,0002 → 0,0139 |
| 100 | Inicio | 420 | 185 | 12 → 9 | 0,0183 → 0,0178 |
| 100 | Movimientos | 522 | 249 | 13 → 10 | 0,0378 → 0,0378 |
| 100 | Plan | 276 | 132 | 15 → 11 | 0,0415 → 0,0411 |
| 100 | Reportes | 717 | 423 | 15 → 12 | 0,0002 → 0,0142 |
| 5 000 | Inicio | 688 | 367 | 12 → 9 | 0,0177 → 0,0176 |
| 5 000 | Movimientos | 1 194 | 415 | 13 → 10 | 0,0382 → 0,0382 |
| 5 000 | Plan | 274 | 159 | 15 → 11 | 0,0368 → 0,0368 |
| 5 000 | Reportes | 2 725 | 1 083 | 15 → 12 | 0,0002 → 0,0142 |

El CLS de Reportes subió ligeramente con su carga separada; se conserva en la tabla, sin presentarlo como una mejora de estabilidad. El tiempo máximo de la última ejecución completa fue 1 227 ms. Una ejecución preliminar se interrumpió al superar su espera original de 10 s; las dos repeticiones completas posteriores terminaron correctamente con un margen de espera de 60 s. Las medianas no garantizan el peor caso.

Los JSON de referencia, resultado y comparación quedan en `artifacts/quality/`. Para reproducir el cálculo, ejecuta `node web/e2e/quality-perf.compare.mjs` desde la raíz después de generar ambos conjuntos. El comparador reconoce la instrumentación antigua de CLS y las nuevas mediciones con un único observador.

## Aceptaciones fuera de la automatización

- Sigue pendiente una revisión con lector de pantalla real, anotando lector, navegador y resultados. Axe y el árbol de accesibilidad automatizado no sustituyen esa aceptación.
- Siguen pendientes las aceptaciones físicas y de producción ya abiertas de las fases 10, 12 y 13. Esta entrega no las cierra.
- La implementación y las pruebas automatizadas de estas fases se registran por separado de esas aceptaciones. El rediseño visual de la fase 17 no se ha iniciado.
