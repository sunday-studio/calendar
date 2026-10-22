#!/usr/bin/env bash
# Cut a release: bump VERSION, roll CHANGELOG, bump the iOS marketing version, commit and tag. Never pushes.
#
#   scripts/release.sh [--dry-run|--execute] <X.Y.Z[-pre.N] | patch | minor | major>
#
# Default is --dry-run: every check runs and the plan is printed, nothing is written. Use --execute
# (or `make release VERSION=...`) to apply it. See RELEASING.md.
#
# Special case (first release): if the target equals VERSION and CHANGELOG.md already has a dated
# "## [X.Y.Z] - date" section, no files are edited; the script only checks and tags the current HEAD.
set -euo pipefail

MODE=dry
ARG=""
for a in "$@"; do
  case "$a" in
    --dry-run) MODE=dry ;;
    --execute) MODE=run ;;
    -h|--help) sed -n '2,11p' "$0"; exit 0 ;;
    -*) echo "unknown option: $a" >&2; exit 2 ;;
    *) [ -z "$ARG" ] || { echo "only one version argument allowed" >&2; exit 2; }; ARG="$a" ;;
  esac
done
[ -n "$ARG" ] || { echo "usage: $0 [--dry-run|--execute] <X.Y.Z | patch | minor | major>" >&2; exit 2; }

fail() { echo "release: $*" >&2; exit 1; }

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# --- git checks ---------------------------------------------------------------------------------------
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1 || [ "$(git rev-parse --show-toplevel 2>/dev/null)" != "$ROOT" ]; then
  fail "$ROOT is not a git repository. Run 'git init -b main', commit, and push first (RELEASING.md, 'Cutting v0.1.0')."
fi
git rev-parse --verify HEAD >/dev/null 2>&1 || fail "no commits yet. Make the initial commit first."
branch="$(git symbolic-ref --short HEAD 2>/dev/null || echo detached)"
[ "$branch" = main ] || fail "on branch '$branch'; releases are cut from main."
[ -z "$(git status --porcelain)" ] || fail "working tree is not clean. Commit or stash first:
$(git status --short)"

# --- current and target version -----------------------------------------------------------------------
[ -f VERSION ] || fail "VERSION file missing."
CUR="$(tr -d ' \n\r' < VERSION)"
SEMVER='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$'
[[ "$CUR" =~ $SEMVER ]] || fail "VERSION ('$CUR') is not SemVer."
core="${CUR%%-*}"
IFS=. read -r MA MI PA <<<"$core"
case "$ARG" in
  patch) NEW="$MA.$MI.$((PA+1))" ;;
  minor) NEW="$MA.$((MI+1)).0" ;;
  major) NEW="$((MA+1)).0.0" ;;
  *) NEW="${ARG#v}" ;;
esac
[[ "$NEW" =~ $SEMVER ]] || fail "'$ARG' is not X.Y.Z (optionally -rc.1) or patch|minor|major."
TAG="v$NEW"
IOSV="${NEW%%-*}"   # MARKETING_VERSION must be numeric, so a pre-release suffix is dropped for iOS
TODAY="$(date +%Y-%m-%d)"

git rev-parse -q --verify "refs/tags/$TAG" >/dev/null && fail "tag $TAG already exists. Nothing to do (or pick another version)."
if git remote get-url origin >/dev/null 2>&1 && git ls-remote --exit-code --tags origin "refs/tags/$TAG" >/dev/null 2>&1; then
  fail "tag $TAG already exists on origin."
fi

# --- CHANGELOG ----------------------------------------------------------------------------------------
[ -f CHANGELOG.md ] || fail "CHANGELOG.md missing."
grep -q '^## \[Unreleased\]' CHANGELOG.md || fail "CHANGELOG.md has no '## [Unreleased]' section."
unreleased_body="$(awk '/^## \[Unreleased\]/{f=1;next} /^## \[/{f=0} /^\[[^]]+\]: /{f=0} f' CHANGELOG.md | grep -v '^[[:space:]]*$' || true)"
has_section=0
grep -q "^## \[$NEW\] - " CHANGELOG.md && has_section=1

# --- iOS marketing version ----------------------------------------------------------------------------
PROJ=ios/project.yml
pat='^    MARKETING_VERSION: "[0-9][0-9.]*"'
[ "$(grep -c "$pat" "$PROJ")" = 1 ] || fail "expected exactly one MARKETING_VERSION line in $PROJ, found $(grep -c "$pat" "$PROJ")."

