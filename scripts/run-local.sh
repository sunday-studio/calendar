#!/usr/bin/env bash
# Run the real hub locally with persistent data, logs on disk and an issue log.
#   scripts/run-local.sh                 build + run the hub (default port 8787, loopback)
#   scripts/run-local.sh token [name]    create an admin device token (prints it once)
#   scripts/run-local.sh doctor          health check against the local database
#   scripts/run-local.sh issue "title"   append an entry to .local/ISSUES.md with the latest warnings/errors
#   scripts/run-local.sh logs            follow today's log
#   scripts/run-local.sh demo            run with FAKE Google data (2 accounts, realistic events) in .local/demo/, separate from your real data
# Everything lives in .local/ (git-ignored): database, master key, backups, logs, google_client.json.
# Env: PORT (8787), LAN=1 (listen on all interfaces so a phone on the same Wi-Fi can reach it),
#      CALENDAR_PUBLIC_URL (needed for Google sign-in; derived from Tailscale when available),
#      any OTEL_* variable (telemetry passes straight through).
set -euo pipefail
cd "$(dirname "$0")/.."
D=".local"; LOGS="$D/logs"; BIN="$D/calendard"
mkdir -p "$LOGS" "$D/backups"
LOG="$LOGS/calendard-$(date +%F).log"

if [ ! -f "$D/master_key" ]; then ( umask 077; head -c32 /dev/urandom | base64 > "$D/master_key" ); echo "created $D/master_key (back it up if you keep this data)" >&2; fi
chmod 600 "$D/master_key"

PORT="${PORT:-8787}"
HOST=127.0.0.1; [ "${LAN:-0}" = "1" ] && HOST=0.0.0.0
export CALENDAR_ADDR="${CALENDAR_ADDR:-$HOST:$PORT}"
export CALENDAR_DB="$D/calendar.db"
export CALENDAR_MASTER_KEY_FILE="$D/master_key"
export CALENDAR_BACKUP_DIR="$D/backups"
if [ -f "$D/google_client.json" ]; then
  export CALENDAR_GOOGLE_CLIENT_FILE="$D/google_client.json"
  if [ -z "${CALENDAR_PUBLIC_URL:-}" ] && command -v tailscale >/dev/null 2>&1; then
    dns="$(tailscale status --json 2>/dev/null | python3 -c 'import sys,json; print(json.load(sys.stdin)["Self"]["DNSName"].rstrip("."))' 2>/dev/null || true)"
    [ -n "$dns" ] && export CALENDAR_PUBLIC_URL="https://$dns"
  fi
  [ -n "${CALENDAR_PUBLIC_URL:-}" ] || echo "warning: google_client.json found but CALENDAR_PUBLIC_URL is not set; Google sign-in will not start" >&2
fi

build() { ( cd server && go build -trimpath -o "../$BIN" ./cmd/calendard ); }

