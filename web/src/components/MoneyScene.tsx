/** A clearly labelled, fictional example shared by the public and access pages. */
export function MoneyScene() {
  return (
    <figure className="money-scene" aria-label="Ejemplo ilustrativo de un resumen mensual de gastos">
      <figcaption><span>Un mes, en equilibrio</span><span className="scene-badge">Ejemplo</span></figcaption>
      <div className="scene-summary"><span>Gasto del mes</span><strong>$684,50</strong><span>de $1.200,00 de presupuesto</span><div className="scene-track"><i /></div><p>57% utilizado <span>Quedan $515,50</span></p></div>
      <div className="scene-chart" aria-hidden="true"><span>Tu ritmo, día a día</span><svg viewBox="0 0 360 110" fill="none"><path className="scene-grid" d="M0 25h360M0 65h360M0 105h360" /><path className="scene-area" d="M0 98 30 85 60 88 90 63 120 69 150 48 180 57 210 30 240 42 270 20 300 28 330 10 360 16V110H0Z" /><path className="scene-line" d="M0 98 30 85 60 88 90 63 120 69 150 48 180 57 210 30 240 42 270 20 300 28 330 10 360 16" /></svg></div>
      <div className="scene-categories"><span><i />Hogar <strong>$280</strong></span><span><i />Comida <strong>$214</strong></span><span><i />Transporte <strong>$96</strong></span></div>
    </figure>
  )
}
