# Predicción de gastos: métodos, selección y evaluación

Pulse proyecta tres horizontes: el **cierre del mes en curso**, la **próxima semana** (lunes a domingo) y el **próximo mes**, en total y por categoría principal. Código: `internal/forecast` (`forecast.go` para el cierre de mes, `horizon.go` para semana y mes, `models.go` para los métodos añadidos y la regla de selección).

## Qué se modela y qué no

- El gasto se separa en **gastos fijos** (cobros de gastos recurrentes), **compras atípicas** (muy por encima de lo habitual: más de Q3 + 3·IQR en escala logarítmica y de 10 veces la mediana) y **consumo variable**. Los métodos solo modelan el consumo variable.
- Los gastos fijos ya cobrados se suman tal cual y los **programados** se suman en su fecha, una sola vez: no se extrapolan ni se cuentan dentro del promedio. En el cierre de mes, el método anterior («ritmo del mes») sí extrapolaba los fijos ya cobrados; se conserva solo como referencia.
- Las devoluciones restan del día en que ocurren; una categoría nunca se proyecta negativa.
- Los días se cuentan por fecha local del usuario (zona horaria de su perfil), así que los cambios de horario no mueven gastos de día.
- El total es siempre la suma de las categorías ya redondeadas a centavos.

## Métodos

| Id | Nombre en pantalla | Idea |
|---|---|---|
| `ritmo` | Promedio diario (semana/mes) · Ritmo del mes (cierre) | Referencia simple. En semana/mes: promedio de los últimos 90 días completos. En el cierre: gastado ÷ días × días del mes (método original). |
| `promedio_reciente` | Promedio diario reciente | Cierre: lo gastado + fijos programados + promedio de 90 días × días que faltan. Es la estimación prudente cuando no hay meses para comparar. |
| `regresion` | Regresión lineal | Cierre: recta sobre el acumulado del mes (método existente). |
| `robusto` | Promedio robusto | **Nuevo.** Estimador M de Huber sobre las últimas 12 semanas completas del total: las semanas excepcionales pesan menos. Se reparte entre categorías según su peso en el mismo periodo. |
| `media_movil` | Media móvil ponderada | Existente: más peso a los días recientes. |
| `dia_semana` | Por día de la semana | Existente: promedio de cada día de la semana, encogido hacia la media. |
| `perfil` | Estacionalidad del mes | Cierre: qué parte del mes suele estar gastada a esta altura (existente). |
| `holt` | Tendencia amortiguada | **Nuevo.** Suavizado exponencial de Holt sobre totales semanales con tendencia amortiguada (φ < 1). |
| `holt_winters` | Tendencia y ajuste semanal | **Nuevo.** Holt-Winters aditivo diario con estacionalidad de 7 días y tendencia amortiguada. |
| `combinado` | Combinación de métodos | **Nuevo.** Promedio de los pronósticos disponibles de los métodos anteriores. |

Los parámetros de suavizado (α, β, γ, φ) se eligen por cuadrícula con el error a un paso **dentro de la ventana de entrenamiento**, nunca mirando el periodo a predecir.

## Validación temporal y selección

- **Semana y mes**: se retrocede el reloj de 7 en 7 días (hasta 12 orígenes). En cada origen se predice un periodo igual de largo y a la misma distancia usando solo los datos anteriores, y se compara con lo que realmente se gastó. Los métodos se comparan en los mismos periodos; si no alcanzan, queda fuera el que más historial exige.
- **Cierre de mes**: cada mes cerrado (hasta 6) se evalúa como si fuera el mismo día del mes que hoy.
- **Regla de selección**: se recorren los métodos del más simple al más complejo. Uno más complejo solo reemplaza al que va ganando si su error medio es **al menos 10 % menor** y además se equivocó menos en **más de la mitad** de los periodos. Ningún método se elige por ser más complejo, y una racha de suerte en uno o dos periodos no basta.
- **Poco historial**: sin periodos suficientes no se compara; se usa el promedio diario (semana/mes) o lo gastado + fijos + promedio reciente (cierre), y la pantalla lo indica con «Confianza baja».

