#!/usr/bin/env bash
# iOS end-to-end UI test against the REAL hub + fake Google (docs/engineering/e2e.md, "iOS end-to-end").
#   scripts/ios-e2e.sh        stack up (free ports), build, boot "Calendar E2E", run CalendarUITests/E2ETests, tear down
# Env: IOS_E2E_SIM (simulator name, default "Calendar E2E"), IOS_E2E_ONLY (xctest filter, default E2ETests),
#      IOS_E2E_STEPS="offline,search" (run only steps whose name contains a word; pairing always runs), KEEP=1 keeps logs and the state dir. Only processes/simulators this script started are stopped.
set -uo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
sim_name="${IOS_E2E_SIM:-Calendar E2E}"
only="${IOS_E2E_ONLY:-E2ETests}"
bundle=studio.thesunday.calendar
work="$(mktemp -d "${TMPDIR:-/tmp}/calendar-ios-e2e.XXXXXX")"
shots="${E2E_SHOTS:-$root/docs/screens/e2e}"
dd="${IOS_E2E_DERIVED:-$root/.e2e-derived}"
started_sim=0; udid=""; rc=1

free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'; }

cleanup() {
  E2E_DIR="$work/stack" "$root/scripts/e2e.sh" down >/dev/null 2>&1
  if [ -n "$udid" ]; then
    xcrun simctl terminate "$udid" "$bundle" >/dev/null 2>&1
    [ "$started_sim" = 1 ] && xcrun simctl shutdown "$udid" >/dev/null 2>&1
  fi
  if [ "${KEEP:-0}" = 1 ] || [ "$rc" != 0 ]; then echo "logs kept in $work"; else rm -rf "$work"; fi
}
trap cleanup EXIT

for t in go curl jq python3 xcodegen xcodebuild xcrun; do command -v "$t" >/dev/null || { echo "ios-e2e: $t is required" >&2; exit 2; }; done

# ---- simulator: a dedicated one, created on first use
udid="$(xcrun simctl list devices available -j | jq -r --arg n "$sim_name" '[.devices[][]|select(.name==$n)][0].udid // empty')"
if [ -z "$udid" ]; then
  rt="$(xcrun simctl list runtimes -j | jq -r '[.runtimes[]|select(.isAvailable and (.identifier|contains("iOS")))]|last|.identifier')"
  echo "ios-e2e: creating simulator '$sim_name' ($rt)"
  udid="$(xcrun simctl create "$sim_name" com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro "$rt")" || { echo "ios-e2e: cannot create simulator"; exit 2; }
fi
state="$(xcrun simctl list devices -j | jq -r --arg u "$udid" '[.devices[][]|select(.udid==$u)][0].state')"
if [ "$state" != Booted ]; then started_sim=1; xcrun simctl boot "$udid" || exit 2; fi
xcrun simctl bootstatus "$udid" -b >/dev/null 2>&1
xcrun simctl status_bar "$udid" override --time 9:41 >/dev/null 2>&1 || true

# ---- stack: real hub + fake Google on free ports
hp="$(free_port)"; fp="$(free_port)"
echo "ios-e2e: starting hub :$hp and fake Google :$fp"
E2E_DIR="$work/stack" E2E_HUB_PORT="$hp" E2E_FAKE_PORT="$fp" "$root/scripts/e2e.sh" up >"$work/stack.log" 2>&1 || { cat "$work/stack.log"; echo "FAIL: stack"; exit 1; }
# shellcheck disable=SC1091
. "$work/stack/env"
for _ in $(seq 1 100); do
  n="$(curl -fsS -H "Authorization: Bearer $CALENDAR_E2E_TOKEN" "$CALENDAR_E2E_URL/v1/accounts" 2>/dev/null | jq '[.items[]|select(.status=="active")]|length' 2>/dev/null || echo 0)"
  [ "$n" = 2 ] && break; sleep 0.3
done
[ "${n:-0}" = 2 ] || { echo "FAIL: accounts did not sync"; exit 1; }

