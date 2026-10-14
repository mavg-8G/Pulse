#!/usr/bin/env bash
# Despliegue de Pulse en el VPS. Lo ejecuta GitHub Actions por SSH con una clave que SOLO
# puede correr este script (comando forzado en authorized_keys del usuario "deploy").
#
#   deploy <sha256:digest> <commit>   imagen nueva: verifica firma, respalda, arranca, prueba;
#                                      si algo falla, restaura la base y vuelve a la versión anterior
#   status                             qué versión corre y su estado
#
# Instalación y requisitos: deploy/CD.md. Este archivo vive en /opt/docker/pulse/deploy.sh,
# propiedad de root y sin permiso de escritura para "deploy".
set -Eeuo pipefail
umask 077

# ---- Ajustes ----------------------------------------------------------------
DIR="${PULSE_DEPLOY_DIR:-/opt/docker/pulse}" # la variable solo sirve para probar el script
REPO_IMAGE=ghcr.io/mavg-8g/pulse
URL=https://pulse.espectro.uk
# Solo se aceptan imágenes firmadas por el workflow de main de este repositorio.
SIGNER='https://github.com/mavg-8G/Pulse/.github/workflows/ci.yml@refs/heads/main'
ISSUER=https://token.actions.githubusercontent.com
REQUIRE_SIGNATURE=1 # 0 solo mientras instalas cosign; ver deploy/CD.md
KEEP_BACKUPS=15
# ------------------------------------------------------------------------------

cd "$DIR"
LOG="$DIR/deploy.log"
log() { printf '%s %s\n' "$(date -u +%FT%TZ)" "$*" | tee -a "$LOG"; }
die() { log "✗ $*"; exit 1; }

exec 9>"$DIR/.deploy.lock"
flock -n 9 || die "ya hay un despliegue en curso"

# Con comando forzado, los argumentos llegan en SSH_ORIGINAL_COMMAND (nunca se evalúan como shell).
read -r -a ARGS <<<"${SSH_ORIGINAL_COMMAND:-$*}"
ACTION="${ARGS[0]:-}"

compose() { docker compose --env-file "$DIR/release.env" "$@"; }
current_image() { sed -n 's/^PULSE_IMAGE=//p' "$DIR/release.env" 2>/dev/null || true; }
health() { docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' pulse 2>/dev/null || echo ausente; }

case "$ACTION" in
status)
  echo "imagen:  $(current_image)"
  sed -n 's/^PULSE_COMMIT=/commit:  /p' "$DIR/release.env" 2>/dev/null || true
  echo "estado:  $(health)"
  tail -n 5 "$LOG" 2>/dev/null || true
  exit 0
  ;;
deploy) ;;
*) die "acción no permitida: '${ACTION}' (deploy | status)" ;;
esac

DIGEST="${ARGS[1]:-}"
COMMIT="${ARGS[2]:-}"
[[ ${#ARGS[@]} -eq 3 && "$DIGEST" =~ ^sha256:[a-f0-9]{64}$ && "$COMMIT" =~ ^[0-9a-f]{40}$ ]] ||
  die "uso: deploy sha256:<64 hex> <commit de 40 hex>"
NEW="$REPO_IMAGE@$DIGEST"
PREV="$(current_image)"
[[ "$NEW" == "$PREV" && "$(health)" == healthy ]] && { log "= ${COMMIT:0:7} ya está desplegado"; exit 0; }

log "→ desplegando ${COMMIT:0:7} ($DIGEST)"
docker pull -q "$NEW" >/dev/null || die "no se pudo descargar $NEW"

if command -v cosign >/dev/null; then
  cosign verify --certificate-identity "$SIGNER" --certificate-oidc-issuer "$ISSUER" "$NEW" >/dev/null 2>>"$LOG" ||
    die "la firma de la imagen NO es válida: no se despliega"
  log "✓ firma verificada"
elif [[ "$REQUIRE_SIGNATURE" == 1 ]]; then
  die "cosign no está instalado y se exige firma (ver deploy/CD.md)"
else
  log "! cosign no instalado: se despliega SIN verificar la firma"
fi

# Respaldo en caliente antes de que la versión nueva aplique migraciones.
BACKUP=""
if [[ "$(health)" == healthy ]]; then
  BACKUP="/data/backups/$(date -u +%Y%m%dT%H%M%SZ)-antes-de-${COMMIT:0:7}.db"
  docker exec pulse /pulse backup --keep "$KEEP_BACKUPS" "$BACKUP" >>"$LOG" 2>&1 || die "falló el respaldo: no se despliega"
  log "✓ respaldo $BACKUP"
fi

[[ -f "$DIR/release.env" ]] && cp "$DIR/release.env" "$DIR/release.prev"
printf 'PULSE_IMAGE=%s\nPULSE_COMMIT=%s\n' "$NEW" "$COMMIT" >"$DIR/release.env.tmp"
mv "$DIR/release.env.tmp" "$DIR/release.env"

rollback() {
  log "✗ $1: volviendo a la versión anterior"
  compose logs --no-color --tail 50 pulse >>"$LOG" 2>&1 || true
  if [[ -z "$PREV" ]]; then
    compose stop pulse >/dev/null 2>&1 || true
    die "primer despliegue fallido; Pulse queda detenido (revisa deploy.log)"
  fi
  compose stop pulse >/dev/null 2>&1 || true
  if [[ -n "$BACKUP" ]]; then
    # La versión nueva pudo migrar la base; la anterior no arrancaría sobre un esquema más nuevo.
    compose run --rm --no-deps pulse restore "$BACKUP" >>"$LOG" 2>&1 || die "no se pudo restaurar $BACKUP (Pulse detenido)"
    log "✓ base restaurada desde $BACKUP"
  fi
  mv "$DIR/release.prev" "$DIR/release.env"
  compose up -d --wait --wait-timeout 90 pulse >>"$LOG" 2>&1 || die "la versión anterior tampoco arrancó (Pulse detenido)"
  die "despliegue de ${COMMIT:0:7} revertido; sigue la versión anterior"
}

compose up -d --wait --wait-timeout 120 pulse >>"$LOG" 2>&1 || rollback "la versión nueva no quedó sana"
"$DIR/smoke.sh" "$URL" >>"$LOG" 2>&1 || rollback "la prueba de humo falló"

# Imágenes viejas de Pulse sin usar (más de 14 días): las recientes quedan para volver atrás.
docker image prune -af --filter "label=org.opencontainers.image.source=https://github.com/mavg-8G/Pulse" --filter "until=336h" >/dev/null 2>&1 || true
log "✓ ${COMMIT:0:7} desplegado y sano"
