#!/usr/bin/env bash
# End-to-end harness: the real hub (real DB, real sync, real write service) against a FAKE Google.
#   scripts/e2e.sh test   build, start on free ports with temp state, assert, tear down (default)
#   scripts/e2e.sh up     leave the stack running on 8787 (hub) / 8788 (fake Google); prints a dev token
#   scripts/e2e.sh down   stop what `up` started
# See docs/engineering/e2e.md. Needs go, curl, jq, python3.
set -uo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cmd="${1:-test}"

for t in go curl jq python3; do command -v "$t" >/dev/null || { echo "e2e: $t is required" >&2; exit 2; }; done

# ---------------------------------------------------------------- stack
build() { # build <bindir>
  mkdir -p "$1"
  (cd "$root/server" && CGO_ENABLED=0 go build -o "$1/calendard" ./cmd/calendard && go build -o "$1/fakegoogle" ./cmd/fakegoogle) || return 1
}

free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'; }

hub_env() { # hub_env <dir> <hub_port> <fg_port>   (exports the hub's environment)
  export CALENDAR_ADDR="127.0.0.1:$2" CALENDAR_PUBLIC_URL="http://127.0.0.1:$2"
  export CALENDAR_DB="$1/calendar.db" CALENDAR_MASTER_KEY_FILE="$1/master.key"
  export CALENDAR_DEV=1 CALENDAR_SYNC_FAST=2s CALENDAR_SYNC_SLOW=2s CALENDAR_ICS_ALLOW_PRIVATE=1
  export CALENDAR_GOOGLE_AUTH_URL="http://127.0.0.1:$3/auth" CALENDAR_GOOGLE_TOKEN_URL="http://127.0.0.1:$3/token"
  export CALENDAR_GOOGLE_API_BASE="http://127.0.0.1:$3" CALENDAR_GOOGLE_USERINFO_URL="http://127.0.0.1:$3/userinfo"
}

wait_http() { # wait_http <url> [header]   up to 20 s
  local i
  for i in $(seq 1 100); do
    curl -fsS ${2:+-H "$2"} "$1" >/dev/null 2>&1 && return 0
    sleep 0.2
  done
  return 1
}

# stack_up <dir> <hub_port> <fg_port>: starts both, creates the admin token (TOKEN), seeds a@ and b@.
stack_up() {
  local dir="$1" hp="$2" fp="$3"
  mkdir -p "$dir"
  [ -f "$dir/master.key" ] || head -c 32 /dev/urandom | base64 > "$dir/master.key"
  hub_env "$dir" "$hp" "$fp"
  "$dir/bin/fakegoogle" --addr "127.0.0.1:$fp" --accounts a@example.com,b@example.com >"$dir/fakegoogle.log" 2>&1 &
  echo $! > "$dir/fakegoogle.pid"
  wait_http "http://127.0.0.1:$fp/health" || { echo "e2e: fakegoogle did not start (see $dir/fakegoogle.log)" >&2; return 1; }
  TOKEN="$("$dir/bin/calendard" token create --name e2e --kind device --scope admin | sed -n 's/^secret: *//p')"
  [ -n "$TOKEN" ] || { echo "e2e: could not create a token" >&2; return 1; }
  "$dir/bin/calendard" dev seed-account --provider google --email a@example.com --name Work >/dev/null || return 1
  "$dir/bin/calendard" dev seed-account --provider google --email b@example.com --name Personal >/dev/null || return 1
  "$dir/bin/calendard" serve >"$dir/calendard.log" 2>&1 &
  echo $! > "$dir/calendard.pid"
  wait_http "http://127.0.0.1:$hp/health" || { echo "e2e: calendard did not start (see $dir/calendard.log)" >&2; return 1; }
}

stack_down() { # stack_down <dir>
  local f p
  for f in calendard fakegoogle; do
    p="$(cat "$1/$f.pid" 2>/dev/null || true)"
    [ -n "$p" ] && kill "$p" 2>/dev/null
  done
  jobs -p | xargs kill 2>/dev/null || true
  sleep 0.3
}