## Rangos y cobertura

- Semana y mes: rango del 95 % por simulación (2 000 periodos armados con días reales del historial; con los métodos semanales, del mismo día de la semana). La **cobertura** cuenta en cuántos periodos pasados el gasto real cayó dentro del rango construido con lo que se sabía entonces.
- Cierre de mes: intervalo empírico con los errores de los meses cerrados (t de Student); la cobertura se mide dejando fuera cada mes al construir su propio rango.
- La API devuelve `typical_error_pct` (error medio ÷ gasto real medio), `improvement_pct` (cuánto menos se equivocó que el promedio/ritmo), `coverage`, `confidence` (alta/media/baja) y `warnings`.

## Resultados reproducibles

Se ejecutan con historiales sintéticos de semillas fijas (gastos log-normales, días sin movimientos, devoluciones, cambios de tendencia y de nivel, fines de semana, gastos excepcionales y fijos mensuales):

```sh
PULSE_FORECAST_EVAL=1 go test ./internal/forecast -run 'TestEvaluacion$' -v      # imprime las tablas
PULSE_FORECAST_EVAL=$PWD/docs/.eval.md go test ./internal/forecast -run 'TestEvaluacion$'   # además las guarda
```

«Selección anterior» usa los métodos y la regla previos a este cambio (menor error, sin margen); «Selección nueva», todos los métodos con la regla nueva. Una versión corta (`TestEvaluacionCorta`) corre con `go test ./...` y falla si la selección nueva empeora a la anterior o se aleja más de un 15 % del promedio diario.

Escenarios sintéticos de 420 días, 3 semillas por escenario y 36 orígenes por semilla (uno cada 5 días). Error = suma de errores absolutos ÷ gasto real (WAPE). Cobertura = veces que el gasto real cayó dentro del rango del 95 %.

### Próxima semana

| Escenario | Promedio diario | Selección anterior | Selección nueva | Cobertura anterior | Cobertura nueva | Métodos elegidos (nueva) |
|---|---:|---:|---:|---:|---:|---|
| Gasto estable con ruido | 30.2 % | 30.2 % | 30.4 % | 96 % | 97 % | ritmo 103/108, holt 3/108 |
| Fines de semana el triple | 33.5 % | 33.5 % | 33.5 % | 96 % | 100 % | ritmo 93/108, holt 8/108 |
| Cambio de tendencia: sube 60 % en 4 meses | 31.9 % | 31.7 % | 32.0 % | 91 % | 91 % | ritmo 105/108, combinado 1/108 |
| Cambio de nivel: baja a la mitad | 43.5 % | 42.9 % | 43.9 % | 93 % | 93 % | ritmo 78/108, media_movil 17/108 |
| Gastos excepcionales frecuentes | 36.3 % | 36.7 % | 36.3 % | 94 % | 94 % | ritmo 100/108, holt 5/108 |
| Pocos movimientos: días sin gasto | 85.2 % | 86.1 % | 86.8 % | 96 % | 94 % | ritmo 87/108, robusto 16/108 |
| Devoluciones frecuentes | 36.6 % | 37.4 % | 37.3 % | 96 % | 96 % | ritmo 100/108, holt_winters 6/108 |
| Con gastos fijos mensuales | 24.4 % | 24.8 % | 24.4 % | 92 % | 93 % | ritmo 89/108, media_movil 7/108 |

### Próximos 30 días

