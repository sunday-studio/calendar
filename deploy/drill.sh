#!/bin/sh
# Backup/restore drill on a throwaway database (no Docker, no network): proves the binary can back up,
# lose the database, restore it and still find its data. `make backup-drill` runs this.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
(cd "$here/../server" && CGO_ENABLED=0 go build -o "$tmp/calendard" ./cmd/calendard)
export CALENDAR_DB="$tmp/data/calendar.db" CALENDAR_BACKUP_DIR="$tmp/backups"
export CALENDAR_MASTER_KEY=$(head -c32 /dev/urandom | base64)
bin="$tmp/calendard"
"$bin" token create --name drill --kind device --scope admin >/dev/null
"$bin" doctor || true
"$bin" backup
file=$(ls "$tmp"/backups/calendar-*.db.enc | head -n 1)
echo "--- losing the database"
rm -rf "$tmp/data"
"$bin" restore --from "$file"
"$bin" token list | grep -q drill && echo "drill ok: token survived backup and restore"
echo "--- wrong key must fail"
if CALENDAR_MASTER_KEY=$(head -c32 /dev/urandom | base64) "$bin" restore --from "$file" --force 2>/dev/null; then
	echo "drill FAILED: restored with the wrong key" >&2
	exit 1
fi
echo "drill ok: wrong key refused"
