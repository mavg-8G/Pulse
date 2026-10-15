#!/usr/bin/env bash
# Respaldo cifrado fuera del VPS: respaldo en caliente → cifrado con age → copia a Nextcloud (WebDAV).
# Se programa con cron (ver deploy/README.md, «Respaldos fuera del servidor»). Configuración en backup.env:
#   AGE_RECIPIENT=age1…                                   clave pública; la privada NO va al VPS
#   NEXTCLOUD_URL=https://nube.ejemplo/remote.php/dav/files/<usuario>/pulse-backups
#   NEXTCLOUD_USER=<usuario>
#   NEXTCLOUD_PASS_FILE=./secrets/nextcloud_app_password  contraseña de aplicación de Nextcloud
set -euo pipefail
cd "${PULSE_DIR:-/opt/docker/pulse}"
[ -f backup.env ] && . ./backup.env
: "${AGE_RECIPIENT:?falta AGE_RECIPIENT en backup.env}" "${NEXTCLOUD_URL:?falta NEXTCLOUD_URL}" "${NEXTCLOUD_USER:?falta NEXTCLOUD_USER}"
PASS_FILE="${NEXTCLOUD_PASS_FILE:-./secrets/nextcloud_app_password}"
[ -r "$PASS_FILE" ] || { echo "✗ no se puede leer $PASS_FILE" >&2; exit 1; }
command -v age >/dev/null || { echo "✗ instala age (apt install age)" >&2; exit 1; }

name="$(date -u +%Y%m%dT%H%M%SZ)-fuera.db"
plain="data/backups/$name"
trap 'rm -f "$plain" "$plain.age"' EXIT # nada sin cifrar se queda en disco, pase lo que pase
docker exec pulse /pulse backup "/data/backups/$name" >/dev/null
age -r "$AGE_RECIPIENT" -o "$plain.age" "$plain"
# Credenciales por stdin (-K -): no aparecen en la lista de procesos.
printf 'user = "%s:%s"\n' "$NEXTCLOUD_USER" "$(cat "$PASS_FILE")" |
  curl -fsS --retry 3 --retry-all-errors -K - -T "$plain.age" "${NEXTCLOUD_URL%/}/$name.age"
echo "✓ respaldo cifrado subido: $name.age"
# shortcut: no borra respaldos antiguos en Nextcloud; usa allí una regla de retención o bórralos a mano.