| Escenario | Promedio diario | Selección anterior | Selección nueva | Cobertura anterior | Cobertura nueva | Métodos elegidos (nueva) |
|---|---:|---:|---:|---:|---:|---|
| Gasto estable con ruido | 15.5 % | 15.9 % | 16.0 % | 95 % | 97 % | ritmo 77/108, media_movil 15/108 |
| Fines de semana el triple | 20.3 % | 21.1 % | 20.8 % | 92 % | 94 % | ritmo 81/108, dia_semana 16/108 |
| Cambio de tendencia: sube 60 % en 4 meses | 17.7 % | 17.9 % | 18.1 % | 91 % | 90 % | ritmo 64/108, media_movil 17/108 |
| Cambio de nivel: baja a la mitad | 31.9 % | 32.6 % | 32.4 % | 83 % | 85 % | ritmo 74/108, media_movil 24/108 |
| Gastos excepcionales frecuentes | 21.4 % | 21.4 % | 21.3 % | 95 % | 96 % | ritmo 100/108, robusto 7/108 |
| Pocos movimientos: días sin gasto | 42.2 % | 44.8 % | 43.0 % | 94 % | 94 % | ritmo 87/108, media_movil 9/108 |
| Devoluciones frecuentes | 21.5 % | 22.3 % | 21.9 % | 94 % | 95 % | ritmo 99/108, robusto 4/108 |
| Con gastos fijos mensuales | 10.2 % | 10.5 % | 10.6 % | 88 % | 89 % | ritmo 79/108, robusto 16/108 |

### Cierre del mes en curso

| Escenario | Promedio diario | Selección anterior | Selección nueva | Cobertura anterior | Cobertura nueva | Métodos elegidos (nueva) |
|---|---:|---:|---:|---:|---:|---|
| Gasto estable con ruido | 20.6 % | 11.6 % | 11.5 % | 97 % | 97 % | promedio_reciente 47/108, ritmo 31/108 |
| Fines de semana el triple | 24.3 % | 13.4 % | 13.7 % | 85 % | 84 % | promedio_reciente 40/108, dia_semana 30/108 |
| Cambio de tendencia: sube 60 % en 4 meses | 20.9 % | 11.3 % | 12.1 % | 87 % | 87 % | promedio_reciente 40/108, ritmo 26/108 |
| Cambio de nivel: baja a la mitad | 27.4 % | 18.6 % | 18.8 % | 95 % | 95 % | ritmo 52/108, promedio_reciente 33/108 |
| Gastos excepcionales frecuentes | 22.7 % | 14.1 % | 13.1 % | 94 % | 94 % | promedio_reciente 69/108, ritmo 20/108 |
| Pocos movimientos: días sin gasto | 51.7 % | 26.8 % | 27.1 % | 95 % | 95 % | promedio_reciente 30/108, robusto 21/108 |
| Devoluciones frecuentes | 31.8 % | 14.7 % | 14.3 % | 97 % | 97 % | promedio_reciente 50/108, ritmo 22/108 |
| Con gastos fijos mensuales | 75.1 % | 7.7 % | 7.1 % | 93 % | 94 % | promedio_reciente 68/108, dia_semana 15/108 |

La selección nueva iguala o mejora a la anterior en 4 de 8 escenarios (semana), 6 de 8 (30 días) y 4 de 8 (cierre de mes).


### Lectura honesta

- **Cierre de mes**: la mejora grande es frente al ritmo del mes (por ejemplo, 75 % → 7 % de error con gastos fijos mensuales), sobre todo por separar los fijos y usar el promedio reciente. Frente a la selección anterior, la nueva iguala o mejora en la mitad de los escenarios y gana con gastos excepcionales (14,1 % → 13,1 %), devoluciones (14,7 % → 14,3 %) y fijos (7,7 % → 7,1 %).
- **Semana y 30 días**: con gasto estacionario y ruidoso, el **promedio diario es muy difícil de superar**; los métodos complejos casi nunca lo superan con claridad, y por eso la regla nueva lo mantiene la mayoría de las veces. La selección nueva queda a menos de 1,6 puntos del promedio diario en todos los escenarios y por debajo de la selección anterior en 30 días con pocos movimientos (44,8 % → 43,0 %) y devoluciones (22,3 % → 21,9 %). Con márgenes menores (3 % sin la condición de mayoría) la selección perseguía ruido y quedaba peor que el promedio diario: así se eligió el margen del 10 %.
- La cobertura del rango del 95 % queda entre 84 % y 100 % en semana/mes salvo el cambio brusco de nivel (85 %), donde el historial reciente ya no representa el futuro.
- Son datos sintéticos: confirman que los cálculos se comportan bien con irregularidades, pero la mejora real depende de tu historial. La sección «Comparar métodos» de cada proyección muestra el error de cada método con tus propios datos.
