#!/bin/bash
# Runs the Go version locally on http://localhost:${PORT:-8080}/webnote/, with
# an in-memory Datastore emulator and the static files app.yaml serves.
# The data is gone when the script stops. Smoke-test it with:
#   python smoketest.py http://localhost:8080 --max-wait 30
set -euo pipefail
cd "$(dirname "$0")"

EMULATOR_PORT=${EMULATOR_PORT:-8462}
export DATASTORE_EMULATOR_HOST=localhost:$EMULATOR_PORT
export DATASTORE_PROJECT_ID=webnote-local
export PORT=${PORT:-8080}
export WEBNOTE_SERVE_STATIC=1

BIN=$(mktemp)
go build -o "$BIN" .

# setsid puts the emulator (gcloud and its Java child) in its own process
# group, so it can all be stopped together.
setsid gcloud beta emulators datastore start --no-store-on-disk \
    --use-firestore-in-datastore-mode --host-port="$DATASTORE_EMULATOR_HOST" \
    --project="$DATASTORE_PROJECT_ID" --quiet &
EMULATOR_PID=$!
SERVER_PID=
cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null
  kill -- -"$EMULATOR_PID" 2>/dev/null
  rm -f "$BIN"
}
trap cleanup EXIT
trap 'exit 130' INT TERM
until curl -s "http://$DATASTORE_EMULATOR_HOST/" >/dev/null; do sleep 0.5; done

"$BIN" &
SERVER_PID=$!
wait "$SERVER_PID"
