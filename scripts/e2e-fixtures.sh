#!/usr/bin/env bash
# Starts the fixture hub (`calendard serve --fixtures`) and runs the CalendarCore sync against it
# (real APIClient + SyncEngine + in-memory GRDB) via xcodebuild test. The fixtures hub has no /v1/stream.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
port="${E2E_PORT:-18787}"
dest="${IOS_DEST:-platform=iOS Simulator,name=iPhone 17 Pro}"
bin="$(mktemp -d)/calendard"

(cd "$root/server" && CGO_ENABLED=0 go build -o "$bin" ./cmd/calendard)
CALENDAR_ADDR="127.0.0.1:$port" "$bin" serve --fixtures >/dev/null 2>&1 &
hub=$!
trap 'kill $hub 2>/dev/null || true' EXIT
for _ in $(seq 1 50); do
  curl -fsS -H 'Authorization: Bearer cal_e2e' "http://127.0.0.1:$port/v1/hello" >/dev/null 2>&1 && break
  sleep 0.2
done

cd "$root/ios/Packages/CalendarCore"
# TEST_RUNNER_ prefixed variables are forwarded (prefix stripped) to the test process.
TEST_RUNNER_CALENDAR_E2E_URL="http://127.0.0.1:$port" TEST_RUNNER_CALENDAR_E2E_TOKEN="cal_e2e" \
  xcodebuild test -scheme CalendarCore -destination "$dest" -skipPackagePluginValidation -skipMacroValidation \
  -only-testing:CalendarCoreTests/FixtureHubE2ETests 2>&1 | grep -E "error:|✘|✔ Test|Test run|TEST (SUCC|FAIL)"
