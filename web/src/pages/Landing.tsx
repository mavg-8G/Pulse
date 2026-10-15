import { Link } from 'react-router-dom'
import { PulseMark } from '../components/icons'
import { MoneyScene } from '../components/MoneyScene'

// Un mes de ejemplo (centavos por día). Solo ilustra: no son datos de nadie.
const SAMPLE = [0, 1240, 0, 450, 0, 0, 3890, 820, 0, 0, 1675, 0, 560, 0, 9450, 0, 0, 1320, 0, 2210, 640, 0, 0, 4150, 0, 780, 0, 0, 1980, 0]
const TODAY = 24

function heroTrace() {
  const W = 960
  const base = 150
  const step = W / SAMPLE.length
  const max = Math.max(...SAMPLE)
  let d = `M0 ${base}`
  SAMPLE.forEach((v, i) => {
    if (i >= TODAY) return
    const mid = i * step + step / 2
    if (v > 0) {
      const h = Math.max(10, (v / max) * 120)
      d += ` L${(mid - step * 0.3).toFixed(1)} ${base} L${(mid - step * 0.12).toFixed(1)} ${(base - h).toFixed(1)} L${(mid + step * 0.06).toFixed(1)} ${(base + Math.min(14, h * 0.14)).toFixed(1)} L${(mid + step * 0.24).toFixed(1)} ${base}`
    }
    d += ` L${((i + 1) * step).toFixed(1)} ${base}`
  })
  return { d, W, base, todayX: (TODAY - 0.5) * step }
}

export function Landing() {
  const t = heroTrace()
  return (
    <div className="landing">
      <header className="landing-bar">
        <Link to="/" className="landing-brand" aria-label="Pulse, inicio">
          <PulseMark />
          <span translate="no">Pulse</span>
        </Link>
        <nav aria-label="Acceso">
          <a href="/about" hrefLang="en" lang="en" className="landing-lang">English</a>
          <Link to="/entrar" className="btn btn-sm">Entrar</Link>
        </nav>
      </header>

      <main id="contenido">
        <section className="landing-hero" aria-labelledby="landing-title">
          <div className="landing-copy">
          <p className="page-kicker">Dale espacio a lo que importa</p>
          <h1 id="landing-title">Tu dinero tiene pulso.</h1>
          <p className="landing-lead">
            Registra cada gasto, ponle un tope a cada categoría y mira tu mes en un solo trazo. Pulse es un registro personal de
            gastos, privado y solo por invitación.
          </p>
          <div className="landing-actions">
            <Link to="/entrar" className="btn btn-primary">Entrar</Link>
            <Link to="/registro" className="btn">Tengo una invitación</Link>
          </div>
          <p className="landing-caption">Personal, privado y a tu ritmo.</p>
          </div>
          <MoneyScene />
        </section>

        <figure className="landing-trace" aria-label="Ejemplo: el pulso de un mes, un pico por cada día con gastos">
          <div className="landing-trace-plot">
            <svg viewBox={`0 0 ${t.W} 190`} preserveAspectRatio="none" aria-hidden="true">
              <line className="landing-trace-base" x1="0" x2={t.W} y1={t.base} y2={t.base} vectorEffect="non-scaling-stroke" />
              <path className="landing-trace-line" d={t.d} vectorEffect="non-scaling-stroke" />
            </svg>
            <span className="landing-trace-dot" style={{ left: `${(t.todayX / t.W) * 100}%`, top: `${(t.base / 190) * 100}%` }} />
          </div>
          <figcaption>
            <span>Día 1</span>
            <span className="landing-trace-today" style={{ left: `${(t.todayX / t.W) * 100}%` }}>Hoy</span>
          </figcaption>
        </figure>

        <section className="landing-steps" aria-labelledby="h-como">
          <h2 id="h-como">Cómo funciona</h2>
          <ol>
            <li>
              <h3>Registra</h3>
              <p>Anota un gasto en segundos desde el teléfono o la computadora: monto, categoría y listo. Si quieres, Pulse lo trae solo desde el correo de tu banco.</p>
            </li>
            <li>
              <h3>Planifica</h3>
              <p>Define cuánto quieres gastar en comida, transporte o lo que importe para ti. Pulse calcula cuánto te queda y cómo cerrarías el mes.</p>
            </li>
            <li>
              <h3>Recibe avisos</h3>
              <p>Te avisa al 80&nbsp;% y al 100&nbsp;% de cada presupuesto, y cuando llega un gasto nuevo para revisar.</p>
            </li>
          </ol>
        </section>

        <section className="landing-gmail" aria-labelledby="h-banco">
          <div>
            <h2 id="h-banco">Tu banco ya te avisa de cada compra</h2>
            <p>
              Conecta Gmail y Pulse convierte las notificaciones de Banco Pichincha, Produbanco y Diners Club en gastos por revisar.
              Es opcional y lo puedes desconectar cuando quieras.
            </p>
            <ul>
              <li>Solo lee los correos de tus bancos: la búsqueda se filtra en Google por remitente.</li>
              <li>No guarda tus correos: extrae monto, fecha y comercio, y los descarta.</li>
              <li>Nada se confirma sin ti: cada gasto importado queda por revisar.</li>
            </ul>
            <p><a href="/privacidad#gmail">Qué datos usa Pulse</a></p>
          </div>
          <div className="landing-demo" aria-hidden="true">
            <div className="landing-mail">
              <span className="landing-mail-from">Banco Pichincha</span>
              <strong>Consumo con tu tarjeta …4821</strong>
              <span>$24,90 en Supermaxi, hoy a las 18:42</span>
            </div>
            <svg className="landing-demo-arrow" viewBox="0 0 24 40"><path d="M12 2v34m-7-7 7 7 7-7" /></svg>
            <div className="tx landing-tx">
              <span className="tx-icon">🛒</span>
              <span className="tx-main">
                <span className="tx-title">Supermaxi <span className="badge warn">Por revisar</span></span>
                <span className="tx-meta">Supermercado en Visa Pichincha</span>
              </span>
              <span className="tx-amount">$24,90</span>
            </div>
          </div>
        </section>

        <section className="landing-trust" aria-labelledby="h-datos">
          <h2 id="h-datos">Tus datos son tuyos</h2>
          <p>
            Sin publicidad, sin analítica y sin vender nada. Segundo factor obligatorio, cifrado por persona de los datos sensibles y
            servidor en la Unión Europea. Puedes pedir una copia de tus datos o borrarlos cuando quieras.
          </p>
          <Link to="/entrar" className="btn btn-primary">Entrar a Pulse</Link>
        </section>
      </main>

      <footer className="landing-foot">
        <span><PulseMark /> <span translate="no">Pulse</span></span>
        <nav aria-label="Legal">
          <a href="/privacidad">Privacidad</a>
          <a href="/terminos">Términos</a>
          <a href="/about" hrefLang="en" lang="en">English</a>
        </nav>
      </footer>
    </div>
  )
}