# ---- hub control for the offline step: the test touches $ctl/hub-down or $ctl/hub-up, we act and write <name>.done
ctl="$work/ctl"; mkdir -p "$ctl"
hub_pid() { cat "$work/stack/calendard.pid" 2>/dev/null; }
hub_start() {
  ( export CALENDAR_ADDR="127.0.0.1:$hp" CALENDAR_PUBLIC_URL="http://127.0.0.1:$hp" CALENDAR_DB="$work/stack/calendar.db" CALENDAR_MASTER_KEY_FILE="$work/stack/master.key"
    export CALENDAR_DEV=1 CALENDAR_SYNC_FAST=2s CALENDAR_SYNC_SLOW=2s CALENDAR_ICS_ALLOW_PRIVATE=1
    export CALENDAR_GOOGLE_AUTH_URL="http://127.0.0.1:$fp/auth" CALENDAR_GOOGLE_TOKEN_URL="http://127.0.0.1:$fp/token"
    export CALENDAR_GOOGLE_API_BASE="http://127.0.0.1:$fp" CALENDAR_GOOGLE_USERINFO_URL="http://127.0.0.1:$fp/userinfo"
    exec "$work/stack/bin/calendard" serve >>"$work/stack/calendard.log" 2>&1 ) &
  echo $! > "$work/stack/calendard.pid"
  for _ in $(seq 1 100); do curl -fsS "http://127.0.0.1:$hp/health" >/dev/null 2>&1 && return 0; sleep 0.2; done
  return 1
}
(
  while [ ! -f "$ctl/stop" ]; do
    if [ -f "$ctl/hub-down" ]; then
      rm -f "$ctl/hub-down"; p="$(hub_pid)"; [ -n "$p" ] && kill "$p" 2>/dev/null; sleep 0.5; : > "$ctl/hub-down.done"
    elif [ -f "$ctl/hub-up" ]; then
      rm -f "$ctl/hub-up"; hub_start; : > "$ctl/hub-up.done"
    fi
    sleep 0.3
  done
) &
watcher=$!; disown "$watcher" 2>/dev/null || true
trap 'touch "$ctl/stop"; kill $watcher 2>/dev/null; cleanup' EXIT

# ---- app + tests
(cd "$root/ios" && CALENDAR_TEAM_ID="${CALENDAR_TEAM_ID:-}" xcodegen generate >/dev/null) || { echo "FAIL: xcodegen"; exit 1; }
echo "ios-e2e: building"
(cd "$root/ios" && xcodebuild build-for-testing -project Calendar.xcodeproj -scheme Calendar -destination "id=$udid" \
  -derivedDataPath "$dd" -skipPackagePluginValidation -skipMacroValidation CODE_SIGNING_ALLOWED=YES CODE_SIGN_IDENTITY=- \
  >"$work/build.log" 2>&1) || { grep -E "error:" "$work/build.log" | head -20; echo "FAIL: build (log $work/build.log)"; exit 1; }
xcrun simctl terminate "$udid" "$bundle" >/dev/null 2>&1
xcrun simctl uninstall "$udid" "$bundle" >/dev/null 2>&1   # a stale install is what makes the runner hang
mkdir -p "$shots"; [ -z "${IOS_E2E_STEPS:-}" ] && find "$shots" -maxdepth 1 -name '*.png' -delete
echo "ios-e2e: running $only"
export TEST_RUNNER_E2E_HUB="$CALENDAR_E2E_URL" TEST_RUNNER_E2E_TOKEN="$CALENDAR_E2E_TOKEN" TEST_RUNNER_E2E_FAKE="$FAKE_GOOGLE" TEST_RUNNER_E2E_SHOTS="$shots" TEST_RUNNER_E2E_CTL="$ctl" TEST_RUNNER_E2E_STEPS="${IOS_E2E_STEPS:-}"
(cd "$root/ios" && xcodebuild test-without-building -project Calendar.xcodeproj -scheme Calendar -destination "id=$udid" \
  -derivedDataPath "$dd" -only-testing:"CalendarUITests/$only" -resultBundlePath "$work/result.xcresult" \
  >"$work/test.log" 2>&1)
code=$?
grep -E "error:|Test Case .*(passed|failed)|XCTAssert|TEST (SUCCEEDED|FAILED)|E2E step" "$work/test.log" | head -60
if [ "$code" = 0 ]; then rc=0; echo "IOS E2E PASS"; else echo "IOS E2E FAIL (log $work/test.log)"; fi
exit "$rc"
