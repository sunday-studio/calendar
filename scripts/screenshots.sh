#!/usr/bin/env bash
# Fresh README screenshots of the real app against a throwaway hub with the rich demo data.
#   scripts/screenshots.sh            stack up (free ports, temp dir), build, tour in "Calendar Shots", process, tear down
# What it does, in order:
#   1. builds calendard + fakegoogle and starts them on FREE ports with a temp data dir (never .local/, never port 8787/8788);
#      fake Google runs --profile rich, accounts work/personal/side/stale are seeded, a device token is created
#   2. uses its own simulator "Calendar Shots" (iPhone 17 Pro class, newest iOS runtime), created on first use,
#      with a clean status bar (9:41, full battery, full signal)
#   3. copies ios/ to a temp dir (no .build) and generates + builds the project THERE, so the committed
#      ios/Calendar.xcodeproj is never touched; runs CalendarUITests/ScreenshotTourUITests (ios/CalendarUITests)
#   4. downsizes the PNGs to a README-friendly width, writes docs/assets/screenshots/*.png, the app icon
#      (docs/assets/icon.png) and the hero composites (docs/assets/hero*.png)
#   5. stops only what it started and deletes the temp dirs
# Light and dark are two runs (simulator appearance switched with simctl), a few minutes apart.
# Time-dependent states ("Up next in N min", "in progress", "Join") are relative to the moment the fake Google starts:
# the app is built first and the tour runs right after the stack's first sync, so the next-event block starts as the Acme renewal call about 8 minutes away (it counts down between the light and dark run).
# Env: SHOTS_WIDTH (pixel width of the output, default 540), SHOTS_DERIVED (reuse a DerivedData dir between runs),
#      SHOTS_RAW (keep full-size PNGs in this directory), SHOTS_SIM (simulator name, default "Calendar Shots"),
#      SHOTS_SKIP_BUILD=1 (reuse SHOTS_DERIVED as is), KEEP=1 (keep logs and temp dir).
# Only processes and simulators this script started are stopped. Needs go, curl, jq, python3, xcodegen, xcodebuild, sips, swift.
set -uo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
sim_name="${SHOTS_SIM:-Calendar Shots}"
width="${SHOTS_WIDTH:-540}"
bundle=dev.casprine.calendar
work="$(mktemp -d "${TMPDIR:-/tmp}/calendar-shots.XXXXXX")"
out="$root/docs/assets"
started_sim=0; udid=""; rc=1; hub_pid=""; fake_pid=""

# never use the simulators other lanes own
case "$sim_name" in "iPhone 17"|"iPhone 17 Pro"|"Calendar E2E"|"Calendar Cleanup"|"Calendar Motion"|"Swift Lane")
  echo "screenshots: '$sim_name' belongs to someone else; use the default 'Calendar Shots'" >&2; exit 2 ;; esac

free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'; }

cleanup() {
  [ -n "$hub_pid" ] && kill "$hub_pid" 2>/dev/null
  [ -n "$fake_pid" ] && kill "$fake_pid" 2>/dev/null
  if [ -n "$udid" ]; then
    xcrun simctl terminate "$udid" "$bundle" >/dev/null 2>&1
    xcrun simctl status_bar "$udid" clear >/dev/null 2>&1
    xcrun simctl ui "$udid" appearance light >/dev/null 2>&1
    [ "$started_sim" = 1 ] && xcrun simctl shutdown "$udid" >/dev/null 2>&1
  fi
  if [ "${KEEP:-0}" = 1 ] || [ "$rc" != 0 ]; then echo "screenshots: temp dir kept: $work"; else rm -rf "$work"; fi
}
trap cleanup EXIT

for t in go curl jq python3 xcodegen xcodebuild xcrun sips rsync; do command -v "$t" >/dev/null || { echo "screenshots: $t is required" >&2; exit 2; }; done

# ---- simulator: a dedicated one, created on first use
udid="$(xcrun simctl list devices available -j | jq -r --arg n "$sim_name" '[.devices[][]|select(.name==$n)][0].udid // empty')"
if [ -z "$udid" ]; then
  rt="$(xcrun simctl list runtimes -j | jq -r '[.runtimes[]|select(.isAvailable and (.identifier|contains("iOS")))]|last|.identifier')"
  echo "screenshots: creating simulator '$sim_name' ($rt)"
  udid="$(xcrun simctl create "$sim_name" com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro "$rt")" || { echo "FAIL: cannot create simulator"; exit 2; }
fi
state="$(xcrun simctl list devices -j | jq -r --arg u "$udid" '[.devices[][]|select(.udid==$u)][0].state')"
if [ "$state" != Booted ]; then started_sim=1; xcrun simctl boot "$udid" || exit 2; fi
xcrun simctl bootstatus "$udid" -b >/dev/null 2>&1
xcrun simctl status_bar "$udid" override --time 9:41 --batteryState charged --batteryLevel 100 --cellularMode active --cellularBars 4 --wifiBars 3 --operatorName '' >/dev/null 2>&1 || true

