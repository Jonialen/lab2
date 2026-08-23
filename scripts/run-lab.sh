#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
RECEIVER_DIR="$PROJECT_ROOT/receiver-go"
SENDER_MANIFEST="$PROJECT_ROOT/sender-rust/Cargo.toml"

HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-9000}"
STARTUP_TIMEOUT_SECONDS="${STARTUP_TIMEOUT_SECONDS:-10}"

command -v go >/dev/null 2>&1 || { printf 'Error: go is required but was not found in PATH.\n' >&2; exit 127; }
command -v cargo >/dev/null 2>&1 || { printf 'Error: cargo is required but was not found in PATH.\n' >&2; exit 127; }
command -v timeout >/dev/null 2>&1 || { printf 'Error: timeout is required but was not found in PATH.\n' >&2; exit 127; }

case "$PORT" in
  ''|*[!0-9]*) printf 'Error: PORT must be an integer from 1 to 65535.\n' >&2; exit 2 ;;
esac
if (( PORT < 1 || PORT > 65535 )); then
  printf 'Error: PORT must be an integer from 1 to 65535.\n' >&2
  exit 2
fi
case "$STARTUP_TIMEOUT_SECONDS" in
  ''|*[!0-9]*) printf 'Error: STARTUP_TIMEOUT_SECONDS must be a positive integer.\n' >&2; exit 2 ;;
esac
if (( STARTUP_TIMEOUT_SECONDS < 1 )); then
  printf 'Error: STARTUP_TIMEOUT_SECONDS must be a positive integer.\n' >&2
  exit 2
fi

BUILD_DIR="$(mktemp -d "${TMPDIR:-/tmp}/lab2-run.XXXXXXXX")"
RECEIVER_BIN="$BUILD_DIR/lab2-receiver"
RECEIVER_LOG="$BUILD_DIR/receiver.log"
RECEIVER_PID=''

cleanup() {
  if [[ -n "$RECEIVER_PID" ]] && kill -0 "$RECEIVER_PID" 2>/dev/null; then
    kill "$RECEIVER_PID" 2>/dev/null || true
    wait "$RECEIVER_PID" 2>/dev/null || true
  fi
  rm -rf -- "$BUILD_DIR"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf 'Building Go receiver...\n'
(cd -- "$RECEIVER_DIR" && go build -o "$RECEIVER_BIN" .)
"$RECEIVER_BIN" -host "$HOST" -port "$PORT" >"$RECEIVER_LOG" 2>&1 &
RECEIVER_PID=$!

deadline=$((SECONDS + STARTUP_TIMEOUT_SECONDS))
while true; do
  if ! kill -0 "$RECEIVER_PID" 2>/dev/null; then
    printf 'Error: Go receiver exited during startup.\n' >&2
    printf '%s\n' '--- receiver log ---' >&2
    command cat "$RECEIVER_LOG" >&2
    exit 1
  fi
  if timeout --foreground 1 bash -c 'exec 3<>"/dev/tcp/$1/$2"' _ "$HOST" "$PORT" 2>/dev/null; then
    # Ensure readiness came from this process rather than a pre-existing listener.
    sleep 0.1
    if kill -0 "$RECEIVER_PID" 2>/dev/null; then
      break
    fi
    printf 'Error: Go receiver exited after the readiness probe.\n' >&2
    printf '%s\n' '--- receiver log ---' >&2
    command cat "$RECEIVER_LOG" >&2
    exit 1
  fi
  if (( SECONDS >= deadline )); then
    printf 'Error: receiver did not listen on %s:%s within %s seconds.\n' "$HOST" "$PORT" "$STARTUP_TIMEOUT_SECONDS" >&2
    printf '%s\n' '--- receiver log ---' >&2
    command cat "$RECEIVER_LOG" >&2
    exit 1
  fi
  sleep 0.1
done

printf 'Receiver ready on %s:%s (PID %s).\n' "$HOST" "$PORT" "$RECEIVER_PID"
if (( $# == 0 )); then
  cargo run --manifest-path "$SENDER_MANIFEST"
else
  cargo run --manifest-path "$SENDER_MANIFEST" -- "$@" --host "$HOST" --port "$PORT"
fi
