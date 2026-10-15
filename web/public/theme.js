// Aplica el tema antes de pintar para evitar el destello claro→oscuro.
// Archivo externo (no en línea) para cumplir la CSP estricta.
try {
  var t = localStorage.getItem('pulse-theme')
  if (t === 'light' || t === 'dark') {
    document.documentElement.dataset.theme = t
    document.addEventListener('DOMContentLoaded', function () {
      var c = t === 'dark' ? '#121c27' : '#f3f5f8'
      document.querySelectorAll('meta[name="theme-color"]').forEach(function (m) { m.content = c })
    })
  }
} catch (e) {}