# ---- app: build from a copy of ios/ so the committed Xcode project stays as it is
dd="${SHOTS_DERIVED:-$work/derived}"
rsync -a --exclude .build --exclude build --exclude DerivedData --exclude '*.xcuserstate' --exclude xcuserdata --exclude '*.xcodeproj' "$root/ios/" "$work/ios/"
(cd "$work/ios" && CALENDAR_TEAM_ID="${CALENDAR_TEAM_ID:-XXXXXXXXXX}" xcodegen generate >/dev/null) || { echo "FAIL: xcodegen"; exit 1; }
if [ "${SHOTS_SKIP_BUILD:-0}" != 1 ]; then
  echo "screenshots: building the app (first build takes a few minutes)"
  (cd "$work/ios" && xcodebuild build-for-testing -project Calendar.xcodeproj -scheme Calendar -destination "id=$udid" \
    -derivedDataPath "$dd" -skipPackagePluginValidation -skipMacroValidation CODE_SIGNING_ALLOWED=YES CODE_SIGN_IDENTITY=- \
    >"$work/build.log" 2>&1) || { grep -E "error:" "$work/build.log" | head -20; echo "FAIL: build (log $work/build.log)"; exit 1; }
fi
xcrun simctl terminate "$udid" "$bundle" >/dev/null 2>&1
xcrun simctl uninstall "$udid" "$bundle" >/dev/null 2>&1   # a stale install is what makes the runner hang

# ---- throwaway stack: fake Google (rich) + the real hub, own temp data dir, free ports
hp="$(free_port)"; fp="$(free_port)"
st="$work/stack"; mkdir -p "$st/bin"
echo "screenshots: building the hub and the fake Google"
(cd "$root/server" && CGO_ENABLED=0 go build -o "$st/bin/calendard" ./cmd/calendard && go build -o "$st/bin/fakegoogle" ./cmd/fakegoogle) || { echo "FAIL: go build"; exit 1; }
( umask 077; head -c32 /dev/urandom | base64 > "$st/master.key" )
export CALENDAR_ADDR="127.0.0.1:$hp" CALENDAR_PUBLIC_URL="http://127.0.0.1:$hp"
export CALENDAR_DB="$st/calendar.db" CALENDAR_MASTER_KEY_FILE="$st/master.key" CALENDAR_BACKUP_DIR="$st/backups"
export CALENDAR_DEV=1 CALENDAR_SYNC_FAST=2s CALENDAR_SYNC_SLOW=5s CALENDAR_ICS_ALLOW_PRIVATE=1
export CALENDAR_GOOGLE_AUTH_URL="http://127.0.0.1:$fp/auth" CALENDAR_GOOGLE_TOKEN_URL="http://127.0.0.1:$fp/token"
export CALENDAR_GOOGLE_API_BASE="http://127.0.0.1:$fp" CALENDAR_GOOGLE_USERINFO_URL="http://127.0.0.1:$fp/userinfo"
unset CALENDAR_GOOGLE_CLIENT_FILE OTEL_EXPORTER_OTLP_ENDPOINT OTEL_EXPORTER_OTLP_TRACES_ENDPOINT OTEL_EXPORTER_OTLP_LOGS_ENDPOINT OTEL_EXPORTER_OTLP_METRICS_ENDPOINT
"$st/bin/fakegoogle" --addr "127.0.0.1:$fp" --profile rich >"$st/fakegoogle.log" 2>&1 & fake_pid=$!
for _ in $(seq 1 100); do curl -fsS "http://127.0.0.1:$fp/health" >/dev/null 2>&1 && break; sleep 0.2; done
curl -fsS "http://127.0.0.1:$fp/health" >/dev/null 2>&1 || { echo "FAIL: fake Google did not start"; exit 1; }
token="$("$st/bin/calendard" token create --name screenshots --kind device --scope admin | sed -n 's/^secret: *//p')"
[ -n "$token" ] || { echo "FAIL: no token"; exit 1; }
for a in work personal side stale; do
  "$st/bin/calendard" dev seed-account --provider google --email "$a@example.com" --name "$(echo "$a" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')" >/dev/null || { echo "FAIL: seed $a"; exit 1; }
done
"$st/bin/calendard" serve >"$st/calendard.log" 2>&1 & hub_pid=$!
for _ in $(seq 1 100); do curl -fsS "http://127.0.0.1:$hp/health" >/dev/null 2>&1 && break; sleep 0.2; done
n=0
for _ in $(seq 1 150); do
  n="$(curl -fsS -H "Authorization: Bearer $token" "http://127.0.0.1:$hp/v1/accounts" 2>/dev/null | jq '[.items[]|select(.status=="active")]|length' 2>/dev/null || echo 0)"
  [ "$n" = 4 ] && break; sleep 0.3
done
[ "${n:-0}" = 4 ] || { echo "FAIL: accounts did not sync (see $st/calendard.log)"; exit 1; }
sleep 4   # let the first full sync of every calendar land
echo "screenshots: hub on :$hp, fake Google on :$fp (throwaway, rich profile)"

