#!/usr/bin/env bash
# Levanta Pulse con una base y una clave nuevas para los tests E2E.
set -euo pipefail
cd "$(dirname "$0")/.."
DIR="$PWD/.e2e"
rm -rf "$DIR" && mkdir -p "$DIR"
(cd .. && go build -o "$DIR/pulse" ./cmd/pulse)
"$DIR/pulse" genkeys --out "$DIR/secrets" > /dev/null
export PULSE_ENV=development PULSE_BASE_URL="http://localhost:$PORT" PULSE_LISTEN=":$PORT" \
  PULSE_DB_PATH="$DIR/pulse.db" PULSE_KEK_FILE="$DIR/secrets/pulse_kek" TZ=America/Guayaquil PULSE_REGISTER_PER_HOUR=1000
exec "$DIR/pulse" serve
