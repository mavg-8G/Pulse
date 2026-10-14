#!/usr/bin/env bash
# Evita el instalador remoto mutable: descarga una release oficial con hash fijado en CI.
set -Eeuo pipefail

: "${TRIVY_VERSION:?falta TRIVY_VERSION}"
: "${TRIVY_SHA256:?falta TRIVY_SHA256}"
: "${RUNNER_TEMP:?falta RUNNER_TEMP}"
: "${GITHUB_PATH:?falta GITHUB_PATH}"
[[ "$TRIVY_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Versión de Trivy inválida' >&2; exit 1; }
[[ "$TRIVY_SHA256" =~ ^[a-f0-9]{64}$ ]] || { echo 'Hash de Trivy inválido' >&2; exit 1; }
[[ "$(uname -s)" == Linux && "$(uname -m)" == x86_64 ]] || { echo 'Este instalador requiere Linux x86_64' >&2; exit 1; }

workdir=$(mktemp -d "$RUNNER_TEMP/pulse-trivy.XXXXXX")
trap 'rm -rf -- "$workdir"' EXIT
archive="$workdir/trivy.tar.gz"
asset="trivy_${TRIVY_VERSION#v}_Linux-64bit.tar.gz"
curl --fail --show-error --location --retry 5 --retry-all-errors \
  --connect-timeout 20 --max-time 180 \
  "https://github.com/aquasecurity/trivy/releases/download/$TRIVY_VERSION/$asset" -o "$archive"
printf '%s  %s\n' "$TRIVY_SHA256" "$archive" | sha256sum --check --strict -
tar -xzf "$archive" -C "$workdir" trivy
install_dir="$RUNNER_TEMP/pulse-tools"
install -d "$install_dir"
install -m 755 "$workdir/trivy" "$install_dir/trivy"
printf '%s\n' "$install_dir" >> "$GITHUB_PATH"
"$install_dir/trivy" --version