case "$cmd" in
  up)
    dir="${E2E_DIR:-$root/.e2e}"; hp="${E2E_HUB_PORT:-8787}"; fp="${E2E_FAKE_PORT:-8788}"
    if [ -f "$dir/calendard.pid" ] && kill -0 "$(cat "$dir/calendard.pid")" 2>/dev/null; then
      echo "e2e: already running ($dir); use 'make e2e-down' first" >&2; exit 1
    fi
    for port in "$hp" "$fp"; do
      if python3 -c 'import socket,sys;s=socket.socket();s.settimeout(0.5);sys.exit(0 if s.connect_ex(("127.0.0.1",int(sys.argv[1])))==0 else 1)' "$port"; then
        echo "e2e: port $port is already in use (set E2E_HUB_PORT / E2E_FAKE_PORT to use others)" >&2; exit 1
      fi
    done
    rm -rf "$dir"; mkdir -p "$dir"
    build "$dir/bin" || exit 1
    set +m
    stack_up "$dir" "$hp" "$fp" || { stack_down "$dir"; exit 1; }
    disown -a 2>/dev/null || true
    printf 'CALENDAR_E2E_URL=http://127.0.0.1:%s\nCALENDAR_E2E_TOKEN=%s\nFAKE_GOOGLE=http://127.0.0.1:%s\n' "$hp" "$TOKEN" "$fp" > "$dir/env"
    echo "hub          http://127.0.0.1:$hp"
    echo "fake google  http://127.0.0.1:$fp   (admin: POST /_admin/{fail,reset,seed,etag-bump,revoke,expire-sync-tokens}, GET /_admin/state)"
    echo "dev token    $TOKEN"
    echo "accounts     a@example.com, b@example.com (first sync runs now)"
    echo "state dir    $dir   (stop with: make e2e-down)"
    exit 0 ;;
  down)
    dir="${E2E_DIR:-$root/.e2e}"
    stack_down "$dir"; rm -rf "$dir"; echo "stopped"; exit 0 ;;
  test) ;;
  *) echo "usage: e2e.sh test|up|down" >&2; exit 2 ;;
esac

# ---------------------------------------------------------------- test run
work="$(mktemp -d "${TMPDIR:-/tmp}/calendar-e2e.XXXXXX")"
cleanup() { stack_down "$work"; if [ "${KEEP:-0}" = 1 ] || [ "$fails" -gt 0 ]; then echo "logs kept in $work"; else rm -rf "$work"; fi; }
fails=0; passes=0
trap cleanup EXIT

build "$work/bin" || { echo "FAIL: build"; fails=1; exit 1; }
HP="$(free_port)"; FP="$(free_port)"
stack_up "$work" "$HP" "$FP" || { fails=1; exit 1; }
H="http://127.0.0.1:$HP"; G="http://127.0.0.1:$FP"

pass() { passes=$((passes+1)); printf '  ok    %s\n' "$1"; }
fail() { fails=$((fails+1)); printf '  FAIL  %s%s\n' "$1" "${2:+  ($2)}"; }
section() { printf '\n%s\n' "$1"; }

