# Runbook

Short answers for the things you will actually need to do. Commands run from the repo root on the Docker host (they show `deploy/compose.yaml`, the sidecar variant; with the default host-Tailscale setup use `deploy/compose.host-tailscale.yaml` instead, and see docs/first-run.md);
`hub` below means `docker compose -f deploy/compose.yaml exec calendard calendard`. Check first: `hub doctor`.

## First install
1. Create the secrets in `deploy/secrets/` (see README.md): `master_key`, `google_client.json`, `ts_key`.
2. `cp deploy/calendard.env.example deploy/calendard.env` and set `CALENDAR_PUBLIC_URL` to `https://calendar.<tailnet>.ts.net`.
3. In the Google Cloud console add `https://calendar.<tailnet>.ts.net/auth/google/callback` to the OAuth client.
4. `docker compose -f deploy/compose.yaml up -d --build`, then `hub doctor` (anything marked FAIL must be fixed).
5. Pair the phone: `hub token create --name iphone --kind device --scope admin` prints a `cal_...` secret once; type it into the app.
6. In the app: add each Google account (the phone must be on the tailnet for the sign-in redirect).
7. **Back up the master key now**, somewhere that is not this server (password manager). Without it the database's secrets and every backup are unreadable.

## Reconnect an account that says "Sign in needed"
App: Settings, Accounts, the account, Reconnect. Sign in with the same Google account (a different one is refused and nothing changes).
Causes: you revoked the app's access in Google, changed the Google password with "sign out everywhere", or the consent screen is still in
*Testing* (tokens then expire after 7 days: set it to *In production*).

## Remove or pause an account
App: Settings, Accounts. Removing deletes that account's mirrored events and its sealed token from the hub; Google is untouched.

## Backups
- The hub writes an encrypted, verified snapshot every night (03:00 local, `CALENDAR_BACKUP_HOUR`) to `/data/backups` (volume `calendar_data`), keeping `CALENDAR_BACKUP_KEEP` (14).
- `/v1/status` (and the app's Server screen) warns when the newest backup is over 36 hours old or none exists.
- Copy them off the machine: `deploy/backup-offsite.sh /mnt/nas/calendar-backups` from cron on the host (also warns when stale).
- Make one now: `hub backup`.  Check the whole procedure without touching real data: `make backup-drill`.

## Restore from backup
1. Stop the hub: `docker compose -f deploy/compose.yaml stop calendard`.
2. Find the file (`ls` the volume or your off-site copy). It must be restored with the master key that made it.
3. Restore into the volume (a one-off container; the hub image has no shell):
   ```bash
   docker compose -f deploy/compose.yaml run --rm --no-deps -v "$PWD/restore:/in:ro" calendard \
     restore --from /in/calendar-YYYYMMDD-HHMMSS.db.enc --force
   ```
   The old database is kept next to it as `calendar.db.pre-restore-<time>`.
4. `docker compose -f deploy/compose.yaml up -d`. The revision is advanced by 100000, so every phone notices and refetches.
5. Accounts reconnect themselves (their sealed tokens are in the backup). Anything created since the backup exists in Google, so the next sync brings it back; only hub-only data is lost: reminder overrides, automations and API tokens made after the backup.

## Rotate the master key
1. `hub backup`, and keep the OLD key (old backups need it).
2. Make the new key: `head -c32 /dev/urandom | base64 > new_key`.
3. Stop the hub, then re-seal: `docker compose -f deploy/compose.yaml run --rm --no-deps -v "$PWD/new_key:/new_key:ro" calendard rekey --new-key-file /new_key`.
   (A safety backup is written to `/data/backups/pre-rekey` first; nothing changes if any secret cannot be opened.)
4. Replace `deploy/secrets/master_key` with the new key and start the hub. Take a fresh `hub backup` and update your off-site copy of the key.

## Rotate API tokens
Make a new token (`hub token create ...` or app: Settings, API tokens), put it in the device or script, then revoke the old one
(`hub token revoke <id>` or in the app). Tokens are stored hashed; `last used` shows which are still in use. Scripts should have `read` scope.

## Rotate the Google client secret
Create a new secret in Google Cloud, update `deploy/secrets/google_client.json`, `docker compose ... up -d`. Existing sign-ins keep working.

## Upgrade
1. `hub backup`. 2. `git pull`, `docker compose -f deploy/compose.yaml up -d --build`.
Migrations run at start; before applying any, the hub copies the database to `calendar.db.pre-migrate-<time>` (the last three are kept).
To roll back: stop, restore the previous image, and restore the `.pre-migrate-` copy as `calendar.db`.

## Logs and health
`docker compose -f deploy/compose.yaml logs -f calendard` (JSON; no tokens, no search words, no event titles at info).
`hub doctor` checks key, database, accounts, backups, Google/APNs files. `GET /v1/status` and `GET /metrics` (read token) give the live picture.
Every response carries `X-Request-Id`; the same id is in the log line.

## "The app says Unreachable"
1. Phone on the tailnet? (Tailscale app connected.) 2. `docker compose ps`: both containers healthy? 3. `tailscale serve status` inside the sidecar shows the HTTPS proxy.
4. `curl https://calendar.<tailnet>.ts.net/health` from another tailnet device. 5. Token revoked? `hub token list`.

## "An account is stuck syncing / has an error"
`GET /v1/sync/runs?accountId=...` (app: Server, Sync log) shows the error per pass. 3 failures in a row mark the account *Problem*; it retries with backoff (1 to 15 min)
and recovers by itself. `needs_reauth` needs a human (see Reconnect).

## Push notifications do nothing
`hub doctor` must show `apns key ok`; the key file, Key ID, Team ID and bundle id must match the app. Sandbox builds register with environment `sandbox`.
A dead device token is removed automatically after Apple rejects it; re-open the app to register again.
