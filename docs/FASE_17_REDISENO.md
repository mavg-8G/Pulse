# Fase 17 — Rediseño visual de escritorio y móvil: plan propuesto

Estado: **implementada el 17 oct 2026** con una dirección distinta de la propuesta inicial: modo oscuro y claro, navegación superior, cifras en monoespaciada y movimiento como parte de la identidad. Las secciones siguientes conservan el plan original como referencia; lo aplicado está en «Resultado».

## 1. Punto de partida (revisado en el código)

- Los estilos están en dos capas: `web/src/styles.css` (1 869 líneas, 23 `@media`) y `web/src/design.css` (504 líneas), que **sobrescribe** a la primera. Los tokens están repartidos: colores base y movimiento en `styles.css`, acentos (`--blue`, `--sage`, `--violet`, `--sand`) y radios en `design.css`, con radios distintos en cada archivo (`--r-s` 8 px frente a 9 px). Esto es la mayor deuda: rediseñar sobre dos capas en conflicto multiplica el trabajo.
- Identidad actual: petróleo/salvia/azul pizarra/lavanda, Public Sans + Bricolage Grotesque, modo claro/oscuro con `data-theme`, «latido» de Pulse (`--beat`), `MoneyScene` y barra lateral oscura en escritorio; barra de pestañas inferior en móvil.
- Ya existe y debe conservarse: contraste AA en ambos temas, reflujo a 320 px y texto al 200 %, movimiento reducido y pausa manual, tablas alternativas de las gráficas, foco visible, axe en ambos temas.
- Cobertura que protege el rediseño: `design.spec.ts`, `interface.spec.ts`, `reportes.spec.ts`, `calidad.spec.ts`, `quality-perf.spec.ts` (con presupuesto de rendimiento).

## 2. Decisiones previas (Decidir)

| # | Pregunta | Recomendación |
|---|----------|---------------|
| 1 | ¿Qué motiva el rediseño? (aspecto anticuado, densidad, legibilidad en móvil, marca…) | Definir 3 problemas concretos con capturas; sin ellos no hay criterio de «terminado». |
| 2 | ¿Evolución o ruptura de identidad? | Evolución: conservar paleta y tipografía, rehacer jerarquía, espaciado, navegación y componentes. |
| 3 | ¿Móvil primero o escritorio primero? | Móvil primero: es el uso diario (captura de gastos) y el más restringido. |
| 4 | ¿Se cambia la navegación (pestañas/barra lateral)? | Solo si el punto 1 lo justifica; es lo más caro de revertir. |
| 5 | ¿Referencias visuales? | Pedir 2–3 apps o capturas que te gusten y 1–2 que no. |

## 3. Principios de diseño

1. **Registrar un gasto en menos de 10 segundos** desde cualquier pantalla: la acción «Nuevo» manda sobre la decoración.
2. **El dato primero.** Importes, estado del mes y alertas arriba; adornos (`MoneyScene`, ondas) nunca empujan contenido útil.
3. **Una sola fuente de verdad visual:** tokens para color, tipografía, espacio, radio, elevación y movimiento; ningún valor suelto en componentes.
4. **Estados completos** en cada componente: reposo, foco, activo, deshabilitado, cargando, error, vacío, sin conexión, pendiente de enviar (reutilizar el lenguaje de estados de la fase 16).
5. **Accesibilidad como requisito de entrada, no de cierre:** AA, objetivos táctiles ≥ 44 px, el color nunca es el único portador de significado (importante en «excedido/advertencia/ok»).
6. **Cifras tabulares** (`font-variant-numeric: tabular-nums`) y alineación a la derecha en todo importe.
7. **Movimiento con propósito:** transiciones para orientar (abrir hoja, cambiar mes), no para decorar; todo respeta `prefers-reduced-motion` y la pausa manual.

## 4. Consideraciones de diseño

**Sistema y tokens**
- Escala de espacio (4/8 px), de tipografía (fluida con `clamp`), radios (s/m/l), elevaciones (máximo 3) y duraciones.
- Tokens semánticos (`--surface`, `--ink`, `--ok/--warn/--over`) separados de los de marca (`--blue`, `--sage`…). Los acentos de categoría se asignan por semántica, no por orden.
- Tema oscuro diseñado a propósito, no invertido: superficies por elevación, saturación reducida, contraste verificado con la misma herramienta que el claro.
- Evaluar `light-dark()` y `color-scheme` para quitar el bloque duplicado `@media (prefers-color-scheme)` + `[data-theme]`.