# api METHOD PATH [JSON] [extra curl args...]  -> CODE, BODY, HDRS
api() {
  local m="$1" p="$2" b="${3:-}"; shift $(( $# < 3 ? $# : 3 ))
  local args=(-sS -X "$m" -H "Authorization: Bearer $TOKEN" -o "$work/body" -D "$work/hdrs" -w '%{http_code}')
  [ -n "$b" ] && args+=(-H 'Content-Type: application/json' --data "$b")
  CODE="$(curl "${args[@]}" "$@" "$H$p" 2>"$work/curl.err")"
  BODY="$(cat "$work/body")"; HDRS="$(cat "$work/hdrs")"
}
admin() { curl -sS -X POST -H 'Content-Type: application/json' --data "${2:-{\}}" "$G/_admin/$1" >/dev/null; }
gstate() { curl -sS "$G/_admin/state?account=$1"; }

check() { # check <name> <command...>  (passes when the command succeeds)
  local n="$1"; shift
  if "$@" >/dev/null 2>&1; then pass "$n"; else fail "$n"; fi
}
eq() { # eq <name> <got> <want>
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1" "got '$2', want '$3'"; fi
}
bjq() { printf '%s' "$BODY" | jq -e "$@"; }
stop_bg() { kill "$1" 2>/dev/null; wait "$1" 2>/dev/null; return 0; }
jqb() { printf '%s' "$BODY" | jq -r "$1" 2>/dev/null; }
wait_for() { # wait_for <secs> <command...>
  local n=$(( $1 * 2 )) i; shift
  for i in $(seq 1 "$n"); do "$@" >/dev/null 2>&1 && return 0; sleep 0.5; done
  return 1
}

# time helpers (Europe/London wall clock, like the seed)
local_dt() { python3 -c 'import sys,datetime,zoneinfo
d=datetime.datetime.now(zoneinfo.ZoneInfo("Europe/London")).date()+datetime.timedelta(days=int(sys.argv[1]))
print(d.isoformat()+"T"+sys.argv[2])' "$1" "$2"; }
utc_iso() { python3 -c 'import sys,datetime
print(datetime.datetime.fromisoformat(sys.argv[1]).astimezone(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))' "$1"; }
FROM="$(python3 -c 'import datetime;print((datetime.datetime.now(datetime.timezone.utc)-datetime.timedelta(days=2)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
TO="$(python3 -c 'import datetime;print((datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=30)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"

agenda() { api GET "/v1/agenda?from=$FROM&to=$TO"; }
has_title() { agenda; printf '%s' "$BODY" | jq -e --arg t "$1" '[.items[]|select(.title==$t)]|length>0' >/dev/null; }
no_title() { agenda; printf '%s' "$BODY" | jq -e --arg t "$1" '[.items[]|select(.title==$t)]|length==0' >/dev/null; }
item() { agenda; printf '%s' "$BODY" | jq -c --arg t "$1" '[.items[]|select(.title==$t)][0]'; }
acct_status() { api GET /v1/accounts; printf '%s' "$BODY" | jq -r --arg e "$1" '.items[]|select(.email==$e)|.status'; }
acct_id() { api GET /v1/accounts; printf '%s' "$BODY" | jq -r --arg e "$1" '.items[]|select(.email==$e)|.id'; }
sync_now() { api POST /v1/sync '{}'; }
gcount() { gstate "$1" | jq --arg c "$2" --arg t "$3" '[.[]|.calendars[$c][]?|select(.summary==$t)]|length'; }
gfield() { gstate "$1" | jq -r --arg c "$2" --arg t "$3" "[.[]|.calendars[\$c][]?|select(.summary==\$t)][0]|$4"; }

# ---------------------------------------------------------------- scenarios
section "first sync"
wait_for 40 test "$(acct_status a@example.com)" = active && wait_for 40 test "$(acct_status b@example.com)" = active
eq "both accounts reach active" "$(acct_status a@example.com)/$(acct_status b@example.com)" "active/active"
api GET /v1/calendars
eq "calendars: 3 + 2 discovered" "$(jqb '.items|length')" "5"
cal_id() { printf '%s' "$BODY" | jq -r --arg n "$1" '.items[]|select(.name==$n)|.id' | head -1; }
CAL_WORK="$(cal_id 'Alex (Work)')"; CAL_TEAM="$(cal_id Team)"; CAL_PERS="$(cal_id Personal)"
A_GCAL="a@example.com"; B_GCAL="b@example.com"
check "calendar ids resolved" test -n "$CAL_WORK" -a -n "$CAL_TEAM" -a -n "$CAL_PERS"

section "agenda shows the seeded events"
agenda
eq "agenda envelope has rev" "$(jqb '(.rev|type)')" "number"
for t in "Design review" "Planning" "1:1 with Sam" "Quarterly sync" "Lunch" "Team retro" "Dentist" "Call with Mum" "Family dinner" "Standup (moved)"; do
  check "has '$t'" has_title "$t"
done
eq "all-day event is a 2-day span" "$(jqb '[.items[]|select(.title=="Team offsite")][0]|.allDay and (.date!=.endDate)')" "true"
check "birthday is all-day" bjq '[.items[]|select(.title|startswith("Kwame"))][0].allDay'
eq "events come from both accounts" "$(jqb '[.items[].accountId]|unique|length')" "2"
eq "recurring Standup expanded (>=4 occurrences)" "$(jqb '[.items[]|select(.title=="Standup")]|length>=4')" "true"
eq "Standup flagged recurring" "$(jqb '[.items[]|select(.title=="Standup")][0].recurring')" "true"
eq "exception: moved occurrence appears once" "$(jqb '[.items[]|select(.title=="Standup (moved)")]|length')" "1"
CANCELLED="$(gstate a@example.com | jq -r --arg c "$A_GCAL" '[.[]|.calendars[$c][]|select(.recurringEventId=="standup" and .status=="cancelled")][0].originalStartTime.dateTime')"
CANCELLED_UTC="$(utc_iso "$CANCELLED")"
eq "exception: cancelled occurrence is absent" "$(printf '%s' "$BODY" | jq --arg s "$CANCELLED_UTC" '[.items[]|select(.title=="Standup" and .start==$s)]|length')" "0"
eq "overlap: Planning and 1:1 overlap" "$(jqb '([.items[]|select(.title=="Planning")][0]) as $p|([.items[]|select(.title=="1:1 with Sam")][0]) as $o|($p.start < $o.end and $o.start < $p.end)')" "true"
eq "invitation needs a reply" "$(jqb '[.items[]|select(.title=="Quarterly sync")][0].myResponse')" "needsAction"
eq "Meet link exposed" "$(jqb '[.items[]|select(.title=="Design review")][0].join.url|startswith("https://meet.google.com/")')" "true"
eq "invitation can be answered" "$(jqb '[.items[]|select(.title=="Quarterly sync")][0].canRSVP')" "true"
eq "read-only team calendar is not editable" "$(jqb '[.items[]|select(.title=="Team retro")][0].canEdit')" "false"

section "calendars toggle"
api PATCH "/v1/calendars/$CAL_TEAM" '{"visible":false}'; eq "hide Team: 200" "$CODE" 200
check "hidden calendar's events leave the agenda" no_title "Team retro"
api PATCH "/v1/calendars/$CAL_TEAM" '{"visible":true}'
check "shown again: events return" has_title "Team retro"

section "create"
IDEM="e2e-create-$$-$RANDOM"
CREATE="{\"calendarId\":\"$CAL_WORK\",\"title\":\"E2E created\",\"when\":{\"allDay\":false,\"start\":\"$(local_dt 4 15:00:00)\",\"tzid\":\"Europe/London\",\"durationMin\":30},\"attendees\":[\"guest@example.com\"],\"location\":\"Room 4\"}"
api POST /v1/events "$CREATE" -H "Idempotency-Key: $IDEM"
eq "create: 201" "$CODE" 201
EV="$(jqb .eventId)"; V1="$(jqb .version)"
eq "created event is in fake Google" "$(gcount a@example.com "$A_GCAL" 'E2E created')" "1"
check "created event is in the agenda" has_title "E2E created"
api POST /v1/events "$CREATE" -H "Idempotency-Key: $IDEM"
eq "idempotent replay: 201 again" "$CODE" 201
eq "idempotent replay: same event" "$(jqb .eventId)" "$EV"
check "idempotent replay header" grep -qi '^idempotent-replayed: true' <<<"$HDRS"
eq "idempotent replay: still one event at Google" "$(gcount a@example.com "$A_GCAL" 'E2E created')" "1"
api POST /v1/events "${CREATE/E2E created/Other}" -H "Idempotency-Key: $IDEM"
eq "same key, different body: 422" "$CODE" 422
admin fail '{"account":"a@example.com","method":"POST","path":"/events","code":403,"n":1}'
api POST /v1/events "${CREATE/E2E created/Doomed}" -H "Idempotency-Key: doomed-$$"
eq "provider failure on create: 502" "$CODE" 502
eq "...and nothing half-saved at Google" "$(gcount a@example.com "$A_GCAL" Doomed)" "0"
check "...or on the hub" no_title Doomed

section "patch, If-Match, conflicts"
api PATCH "/v1/events/$EV" '{"title":"E2E renamed"}' -H "If-Match: \"$V1\""
eq "patch with If-Match: 200" "$CODE" 200
V2="$(jqb .version)"
check "version moved on" test "$V2" -gt "$V1"
eq "rename reached fake Google" "$(gcount a@example.com "$A_GCAL" 'E2E renamed')" "1"
api PATCH "/v1/events/$EV" '{"title":"stale"}' -H "If-Match: \"$V1\""
eq "stale version: 412" "$CODE" 412
eq "412 carries current" "$(jqb .error.current.title)" "E2E renamed"
GID="$(gstate a@example.com | jq -r --arg c "$A_GCAL" '[.[]|.calendars[$c][]|select(.summary=="E2E renamed")][0].id')"
admin etag-bump "{\"account\":\"a@example.com\",\"cal\":\"$A_GCAL\",\"id\":\"$GID\",\"summary\":\"Changed elsewhere\"}"
api PATCH "/v1/events/$EV" '{"title":"mine"}' -H "If-Match: \"$V2\""
eq "provider copy changed: 409" "$CODE" 409
eq "409 carries the provider's current" "$(jqb .error.current.title)" "Changed elsewhere"
eq "nothing was overwritten at Google" "$(gcount a@example.com "$A_GCAL" 'Changed elsewhere')" "1"
V3="$(jqb .error.current.version)"
api PATCH "/v1/events/$EV" '{"title":"E2E final"}' -H "If-Match: \"$V3\""
eq "retry on the current version: 200" "$CODE" 200

section "rsvp and Meet"
QS="$(item 'Quarterly sync')"; QID="$(printf '%s' "$QS" | jq -r .eventId)"; QV="$(printf '%s' "$QS" | jq -r .version)"
api POST "/v1/events/$QID/rsvp" '{"response":"accepted","note":"see you"}' -H "If-Match: \"$QV\""
eq "rsvp: 200" "$CODE" 200
eq "rsvp: myResponse accepted" "$(jqb .myResponse)" "accepted"
eq "rsvp reached fake Google" "$(gstate a@example.com | jq -r --arg c "$A_GCAL" '[.[]|.calendars[$c][]|select(.summary=="Quarterly sync")][0].attendees[]|select(.self==true)|.responseStatus')" "accepted"
api POST "/v1/events/$EV/meet" ''
eq "add Meet: 200" "$CODE" 200
eq "Meet link on the event" "$(jqb '.join.url|startswith("https://meet.google.com/")')" "true"
eq "Meet link at Google" "$(gstate a@example.com | jq -r --arg c "$A_GCAL" '[.[]|.calendars[$c][]|select(.id=="'"$GID"'")][0].hangoutLink|startswith("https://meet.google.com/")')" "true"

section "delete, recurring scopes"
agenda
OCC="$(printf '%s' "$BODY" | jq -c '[.items[]|select(.title=="Standup")]|.[3]')"
OCC_ID="$(printf '%s' "$OCC" | jq -r .id)"; OCC_V="$(printf '%s' "$OCC" | jq -r .version)"; N0="$(jqb '[.items[]|select(.title=="Standup")]|length')"
api DELETE "/v1/events/$OCC_ID?scope=this" '' -H "If-Match: \"$OCC_V\""
eq "delete one occurrence: 204" "$CODE" 204
agenda
eq "only that occurrence is gone" "$(jqb '[.items[]|select(.title=="Standup")]|length')" "$((N0-1))"
check "the occurrence is gone from the agenda" bjq --arg i "$OCC_ID" '[.items[]|select(.id==$i)]|length==0'
check "series still at Google" test "$(gcount a@example.com "$A_GCAL" Standup)" -ge 1
CL="$(item Climbing)"; CLID="$(printf '%s' "$CL" | jq -r .eventId)"; CLV="$(printf '%s' "$CL" | jq -r .version)"
api DELETE "/v1/events/$CLID?scope=all" '' -H "If-Match: \"$CLV\""
eq "delete whole series: 204" "$CODE" 204
check "series gone from the agenda" no_title Climbing
eq "series gone at Google" "$(gcount b@example.com "$B_GCAL" Climbing)" "0"
api DELETE "/v1/events/$EV" '' -H "If-Match: \"$V3\""
eq "stale delete: 412" "$CODE" 412

section "revoked token flows to accounts, status and SSE"
curl -sN -H "Authorization: Bearer $TOKEN" "$H/v1/stream" > "$work/sse1" 2>/dev/null &
SSE1=$!
sleep 1
admin revoke '{"account":"a@example.com"}'
sync_now
wait_for 30 test "$(acct_status a@example.com)" = needs_reauth
eq "/v1/accounts: needs_reauth" "$(acct_status a@example.com)" "needs_reauth"
api GET /v1/status
eq "/v1/status: needs_reauth" "$(jqb '.accounts[]|select(.email=="a@example.com")|.status')" "needs_reauth"
check "SSE emitted the account event" wait_for 10 grep -q 'needs_reauth' "$work/sse1"
eq "the other account is unaffected" "$(acct_status b@example.com)" "active"
AID="$(acct_id a@example.com)"
api POST "/v1/accounts/$AID/reconnect" ''
eq "reconnect: 200 with authUrl" "$CODE" 200
AUTHURL="$(jqb .authUrl)"
admin unrevoke '{"account":"a@example.com"}'
CB="$(curl -sS -o /dev/null -w '%{redirect_url}' "$AUTHURL")"
CONNECTED="$(curl -sS -o /dev/null -w '%{redirect_url}' "$CB")"
case "$CONNECTED" in calendar://connected*account=*) pass "OAuth round trip through fake Google";; *) fail "OAuth round trip through fake Google" "$CONNECTED";; esac
wait_for 40 test "$(acct_status a@example.com)" = active
eq "account active again after reconnect" "$(acct_status a@example.com)" "active"
check "events still there" has_title "Design review"
stop_bg "$SSE1"

section "410 syncToken recovery"
admin expire-sync-tokens '{"account":"a@example.com"}'
admin seed "{\"account\":\"a@example.com\",\"cal\":\"$A_GCAL\",\"id\":\"after410\",\"event\":{\"summary\":\"After 410\",\"start\":{\"dateTime\":\"$(local_dt 5 10:00:00)+01:00\",\"timeZone\":\"Europe/London\"},\"end\":{\"dateTime\":\"$(local_dt 5 11:00:00)+01:00\",\"timeZone\":\"Europe/London\"}}}"
sync_now
check "full re-list picks up the new event" wait_for 30 has_title "After 410"
eq "account still active" "$(acct_status a@example.com)" "active"
admin seed "{\"account\":\"a@example.com\",\"cal\":\"$A_GCAL\",\"id\":\"after410\",\"delete\":true}"
sync_now
check "outside delete propagates" wait_for 30 no_title "After 410"

section "SSE rev on change"
curl -sN -H "Authorization: Bearer $TOKEN" "$H/v1/stream" > "$work/sse2" 2>/dev/null &
SSE2=$!
sleep 1
REV0="$(agenda; jqb .rev)"
admin seed "{\"account\":\"b@example.com\",\"cal\":\"$B_GCAL\",\"id\":\"ssepoke\",\"event\":{\"summary\":\"SSE poke\",\"start\":{\"dateTime\":\"$(local_dt 6 10:00:00)+01:00\",\"timeZone\":\"Europe/London\"},\"end\":{\"dateTime\":\"$(local_dt 6 10:30:00)+01:00\",\"timeZone\":\"Europe/London\"}}}"
sync_now
check "SSE sent a rev after the change" wait_for 20 bash -c "grep -q '^event: rev' '$work/sse2' && grep '^data:' '$work/sse2' | jq -R 'sub(\"data: *\";\"\")|fromjson?|.rev//empty' | awk -v r=$REV0 '\$1>r{f=1} END{exit !f}'"
eq "agenda rev advanced" "$(agenda; jqb '.rev>'"$REV0")" "true"
stop_bg "$SSE2"

section "summary"
printf '\n%s passed, %s failed\n' "$passes" "$fails"
if [ "$fails" -gt 0 ]; then echo "E2E FAIL"; exit 1; fi
echo "E2E PASS"
