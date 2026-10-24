#!/usr/bin/env bash
# Run the real hub locally with persistent data, logs on disk and an issue log.
#   scripts/run-local.sh                 build + run the hub (default port 8787, loopback)
#   scripts/run-local.sh token [name]    create an admin device token (prints it once)
#   scripts/run-local.sh doctor          health check against the local database
#   scripts/run-local.sh issue "title"   append an entry to .local/ISSUES.md with the latest warnings/errors
#   scripts/run-local.sh logs            follow today's log
#   scripts/run-local.sh demo            run with FAKE Google data (rich profile: 4 accounts, every UI state, docs/ops/demo-data.md) in .local/demo/, separate from your real data
#   scripts/run-local.sh demo-reset      delete the demo database (refuses while a hub listens on the port; never stops anything)
#   scripts/run-local.sh scenario NAME   sync states of the demo's fake Google: reauth, google-down, rate-limit, slow, outside-move, burst, ... (scenario list)
# Everything lives in .local/ (git-ignored): database, master key, backups, logs, google_client.json.
# Env: PORT (8787), LAN=1 (listen on all interfaces so a phone on the same Wi-Fi can reach it),
#      CALENDAR_PUBLIC_URL (needed for Google sign-in; derived from Tailscale when available),
#      any OTEL_* variable (telemetry passes straight through); or put them in .local/otel.env (loaded automatically).
set -euo pipefail
cd "$(dirname "$0")/.."
D=".local"; LOGS="$D/logs"; BIN="$D/calendard"
mkdir -p "$LOGS" "$D/backups"
LOG="$LOGS/calendard-$(date +%F).log"

