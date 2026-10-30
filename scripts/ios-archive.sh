#!/usr/bin/env bash
# Archive + export a Release build with the PAID team (TestFlight, or ad hoc with --adhoc).
#
#   TEAM_ID=ABCDE12345 [BUILD_NUMBER=7] scripts/ios-archive.sh [--adhoc]
#
# Env: TEAM_ID (required)  BUILD_NUMBER (optional, overrides CURRENT_PROJECT_VERSION)
#      NO_UPLOAD=1  (TestFlight mode: export the .ipa but do not upload it)
# Needs your paid team signed into Xcode (Settings > Accounts). Credentials are never read or printed here.
set -euo pipefail

MODE=appstore
for a in "$@"; do
  case "$a" in
    --adhoc) MODE=adhoc ;;
    -h|--help) sed -n '2,9p' "$0"; exit 0 ;;
    *) echo "unknown argument: $a" >&2; exit 2 ;;
  esac
done

if [ -z "${TEAM_ID:-}" ]; then
  echo "TEAM_ID is required (your paid Apple Developer team id, 10 characters)." >&2
  echo "usage: TEAM_ID=ABCDE12345 [BUILD_NUMBER=7] $0 [--adhoc]" >&2
  exit 2
fi
if ! [[ "$TEAM_ID" =~ ^[A-Z0-9]{10}$ ]]; then
  echo "TEAM_ID does not look like a team id (10 uppercase letters/digits)." >&2
  exit 2
fi
if [ -n "${BUILD_NUMBER:-}" ] && ! [[ "$BUILD_NUMBER" =~ ^[0-9]+$ ]]; then
  echo "BUILD_NUMBER must be a positive integer." >&2
  exit 2
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/ios"
ARCHIVE=build/Calendar.xcarchive
if [ "$MODE" = adhoc ]; then
  EXPORT_DIR=build/export-adhoc; BASE=ExportOptions.plist
else
  EXPORT_DIR=build/export; BASE=ExportOptions.appstore.plist
fi

echo "==> Generating project for team $TEAM_ID (full entitlements)"
CALENDAR_TEAM_ID="$TEAM_ID" xcodegen generate

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
OPTS="$TMP/ExportOptions.plist"
cp "$BASE" "$OPTS"
/usr/libexec/PlistBuddy -c "Set :teamID $TEAM_ID" "$OPTS"
if [ "$MODE" = appstore ] && [ -n "${NO_UPLOAD:-}" ]; then
  /usr/libexec/PlistBuddy -c "Set :destination export" "$OPTS"
fi

EXTRA=()
[ -n "${BUILD_NUMBER:-}" ] && EXTRA+=("CURRENT_PROJECT_VERSION=$BUILD_NUMBER")

rm -rf "$ARCHIVE" "$EXPORT_DIR"
echo "==> Archiving (Release, generic iOS device)"
xcodebuild archive -project Calendar.xcodeproj -scheme Calendar -configuration Release \
  -destination 'generic/platform=iOS' -archivePath "$ARCHIVE" -allowProvisioningUpdates \
  DEVELOPMENT_TEAM="$TEAM_ID" CODE_SIGNING_ALLOWED=YES ${EXTRA[@]+"${EXTRA[@]}"}

echo "==> Exporting ($MODE)"
xcodebuild -exportArchive -archivePath "$ARCHIVE" -exportPath "$EXPORT_DIR" \
  -exportOptionsPlist "$OPTS" -allowProvisioningUpdates

echo
echo "Archive: $ROOT/ios/$ARCHIVE"
if [ "$MODE" = adhoc ]; then
  echo "IPA:     $ROOT/ios/$EXPORT_DIR/Calendar.ipa  (install via Xcode > Devices, Apple Configurator or a manifest)"
elif [ -n "${NO_UPLOAD:-}" ]; then
  echo "IPA:     $ROOT/ios/$EXPORT_DIR/Calendar.ipa  (not uploaded; drag it into Transporter)"
else
  echo "Uploaded to App Store Connect. Wait for processing, then enable it in TestFlight."
  echo "(Local export folder: $ROOT/ios/$EXPORT_DIR)"
fi
echo
echo "The Xcode project is now generated for team $TEAM_ID."
echo "To go back to the free Personal Team project, run: make ios-gen-personal"
