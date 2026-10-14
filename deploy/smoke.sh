#!/usr/bin/env bash
# Prueba de humo tras cada despliegue: ./deploy/smoke.sh https://pulse.espectro.uk
set -euo pipefail
URL="${1:-https://pulse.espectro.uk}"
fail() { echo "✗ $1"; exit 1; }
ok() { echo "✓ $1"; }

curl -fsS "$URL/healthz" | grep -q '"ok"' && ok "healthz responde" || fail "healthz"
H=$(curl -fsSI "$URL/")
grep -qi "^content-security-policy: default-src 'self'" <<<"$H" && ok "CSP presente" || fail "falta CSP"
grep -qi "^strict-transport-security: max-age=" <<<"$H" && ok "HSTS presente" || fail "falta HSTS"
grep -qi "^x-frame-options: DENY" <<<"$H" && ok "X-Frame-Options" || fail "falta X-Frame-Options"
code=$(curl -s -o /dev/null -w '%{http_code}' "$URL/api/me")
[ "$code" = "401" ] && ok "/api/me sin sesión → 401" || fail "/api/me devolvió $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -H 'Origin: https://evil.example' -d '{}' "$URL/api/auth/login")
[ "$code" = "403" ] && ok "CSRF bloqueado" || fail "CSRF devolvió $code"
curl -fsS "$URL/manifest.webmanifest" | grep -q '"standalone"' && ok "manifest PWA" || fail "manifest"
code=$(curl -s -o /dev/null -w '%{http_code}' "http://${URL#https://}/")
[[ "$code" =~ ^30[18]$ ]] && ok "http redirige a https" || fail "http devolvió $code"
echo "Todo en orden."
