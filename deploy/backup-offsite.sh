#!/bin/sh
# Copy the hub's encrypted nightly backups to another disk or machine. Run from cron on the Docker host:
#   30 3 * * *  /path/to/deploy/backup-offsite.sh /mnt/nas/calendar-backups
# The files are encrypted with a key derived from the master key, so the destination needs no extra protection;
# keep the master key itself somewhere else (password manager). Without it a backup cannot be read.
set -eu
DEST="${1:?usage: backup-offsite.sh DESTINATION_DIR}"
PROJECT="${COMPOSE_PROJECT:-calendar}"
VOLUME="${PROJECT}_data"
mkdir -p "$DEST"
# copy straight out of the data volume; no shell is needed inside the (distroless) hub image
docker run --rm -v "${VOLUME}:/data:ro" -v "$DEST:/out" alpine:3 sh -c 'cp -n /data/backups/calendar-*.db.enc /out/ 2>/dev/null; ls -1 /out | tail -n 3'
# alert if the newest backup is older than 36 hours (cron mails the output)
newest=$(ls -1t "$DEST"/calendar-*.db.enc 2>/dev/null | head -n 1 || true)
if [ -z "$newest" ] || [ -n "$(find "$newest" -mmin +2160)" ]; then
	echo "WARNING: newest calendar backup in $DEST is missing or older than 36 hours" >&2
	exit 1
fi
