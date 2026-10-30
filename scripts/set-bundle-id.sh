#!/usr/bin/env bash
# Point the iOS app at your own Apple identifiers: bundle ids, App Group, keychain group, background task id.
#
#   scripts/set-bundle-id.sh com.example.calendar
#
# Rewrites every `dev.casprine.calendar` under ios/ (and the `dev.casprine` prefix in project.yml), then tells you the
# hub setting to match. Run it once, before `make ios-gen`. Safe to repeat with the same id.
set -euo pipefail

NEW="${1:-}"
[[ "$NEW" =~ ^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$ ]] || { echo "usage: $0 <reverse.dns.id>   e.g. com.example.calendar" >&2; exit 2; }
OLD="dev.casprine.calendar"
PREFIX="${NEW%.*}"

cd "$(dirname "$0")/.."
files=$(grep -rlF --exclude-dir=Calendar.xcodeproj --exclude-dir=.build --exclude-dir=build "$OLD" ios || true)
[ -n "$files" ] || { echo "nothing to change: no file under ios/ mentions $OLD"; exit 0; }
for f in $files; do
  sed -i.bak "s/dev\.casprine\.calendar/${NEW//./\\.}/g" "$f" && rm -f "$f.bak"
done
sed -i.bak "s/^  bundleIdPrefix: dev\.casprine$/  bundleIdPrefix: ${PREFIX}/" ios/project.yml && rm -f ios/project.yml.bak

echo "Updated $(echo "$files" | wc -l | tr -d ' ') files. New bundle id: $NEW (App Group: group.$NEW)"
echo
echo "Next:"
echo "  1. In the Apple developer portal register the App IDs $NEW, $NEW.widgets, $NEW.notification-service,"
echo "     $NEW.notification-content and the App Group group.$NEW (Xcode does this for you when signing is automatic)."
echo "  2. On the hub set CALENDAR_APNS_TOPIC=$NEW"
echo "  3. CALENDAR_TEAM_ID=<your team id> make ios-gen"