if [ ! -f "$D/master_key" ]; then ( umask 077; head -c32 /dev/urandom | base64 > "$D/master_key" ); echo "created $D/master_key (back it up if you keep this data)" >&2; fi
chmod 600 "$D/master_key"
# Optional local telemetry settings (git-ignored): OTEL_* variables for the hub, e.g. an Orion/collector endpoint.
if [ -f "$D/otel.env" ]; then set -a; . "$D/otel.env"; set +a; fi

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
    # Rich demo data without Google: the fake Google (--profile rich, see docs/ops/demo-data.md) + the real hub,
    # real sync, own database. DEMO_PROFILE=e2e gives the small dataset of the e2e tests instead.
    DD="${DEMO_DIR:-$D/demo}"; mkdir -p "$DD/logs" "$DD/backups"; LOG="$DD/logs/calendard-$(date +%F).log"
    PROFILE="${DEMO_PROFILE:-rich}"
    if [ "$PROFILE" = rich ]; then DEMO_ACCOUNTS="work personal side stale"; else DEMO_ACCOUNTS="work personal"; fi
    # a database made with another profile would mix old and new events: ask for demo-reset instead
    if [ -f "$DD/calendar.db" ] && [ "$(cat "$DD/profile" 2>/dev/null || echo e2e)" != "$PROFILE" ]; then
      echo "the demo database in $DD/ was made with the '$(cat "$DD/profile" 2>/dev/null || echo e2e)' dataset, not '$PROFILE'."
      echo "stop the demo hub and fake Google if they run, then:  scripts/run-local.sh demo-reset   (deletes only $DD/calendar.db)"; exit 1
    fi
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
      fargs=(--addr "127.0.0.1:$FP" --profile "$PROFILE"); [ -n "${DEMO_TZ:-}" ] && fargs+=(--tz "$DEMO_TZ")
      [ "$PROFILE" = rich ] || fargs+=(--accounts work@example.com,personal@example.com)
      "$D/fakegoogle" "${fargs[@]}" >"$DD/fakegoogle.log" 2>&1 &
      FAKE_PID=$!; trap 'kill $FAKE_PID 2>/dev/null || true' EXIT
      for _ in $(seq 1 30); do lsof -nP -iTCP:"$FP" -sTCP:LISTEN >/dev/null 2>&1 && break; sleep 0.5; done
    elif ! curl -fsS "http://127.0.0.1:$FP/_admin/summary" 2>/dev/null | grep -q "\"profile\":\"$PROFILE\""; then
      echo "warning: the fake Google already running on port $FP is not the '$PROFILE' dataset (stop it, or use FAKE_PORT=...)." >&2
      echo "         it was not started by this script, so it is left alone." >&2
    fi
    for a in $DEMO_ACCOUNTS; do "$BIN" dev seed-account --provider google --email "$a@example.com" --name "$(echo "$a" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')" >/dev/null; done
    echo "$PROFILE" > "$DD/profile"
    if [ ! -f "$DD/token" ]; then ( umask 077; "$BIN" token create --name iphone-demo --kind device --scope admin 2>&1 | grep -o 'cal_[A-Za-z0-9_-]*' | head -1 > "$DD/token" ); fi
    echo "DEMO hub ($PROFILE fake Google data, accounts: $DEMO_ACCOUNTS): http://${CALENDAR_ADDR}   logs: $LOG" >&2
    echo "token: $(cat "$DD/token")   (also in $DD/token)" >&2
    echo "what is in it and what to test: docs/ops/demo-data.md.  Sync states:  scripts/run-local.sh scenario list" >&2
    "$BIN" serve 2>&1 | tee -a "$LOG" ;;
  demo-reset)
    # Forget the demo database (events, accounts, device token) so the next `demo` starts clean. Stops nothing.
    DD="${DEMO_DIR:-$D/demo}"; port="${CALENDAR_ADDR##*:}"
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
      echo "a hub is listening on port $port, not resetting:"; lsof -nP -iTCP:"$port" -sTCP:LISTEN | tail -n +2 | awk '{print "  "$1" pid "$2}'
      echo "stop it first (Ctrl-C in its terminal); this command never stops processes."; exit 1
    fi
    if ! ls "$DD"/calendar.db* >/dev/null 2>&1; then echo "nothing to reset: no database in $DD/"; exit 0; fi
    echo "This deletes the demo database in $DD/ (its accounts, events and device token). Your real data in $D/ is not touched."
    if [ "${1:-}" != "-y" ] && [ "${YES:-0}" != 1 ]; then
      read -r -p "Delete it? [y/N] " ans; case "$ans" in y|Y|yes) ;; *) echo "kept"; exit 1 ;; esac
    fi
    rm -f "$DD"/calendar.db "$DD"/calendar.db-wal "$DD"/calendar.db-shm "$DD/token" "$DD/profile"
    echo "done. The iPhone must be paired again with the new token. Start again with:"
    echo "  CALENDAR_ADDR=${CALENDAR_ADDR} scripts/run-local.sh demo"
    echo "(a fake Google still running keeps its scenario state; run 'scripts/run-local.sh scenario reset' to re-base it)" ;;
  scenario)
    # Sync-state hooks of the fake Google behind the demo (docs/ops/demo-data.md). Needs the demo running.
    FP="${FAKE_PORT:-8788}"; A="http://127.0.0.1:$FP/_admin"
    name="${1:-list}"; [ $# -gt 0 ] && shift || true
    say() { printf 'did:  %s\nlook: %s\n' "$1" "$2"; }
    post() { # post NAME JSON -> prints did/look from the fake's answer
      local out; out="$(curl -sS -X POST -H 'Content-Type: application/json' --data "$2" "$A/$1" 2>&1)" || {
        echo "cannot reach the fake Google on port $FP ($out). Is 'scripts/run-local.sh demo' running? (FAKE_PORT=... if you changed it)"; exit 1; }
      printf '%s' "$out" | python3 -c 'import sys,json
d=json.load(sys.stdin)
if not d.get("ok"): print("failed:", d.get("error", d)); sys.exit(1)
if d.get("did"): print("did:  "+d["did"]); print("look: "+d["look"])'
    }
    aj() { [ -z "${1:-}" ] || printf '"account":"%s"' "$1"; }      # "account":"x" or nothing
    acct() { [ -z "${1:-}" ] || printf ',"account":"%s"' "$1"; }   # ,"account":"x" or nothing
    case "$name" in
      reauth)         post reauth "{$(aj "${1:-}")}" ;;
      reconnect)      # the fake accepts again and the hub account is re-seeded (what Reconnect in the app does)
        a="${1:-stale@example.com}"; post recover "{}" >/dev/null
        DD="${DEMO_DIR:-$D/demo}"; [ -x "$BIN" ] || { echo "$BIN is missing: run scripts/run-local.sh demo once"; exit 1; }
        CALENDAR_DEV=1 CALENDAR_DB="$DD/calendar.db" CALENDAR_MASTER_KEY_FILE="$DD/master_key" "$BIN" dev seed-account --provider google --email "$a" >/dev/null
        say "re-seeded $a in the hub and cleared the revocation" "within one sync cycle $a is active again (Settings > Accounts), its events no longer stale." ;;
      google-down)    post down "{\"seconds\":${1:-60}}" ;;
      rate-limit)     post rate-limit "{\"seconds\":${1:-60},\"retryAfter\":\"${2:-20}\"}" ;;
      slow)           post slow "{\"ms\":${1:-1500}}" ;;
      recover)        post recover "{}" ;;
      outside-move)   post outside "{\"action\":\"move\"$(acct "${1:-}")}" ;;
      outside-rename) post outside "{\"action\":\"rename\"$(acct "${1:-}")}" ;;
      outside-delete) post outside "{\"action\":\"delete\"$(acct "${1:-}")}" ;;
      outside-add)    post outside "{\"action\":\"add\"$(acct "${1:-}")}" ;;
      expire-sync)    post expire-sync-tokens "{}" >/dev/null
        say "every sync token the fake issued now answers 410" "the next sync of each calendar re-lists everything (410 recovery); nothing should change on screen and no error shows; the sync log has a full run." ;;
      burst)          post burst "{\"n\":${1:-200}$(acct "${2:-}")}" ;;
      calendar-add)   post calendar-add "{$(aj "${1:-}")}" ;;
      calendar-remove) post calendar-remove "{$(aj "${1:-}")}" ;;
      reset)          post reset "{}" >/dev/null
        say "re-seeded the fake Google (dates re-based to now, all scenarios cleared, sync tokens expired)" "the next sync re-lists: events added by burst/outside-add disappear, moved/deleted ones are back, removed calendars return. Revoked accounts stay needs_reauth in the hub until 'scenario reconnect'." ;;
      list|"")
        curl -fsS "$A/summary" 2>/dev/null | python3 -c 'import sys,json
