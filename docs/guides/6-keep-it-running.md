# 6. Keep it running

## Backups

The hub writes an encrypted, verified snapshot every night at 03:00 into the container's data volume and keeps the last 14.
That volume lives on the same machine as the hub, so copy the snapshots elsewhere too.

1. Check that a backup exists and is fresh:

   ```bash
   docker compose -f compose.image.yaml exec calendard calendard doctor      # "last backup" line
   ```

2. Copy them off the server regularly, for example with the script in the bundle:

   ```bash
   ./backup-offsite.sh /path/to/a/folder/that/is/backed-up
   ```

3. **Keep the master key somewhere else** (your password manager). A backup is encrypted with a key derived from it, and
   is useless without it.

## Try a restore once

A backup you have not restored is a hope. On any machine with Docker, restore the newest file into a scratch container and
check that it opens: the exact commands are in [RUNBOOK.md](../ops/RUNBOOK.md), and `deploy/drill.sh` does it end to end
with throwaway data.

## Updating

```bash
cd calendar-deploy-<version>/deploy
docker compose -f compose.image.yaml exec calendard calendard backup      # safety copy first
CALENDAR_VERSION=0.2.0 docker compose -f compose.image.yaml pull
CALENDAR_VERSION=0.2.0 docker compose -f compose.image.yaml up -d
docker compose -f compose.image.yaml exec calendard calendard doctor
```

Read the release notes first. The hub copies the database before it applies a migration, and the previous version's image is
still on disk, so rolling back means running the old tag again. If a migration changed the schema, restore the safety copy.

## Moving to another server

Install the hub there ([step 2](2-run-the-hub.md)) with the **same master key**, restore a backup, set the new
`CALENDAR_PUBLIC_URL`, add the new `/auth/google/callback` address to the Google client, stop the old hub, and pair the
app again with the new address. Your accounts keep working without reconnecting. Never run two hubs against the same
accounts at once.

## When something looks wrong

[Troubleshooting](../ops/troubleshooting.md) covers the common failures. Logs: `docker compose -f compose.image.yaml logs -f calendard`
(JSON, with no tokens and no event titles).