cmd="${1:-serve}"; [ $# -gt 0 ] && shift || true
case "$cmd" in
  serve)
    port="${CALENDAR_ADDR##*:}"
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
      echo "port $port is busy:"; lsof -nP -iTCP:"$port" -sTCP:LISTEN | tail -n +2 | awk '{print "  "$1" pid "$2}'
      echo "free it, or run: PORT=8790 scripts/run-local.sh"; exit 1
    fi
    build
    echo "hub: http://${CALENDAR_ADDR}   logs: $LOG   data: $D/" >&2
    [ -n "${CALENDAR_PUBLIC_URL:-}" ] && echo "public URL: $CALENDAR_PUBLIC_URL" >&2
    exec "$BIN" serve 2>&1 | tee -a "$LOG" ;;
  demo)
    # Realistic data without Google: the fake Google from the e2e tests + the real hub, real sync, own database.
    DD="$D/demo"; mkdir -p "$DD/logs" "$DD/backups"; LOG="$DD/logs/calendard-$(date +%F).log"
    if [ ! -f "$DD/master_key" ]; then ( umask 077; head -c32 /dev/urandom | base64 > "$DD/master_key" ); fi
    unset CALENDAR_GOOGLE_CLIENT_FILE
    FP="${FAKE_PORT:-8788}"
    export CALENDAR_DB="$DD/calendar.db" CALENDAR_MASTER_KEY_FILE="$DD/master_key" CALENDAR_BACKUP_DIR="$DD/backups"
    export CALENDAR_DEV=1 CALENDAR_SYNC_FAST=15s CALENDAR_SYNC_SLOW=30s
    export CALENDAR_GOOGLE_AUTH_URL="http://127.0.0.1:$FP/auth" CALENDAR_GOOGLE_TOKEN_URL="http://127.0.0.1:$FP/token"
    export CALENDAR_GOOGLE_API_BASE="http://127.0.0.1:$FP" CALENDAR_GOOGLE_USERINFO_URL="http://127.0.0.1:$FP/userinfo"
    port="${CALENDAR_ADDR##*:}"
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
      echo "port $port is busy:"; lsof -nP -iTCP:"$port" -sTCP:LISTEN | tail -n +2 | awk '{print "  "$1" pid "$2}'
      echo "stop that process or use PORT=..."; exit 1
    fi
    build; ( cd server && go build -trimpath -o "../$D/fakegoogle" ./cmd/fakegoogle )
    if ! lsof -nP -iTCP:"$FP" -sTCP:LISTEN >/dev/null 2>&1; then
      "$D/fakegoogle" --addr "127.0.0.1:$FP" --accounts work@example.com,personal@example.com >"$DD/fakegoogle.log" 2>&1 &
      FAKE_PID=$!; trap 'kill $FAKE_PID 2>/dev/null || true' EXIT
      for _ in $(seq 1 30); do lsof -nP -iTCP:"$FP" -sTCP:LISTEN >/dev/null 2>&1 && break; sleep 0.5; done
    fi
    "$BIN" dev seed-account --provider google --email work@example.com --name Work >/dev/null
    "$BIN" dev seed-account --provider google --email personal@example.com --name Personal >/dev/null
    if [ ! -f "$DD/token" ]; then ( umask 077; "$BIN" token create --name iphone-demo --kind device --scope admin 2>&1 | grep -o 'cal_[A-Za-z0-9_-]*' | head -1 > "$DD/token" ); fi
    echo "DEMO hub (fake Google data): http://${CALENDAR_ADDR}   logs: $LOG" >&2
    echo "token: $(cat "$DD/token")   (also in $DD/token)" >&2
    "$BIN" serve 2>&1 | tee -a "$LOG" ;;
  token)   build; exec "$BIN" token create --name "${1:-iphone}" --kind device --scope admin ;;
  doctor)  build; exec "$BIN" doctor ;;
  logs)    exec tail -n 100 -f "$LOG" ;;
  issue)
    title="${1:?usage: scripts/run-local.sh issue \"what went wrong\"}"
    f="$D/ISSUES.md"
    [ -f "$f" ] || printf '# Local issues\n\nOne entry per problem. Newest last. Paste this file to the coordinator to get them fixed.\n' > "$f"
    {
      printf '\n## %s  %s\n\n- **What happened:** %s\n- **Expected:** \n- **Steps / where:** \n- **Device / app build:** \n- **Trace id (if shown):** \n\n<details><summary>latest warnings and errors from the hub log</summary>\n\n```\n' "$(date '+%Y-%m-%d %H:%M')" "$title" "$title"
      grep -hE '"level":"(WARN|ERROR)"' "$LOGS"/*.log 2>/dev/null | tail -n 25 | cut -c1-400 || true
      printf '```\n</details>\n'
    } >> "$f"
    echo "added to $f" ;;
  *) echo "unknown command: $cmd (serve|demo|token|doctor|logs|issue)"; exit 2 ;;
esac