**Móvil**
- Zona del pulgar: «Nuevo gasto» y filtros frecuentes en la mitad inferior; hojas (`Sheet`) a pantalla casi completa con acción primaria fija abajo y teclado numérico.
- Barra de pestañas: máximo 5 destinos; el resto en «Más». Respetar `safe-area-inset-*` y no tapar contenido (hay ya un padding compensatorio que deberá desaparecer al rehacer el layout).
- Listas largas: densidad cómoda, agrupación por día, pegado del encabezado del día, sin saltos de disposición (presupuesto de la fase 18).
- Prueba en 320, 375, 390 y 430 px, texto al 125 y 200 %, y orientación horizontal.

**Escritorio**
- Rejilla de 12 columnas con ancho máximo de lectura; no estirar listas ni gráficas a pantallas anchas.
- Barra lateral: decidir si sigue oscura en ambos temas (hoy lo es) o sigue al tema; colapsable en anchos medios (900–1200 px, hoy el tramo más frágil).
- Aprovechar el ancho con paneles lado a lado (lista + detalle/edición) en lugar de solo hojas modales.
- Atajos de teclado visibles para las acciones frecuentes.

**Pantallas con decisiones propias**
- *Inicio:* resumen del mes (gastado, presupuesto, ritmo/proyección), «Por revisar» y últimos movimientos. Decidir el destino de `MoneyScene`: conservar como elemento de marca acotado o retirar.
- *Movimientos:* es la pantalla más usada; priorizar escaneo (comercio, categoría, importe), filtros plegables y búsqueda visible.
- *Plan:* estado de cada tope con barra + texto + icono (no solo color).
- *Reportes:* ya rediseñado el 16–17 oct; entra al sistema de tokens sin rehacer su estructura. Revisar gráficas con la paleta nueva (daltonismo: validar series con un simulador).
- *Ajustes, Cuentas, Categorías, Gmail, Respaldos:* formularios largos; agrupar por secciones con encabezado y descripción corta, acciones peligrosas separadas.
- *Landing y acceso (público):* son la primera impresión; mantener coherencia con la app y el español/inglés actual.

**Marca y contenido**
- Revisar textos de interfaz junto con el diseño (inventario de la fase 19) para no rehacer etiquetas dos veces.
- Íconos: un solo set, trazo y tamaño consistentes (`icons.tsx`).
- PWA: iconos, pantalla de arranque y `theme-color` coherentes con la paleta final.

## 5. Plan de implementación

Cada etapa se entrega y se revisa por separado; ninguna se da por terminada solo porque compile.

**Etapa 0 — Descubrimiento y criterios (1–2 días)**
- Responder las decisiones de la §2. Capturar el estado actual (claro/oscuro × 390/768/1280 px × todas las vistas) como línea base.
- Definir criterios de aprobación medibles: tareas clave con tiempo/toques, axe sin hallazgos serios, presupuesto de rendimiento sin empeorar más de 10 %.
- **Entregable:** este documento con las decisiones cerradas.

**Etapa 1 — Dirección visual (2–3 días)**
- Prototipos de 3 pantallas (Inicio, Movimientos, formulario de gasto) en móvil y escritorio, claro y oscuro, antes de tocar código de producción.
- Tú eliges dirección; se ajusta una vez.
- **Entregable:** prototipos aprobados + tabla de tokens.

**Etapa 2 — Cimientos de estilos (2–3 días)**
- Fusionar `styles.css` y `design.css` en una sola capa ordenada (tokens → base → componentes → páginas); eliminar las sobrescrituras. Sin cambio visual intencional: las capturas de la Etapa 0 deben coincidir.
- Resolver los tokens duplicados (radios, `--nav-w`).
- **Riesgo:** es la etapa más delicada; el E2E de diseño y las capturas son la red de seguridad. Conviene dejar las capturas como pruebas visuales automáticas (pendiente abierto de la fase 3).