TAG_ONLY=0
if [ "$NEW" = "$CUR" ]; then
  [ "$has_section" = 1 ] || fail "$NEW is already the current VERSION and CHANGELOG.md has no '## [$NEW] - date' section."
  [ -z "$unreleased_body" ] || fail "VERSION is already $NEW but [Unreleased] still has entries. Pick a new version."
  grep -q "^    MARKETING_VERSION: \"$IOSV\"" "$PROJ" || fail "VERSION is $NEW but $PROJ has a different MARKETING_VERSION."
  TAG_ONLY=1
else
  # new must be greater than current (compare core numerically; a release beats its own pre-release)
  IFS=. read -r nA nB nC <<<"${NEW%%-*}"
  newkey=$(printf '%09d%09d%09d' "$nA" "$nB" "$nC"); curkey=$(printf '%09d%09d%09d' "$MA" "$MI" "$PA")
  if [ "$newkey" \< "$curkey" ] || { [ "$newkey" = "$curkey" ] && [ "$CUR" = "$core" ]; }; then
    fail "$NEW is not greater than the current version $CUR."
  fi
  [ "$has_section" = 0 ] || fail "CHANGELOG.md already has a section for $NEW."
  [ -n "$unreleased_body" ] || fail "[Unreleased] in CHANGELOG.md is empty. Describe the changes first."
fi

# --- plan ---------------------------------------------------------------------------------------------
echo "Release plan ($([ "$MODE" = dry ] && echo 'DRY RUN, nothing will be written' || echo EXECUTE))"
echo "  version      $CUR -> $NEW   (tag $TAG, iOS MARKETING_VERSION $IOSV)"
if [ "$TAG_ONLY" = 1 ]; then
  echo "  files        unchanged (VERSION, CHANGELOG and $PROJ already say $NEW)"
  echo "  commit       none"
else
  echo "  VERSION      $NEW"
  echo "  CHANGELOG    [Unreleased] -> [$NEW] - $TODAY, fresh empty [Unreleased], compare links updated"
  echo "  $PROJ  MARKETING_VERSION \"$IOSV\" (that line only)"
  echo "  commit       Release $TAG"
fi
echo "  tag          $TAG (annotated, on HEAD$([ "$TAG_ONLY" = 1 ] || echo ' after the commit'))"

if [ "$MODE" = dry ]; then
  echo
  echo "Dry run only. Re-run with --execute (or: make release VERSION=$ARG) to apply."
  exit 0
fi

# --- apply --------------------------------------------------------------------------------------------
if [ "$TAG_ONLY" = 0 ]; then
  printf '%s\n' "$NEW" > VERSION

  tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
  awk -v new="$NEW" -v cur="$CUR" -v today="$TODAY" '
    /^## \[Unreleased\]/ { print; print ""; print "## [" new "] - " today; next }
    /^\[Unreleased\]: / {
      url = $0; sub(/^\[Unreleased\]: /, "", url)
      base = url; sub(/\/compare\/.*$/, "", base)
      print "[Unreleased]: " base "/compare/v" new "...HEAD"
      print "[" new "]: " base "/compare/v" cur "...v" new
      next }
    { print }' CHANGELOG.md > "$tmp"
  cat "$tmp" > CHANGELOG.md

  sed "s/^    MARKETING_VERSION: \"[0-9][0-9.]*\"/    MARKETING_VERSION: \"$IOSV\"/" "$PROJ" > "$tmp"
  cat "$tmp" > "$PROJ"
  grep -q "^    MARKETING_VERSION: \"$IOSV\"" "$PROJ" || fail "failed to update MARKETING_VERSION"

  git add VERSION CHANGELOG.md "$PROJ"
  git commit -q -m "Release $TAG"
fi
git tag -a "$TAG" -m "Calendar $NEW"

echo
echo "Created tag $TAG$([ "$TAG_ONLY" = 1 ] || echo ' and commit "Release '"$TAG"'"'). Nothing has been pushed."
echo "To publish (this starts the release workflow):"
echo "  git push origin main"
echo "  git push origin $TAG"
echo "To undo before pushing:  git tag -d $TAG$([ "$TAG_ONLY" = 1 ] || echo '; git reset --hard HEAD~1')"