# ---- the tour: one run per appearance, right after the stack is up
raw="${SHOTS_RAW:-$work/raw}"; rm -rf "$raw"; mkdir -p "$raw"
export TEST_RUNNER_SHOTS_HUB="http://127.0.0.1:$hp" TEST_RUNNER_SHOTS_TOKEN="$token" TEST_RUNNER_SHOTS_DIR="$raw"
for mode in ${SHOTS_MODES:-light dark}; do
  xcrun simctl ui "$udid" appearance "$mode" >/dev/null 2>&1
  echo "screenshots: running the tour ($mode)"
  TEST_RUNNER_SHOTS_MODE="$mode" bash -c 'cd "$1" && xcodebuild test-without-building -project Calendar.xcodeproj -scheme Calendar -destination "id=$2" -derivedDataPath "$3" -only-testing:CalendarUITests/ScreenshotTourUITests -resultBundlePath "$4" >"$5" 2>&1' _ "$work/ios" "$udid" "$dd" "$work/result-$mode.xcresult" "$work/test-$mode.log"
  code=$?
  grep -E "SHOTS skip|error:|TEST (SUCCEEDED|FAILED)" "$work/test-$mode.log" | head -40
  [ "$code" = 0 ] || { echo "FAIL: tour $mode (log $work/test-$mode.log)"; exit 1; }
done
nraw="$(ls "$raw"/*.png 2>/dev/null | wc -l | tr -d ' ')"
[ "$nraw" -gt 0 ] || { echo "FAIL: no screenshots"; exit 1; }

# ---- post-process
mkdir -p "$out/screenshots"; rm -f "$out/screenshots/"*.png
for f in "$raw"/*.png; do
  sips --resampleWidth "$width" "$f" --out "$out/screenshots/$(basename "$f")" >/dev/null 2>&1
done
if command -v pngquant >/dev/null; then pngquant --force --skip-if-larger --quality 70-95 --ext .png "$out/screenshots/"*.png >/dev/null 2>&1 || true; fi
sips -z 512 512 "$root/ios/Calendar/Resources/Assets.xcassets/AppIcon.appiconset/AppIcon-light.png" --out "$out/icon.png" >/dev/null 2>&1

# hero images: three phones side by side (rounded corners, transparent background), drawn with AppKit
S="$out/screenshots"
cat >"$work/hero.swift" <<'SWIFT'
import AppKit
let a = CommandLine.arguments
// usage: hero out.png img1 img2 img3
let imgs = a.dropFirst(2).compactMap { NSImage(contentsOfFile: $0) }
guard imgs.count == 3 else { exit(1) }
let h: CGFloat = 1000, gap: CGFloat = 36, pad: CGFloat = 36
let ws = imgs.map { h * $0.size.width / $0.size.height }
let W = ws.reduce(0, +) + gap * 2 + pad * 2, H = h + pad * 2
let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(W), pixelsHigh: Int(H), bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
var x = pad
for (i, img) in imgs.enumerated() {
    let r = NSRect(x: x, y: pad, width: ws[i], height: h)
    let path = NSBezierPath(roundedRect: r, xRadius: 70, yRadius: 70)
    NSGraphicsContext.saveGraphicsState()
    let sh = NSShadow(); sh.shadowBlurRadius = 22; sh.shadowOffset = NSSize(width: 0, height: -6); sh.shadowColor = NSColor(white: 0, alpha: 0.28); sh.set()
    NSColor.white.setFill(); path.fill()
    NSGraphicsContext.restoreGraphicsState()
    NSGraphicsContext.saveGraphicsState()
    path.addClip()
    img.draw(in: r, from: .zero, operation: .sourceOver, fraction: 1)
    NSGraphicsContext.restoreGraphicsState()
    NSColor(white: 0.5, alpha: 0.35).setStroke(); path.lineWidth = 2; path.stroke()
    x += ws[i] + gap
}
NSGraphicsContext.restoreGraphicsState()
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: a[1]))
SWIFT
if command -v swift >/dev/null; then
  mk() { swift "$work/hero.swift" "$out/$1" "$S/$2.png" "$S/$3.png" "$S/$4.png" >/dev/null 2>&1 || echo "screenshots: note: could not compose $1"; }
  mk hero.png home-dark day-light event-detail-dark
  mk hero-light.png home-light day-light event-detail-light
  mk hero-dark.png home-dark day-dark event-detail-dark
  for h in hero hero-light hero-dark; do [ -f "$out/$h.png" ] && sips --resampleWidth 1500 "$out/$h.png" --out "$out/$h.png" >/dev/null 2>&1; done
fi

rc=0
echo "screenshots: $(ls "$S"/*.png | wc -l | tr -d ' ') images in docs/assets/screenshots ($(du -sh "$S" | cut -f1)), icon.png, hero*.png"
echo "SCREENSHOTS OK (look at every image before committing: clipped, keyboard up, skeletons, empty states)"
exit 0