**Etapa 3 — Componentes base (3–4 días)**
- Botones, campos, selector segmentado, insignias, `Sheet`, confirmaciones, avisos, estados vacíos/carga/error, `TxRow`, indicadores de sincronización. Todos con sus estados completos.
- **Entregable:** una página interna de referencia de componentes (solo desarrollo) para revisar estados en ambos temas.

**Etapa 4 — Estructura y navegación (3–4 días)**
- Marco de la aplicación (barra lateral/pestañas/barra superior) según la decisión 4. Verificar que el contexto de la fase 15 (mes, filtros, posición) sigue funcionando.

**Etapa 5 — Pantallas, en este orden (1–2 días cada una)**
1. Movimientos y formulario de gasto (mayor uso)
2. Inicio
3. Plan
4. Reportes (migración a tokens)
5. Cuentas, Categorías, Ajustes, Gmail y Respaldos
6. Landing y acceso

**Etapa 6 — Cierre transversal (2–3 días)**
- Axe en todas las vistas y ambos temas, reflujo a 320 px, texto al 200 %, movimiento reducido.
- Comparación de rendimiento con `quality-perf` en las mismas condiciones que la fase 18.
- Revisión manual en un iPhone y un Android reales y con el lector de pantalla pendiente de la fase 19 (se aprovecha para cerrar esa aceptación).
- Actualizar PLAN.md, CHANGELOG.md y las capturas de referencia.

Estimación: 3–4 semanas a tiempo parcial. Si se decide solo refinar (sin cambiar navegación), las etapas 4 y parte de la 5 se reducen y baja a unas 2 semanas.

## 6. Riesgos

| Riesgo | Mitigación |
|--------|------------|
| Regresiones visuales silenciosas al fusionar los CSS | Capturas de línea base y comparación automática antes de cualquier cambio de aspecto. |
| Romper accesibilidad ya lograda | axe y reflujo en cada etapa, no solo al final. |
| Rediseño que reabre la navegación y el contexto de la fase 15 | Etapa 4 aislada y con las pruebas de contexto de la fase 15 en verde. |
| Empeorar rendimiento (fuentes, sombras, animaciones) | Presupuesto del 10 % de la fase 18 y medición en cada etapa. |
| Alcance que crece («ya que estamos…») | Lista de lo que queda fuera: nuevas funciones, nuevos módulos, cambios de datos o de API. |

## 7. Fuera de alcance

Nuevas funciones financieras, cambios de API o base de datos, nuevos bancos, cambios de idioma o de textos legales.

## 8. Criterios para dar la fase por terminada

- Las decisiones de la §2 están registradas y aprobadas.
- Todas las vistas usan los mismos tokens; no queda CSS que sobrescriba a otro ni valores de color/espacio sueltos.
- Axe sin hallazgos críticos ni serios en todas las vistas, ambos temas y anchos.
- Reflujo correcto a 320 px y con texto al 200 %; movimiento reducido respetado.
- Rendimiento dentro del presupuesto; las pruebas E2E existentes pasan sin relajarlas.
- Revisión manual en dispositivos físicos y con lector de pantalla registrada.

## Resultado (17 oct 2026)

- **Decisiones tomadas:** identidad nueva y no una evolución (verde señal `#3DDC97` en oscuro / `#0B7A53` en claro, fondos `#0A0E13` / `#F3F5F7`); navegación superior en escritorio y barra inferior con botón central en móvil; Hanken Grotesk + JetBrains Mono.
- **Archivos:** tokens en `web/src/styles.css`; capa visual reescrita en `web/src/design.css`, que ya no usa degradados ni fondos pastel; navegación en `web/src/components/AppShell.tsx`; contador animado en `web/src/components/CountUp.tsx`.
- **Se eliminó:** la barra lateral, su tarjeta «Este mes» y la ruta de migas de la barra superior. Cuentas y Categorías pasan a la barra superior en escritorio; en móvil se llega desde Ajustes.
- **Movimiento:** una curva de salida (`--ease-out`) y otra de entrada y salida (`--ease-in-out`); duraciones de 140, 220, 360 y 800–1100 ms. La revelación de gráficas usa `clip-path`; el resto, solo `transform` y `opacity`.
- **Pendiente:** revisión en iPhone y Android físicos; las páginas estáticas `acerca.html`, `about.html` y `legal.css` siguen con la tipografía anterior.