d=json.load(sys.stdin)
print("fake Google: profile %s, fake clock %s" % (d["profile"], d["now"]))
for e,a in d["accounts"].items():
    print("  %-22s %s, %d events%s" % (e, "REVOKED" if a["revoked"] else "ok", a["events"], ", %d failure rule(s) pending" % a["pendingFailureRules"] if a["pendingFailureRules"] else ""))
    for c in a["calendars"]: print("      %-48s %-14s %4d%s" % (c["name"][:48], c["role"], c["events"], "  (hidden)" if c["hidden"] else ""))
print("outages:", d["outages"] or "none", "| extra delay: %d ms" % d["delayMs"])' \
          || { echo "cannot reach the fake Google on port $FP. Is 'scripts/run-local.sh demo' running?"; exit 1; }
        cat <<'EOT'

scenarios:  scripts/run-local.sh scenario NAME [args]
  reauth [email]            revoke an account (default stale@example.com): needs_reauth banner
  reconnect [email]         undo reauth (what Reconnect does)
  google-down [seconds]     every Google call answers 503 (default 60 s)
  rate-limit [secs] [retry] every Google call answers 429 with Retry-After (default 60 s, 20)
  slow [ms]                 every Google answer takes this long (default 1500)
  recover                   clear outages, rate limit, slowness, revocations
  outside-move|-rename|-delete [account]   change the next upcoming event as if in Google web
  outside-add [account]     a new event in 10 minutes
  expire-sync               sync tokens answer 410 (full re-list)
  burst [n] [account]       add n (200) events over the next 30 days
  calendar-add|calendar-remove [account]   a calendar appears / disappears
  reset                     re-seed the fake (re-bases dates, undoes everything)
EOT
        ;;
      *) echo "unknown scenario: $name (run: scripts/run-local.sh scenario list)"; exit 2 ;;
    esac ;;
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
  *) echo "unknown command: $cmd (serve|demo|demo-reset|scenario|token|doctor|logs|issue)"; exit 2 ;;
esac
