// Aplica el tema antes de pintar para evitar el destello claro→oscuro.
// Archivo externo (no en línea) para cumplir la CSP estricta.
try {
  var t = localStorage.getItem('pulse-theme')
  if (t === 'light' || t === 'dark') document.documentElement.dataset.theme = t
} catch (e) {}
