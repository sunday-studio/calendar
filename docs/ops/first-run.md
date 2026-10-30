# Build from source with secret files (Docker on a Mac)

The step-by-step guides in [docs/guides](../guides/README.md) install the published image and are what most people want. This is the other route: build the image from source, with the master key and the Google client as files in `deploy/secrets/`. It was written for a Mac mini. Everything runs on the Mac mini (Apple Silicon, Docker Desktop or OrbStack, Tailscale already logged in) and the iPhone (Tailscale logged in). Commands run from the repo root. Nothing is exposed to the internet.

Why the host-Tailscale variant is the default: the Mac already is a tailnet node, so a second sidecar node would need an auth key, an ACL tag and a separate name, and it adds a TUN device inside the Docker VM, which is the least portable part of Docker on macOS. With `compose.host-tailscale.yaml` the hub listens on `127.0.0.1:8787` only (not even the LAN), `tailscale serve` adds HTTPS with a real certificate under the Mac's own name, and there is one fewer secret to guard. The sidecar variant (`deploy/compose.yaml`) stays available for servers that do not run Tailscale.

`COMPOSE` below means `docker compose -f deploy/compose.host-tailscale.yaml`.

Verification status: before deploying, run `deploy/verify.sh` (throwaway project, fake secrets, random port, cleans up; ~1 minute). On OrbStack/arm64 (Docker 29) it passed 24 checks: image build (17 MB, non-root), secrets-init, healthy hub, `doctor`, `/health`, `/v1/hello`, token, backup, read-only root with tmpfs, restart policy, clean logs, data persistence across `down`/`up`, upgrade and rollback. Steps marked **not run here** below need your Google account or your Mac's Tailscale and were not exercised.

## 0. Quick pre-flight
```sh
deploy/first-run.sh            # checks tools, secrets, settings; prints the exact next commands; safe to repeat
```
It never prints secrets and never overwrites files. Re-run it after each step until it says "All checks passed".

## 1. Google Cloud (about 15 minutes, once) (not run here: needs your Google console)
Use any Google account; it only owns the project, not your calendars.

1. console.cloud.google.com, project picker, **New project** (name: `calendar-hub`). Select it.
2. **APIs & Services, Library**, search "Google Calendar API", **Enable**.
3. **OAuth consent screen** (newer consoles call it "Google Auth Platform"): click Get started.
   - App name `Calendar`, support email and developer contact: yours.
   - Audience / user type: **External**. (Internal exists only inside a Workspace organization and would block your personal Gmail.)
4. **Scopes** (Data access): add `https://www.googleapis.com/auth/calendar`, plus `openid` and `.../auth/userinfo.email` (the hub requests `calendar openid email profile`). The calendar scope is "sensitive", which matters for step 6.
5. **Publish the app**: Audience, **Publish app** so the status is **In production**. Do this, do not stay in *Testing*: in Testing mode Google expires refresh tokens after 7 days and every account would say "Sign in needed" weekly.
6. You will not verify the app (verification is for public apps). Consequence: **each sign-in shows "Google hasn't verified this app"**. Click **Advanced**, then **Go to Calendar (unsafe)**, then allow. You see this once per account sign-in (and again on every reconnect), not on every sync. Unverified apps are capped at 100 users, far above what you need.
7. **Credentials, Create credentials, OAuth client ID**, type **Web application**, name `calendar-hub`.
   - **Authorized redirect URIs**: `https://<mac-mini>.<tailnet>.ts.net/auth/google/callback`. Get the exact host with `tailscale status --json | jq -r .Self.DNSName` (drop the trailing dot). Match it character for character: https, no trailing slash.
   - Leave "Authorized JavaScript origins" empty. Create, then keep the client ID and secret for step 2.
8. **Workspace accounts caveat**: a work or school account may be blocked by its admin ("access_blocked" / "app not allowed"): admins can restrict third-party apps. Ask the admin to allow the OAuth client ID (Admin console, Security, API controls, Manage third-party app access), or use that calendar through Google's sharing into a personal account. Personal Gmail accounts have no such issue.

## 2. Secrets (`deploy/secrets/`, mode 0600, git-ignored)
```sh
(umask 077; head -c32 /dev/urandom | base64 > deploy/secrets/master_key)    # or: deploy/first-run.sh --make-key
```
Then create `deploy/secrets/google_client.json` with exactly:
```json
{"client_id": "123-abc.apps.googleusercontent.com", "client_secret": "GOCSPX-..."}
```
(Google's downloaded JSON nests these under `"web"`; copy just the two values.) Then `chmod 600 deploy/secrets/*`.

**Back up `master_key` now, off this Mac** (password manager). Without it the database's sealed tokens and every backup are unreadable, and it cannot be recovered.

File ownership: the hub image runs as an unprivileged user that may not be able to read a 0600 file from your Mac user through Docker's file sharing, so compose copies the two files once into a root-managed volume with the right owner (`secrets-init` service). You do nothing; host files stay 0600.

Settings: `deploy/first-run.sh` creates `deploy/calendard.env` from the example and fills `CALENDAR_PUBLIC_URL=https://<mac-mini>.<tailnet>.ts.net` (no trailing slash). If you created the file yourself, set that line by hand. Optional: push (APNs) later, see `docs/ops/deploy.md`.

## 3. Build and start
```sh
docker compose -f deploy/compose.host-tailscale.yaml up -d --build
docker compose -f deploy/compose.host-tailscale.yaml ps          # calendard "healthy" after ~15 s
docker compose -f deploy/compose.host-tailscale.yaml exec calendard calendard doctor
```
Set `CALENDAR_HOST_PORT=<port>` in your shell (or in a `deploy/.env` file) to publish on another loopback port than 8787; then use the same port in step 4. The first build takes a few minutes and compiles natively for arm64 (about 15 s with a warm cache). Verified: `ps` shows `calendard` healthy and `secrets-init` exited 0; the image is about 17 MB and runs as `nonroot`. `doctor` must show no FAIL (warnings about "no accounts" or "no backup yet" are normal on a new install). Stopping: `$COMPOSE down` (data stays in the `calendar_data` volume; never add `-v`).

Start at boot: Docker must run for `restart: unless-stopped` to bring the hub back. Docker Desktop: Settings, General, "Start Docker Desktop when you sign in". OrbStack: Settings, "Start at login". A headless Mac mini needs automatic login (System Settings, Users & Groups) or the hub stays down after a reboot until you log in. Also disable sleep for the display-off case: System Settings, Energy, "Prevent automatic sleeping when the display is off".

## 4. HTTPS on the tailnet (not run here: `tailscale serve`, the admin console and the certificate need your tailnet; the compose side it points at was verified)
One-time in the Tailscale admin console (login.tailscale.com/admin/dns): MagicDNS on, and **HTTPS Certificates enabled**.
```sh
tailscale serve --bg --https=443 http://127.0.0.1:8787
tailscale serve status
curl -fsS https://<mac-mini>.<tailnet>.ts.net/health && echo
```
Syntax note: this is the Tailscale 1.52+ CLI (`serve --bg --https=443 <target>`); older releases used `tailscale serve https / http://...`. If your version complains, `tailscale serve --help` is authoritative (uncertain which exact version you run). The Mac App Store / standalone app ships its CLI at `/Applications/Tailscale.app/Contents/MacOS/Tailscale` (alias it: `alias tailscale=/Applications/Tailscale.app/Contents/MacOS/Tailscale`); the Homebrew `tailscale` formula puts `tailscale` on your PATH. The serve config survives reboots. Do not use `tailscale funnel` (that is the public internet).

Check from the phone too (Safari, same URL + `/health`, with Tailscale connected) before going on. A certificate error or timeout here means step 4 is not finished.

## 5. Create the admin token (verified)
```sh
docker compose -f deploy/compose.host-tailscale.yaml exec calendard calendard token create --name iphone --kind device --scope admin
```
It prints `id:` and `secret: cal_...` **once**. Put it in your password manager now. Connecting Google accounts needs an `admin` token. Lost it? Create another and `token revoke <id>` the old one.

## 6. Pair the phone and connect Google
1. iPhone: Tailscale connected. (Release iOS build: install per `docs/ops/testflight.md`.)
2. App, pairing screen: address `https://<mac-mini>.<tailnet>.ts.net`, paste the `cal_...` secret. Expect reachable, TLS ok, token ok, version ok.
3. Settings, Accounts, **Add Google account**. A sheet opens Google sign-in; choose the account, pass the "unverified app" screen (Advanced, Go to Calendar), allow calendar access. You return to the app. The phone must be on the tailnet for the redirect to reach the hub.
4. Repeat per account. Signing in with an account that is already connected is refused harmlessly.

## 6b. Notes
Steps 6 and 7 need real Google accounts and the phone: not run here.

## 7. First sync: what to expect
- The initial pull of a large calendar (years of events) takes seconds to a few minutes; events appear as calendars finish. After that: about 1 minute while the app is open, about 5 minutes otherwise.
- Watch it:
  ```sh
  docker compose -f deploy/compose.host-tailscale.yaml logs -f calendard             # JSON, no tokens or titles
  docker compose -f deploy/compose.host-tailscale.yaml exec calendard calendard doctor
  curl -H "Authorization: Bearer cal_..." https://<mac-mini>.<tailnet>.ts.net/v1/status
  ```
  App: Settings, Server shows status and the Sync log (per-pass errors). Three failures in a row mark an account *Problem*; it retries with backoff and recovers alone. `needs_reauth` ("Sign in needed") needs you: Settings, Accounts, Reconnect.
- Then work through `docs/ops/SMOKE.md` with real accounts.

## 8. Backups on the Mac mini (`calendard backup` verified: writes an encrypted, verified file in `/data/backups`; the offsite script, launchd and Time Machine parts not run here)
- The hub writes an encrypted, verified snapshot nightly at 03:00 to the `calendar_data` volume (`/data/backups`, last 14). That lives inside Docker's disk image, so it is not enough by itself. `/v1/status` warns when the newest is over 36 hours old.
- Get copies onto the Mac's normal filesystem (and so into **Time Machine**): run the offsite script into a folder that Time Machine includes, ideally on another disk too:
  ```sh
  deploy/backup-offsite.sh "$HOME/Backups/calendar"          # copies calendar-*.db.enc, warns if stale
  ```
  Schedule it with launchd (preferred on macOS) or cron. cron: `crontab -e` and add `30 3 * * * /Users/<you>/.../calendar/deploy/backup-offsite.sh $HOME/Backups/calendar` (cron needs Full Disk Access if the target is protected, and Docker must be running). launchd: a `~/Library/LaunchAgents/com.yourname.calendar-backup.plist` with `ProgramArguments` (the script and the folder) and `StartCalendarInterval` Hour 3 Minute 30, then `launchctl load` it.
- The backup files are encrypted with a key derived from the master key, so the folder needs no extra protection, but **keep the master key somewhere else** (password manager, not Time Machine alone, not next to the backups). Both are needed to restore.
- Rehearse once: `make backup-drill`. Restore steps: `docs/ops/RUNBOOK.md`.

## 9. Updating, rollback (exercised by `deploy/verify.sh` with two image tags)
Every image is tagged by `CALENDAR_VERSION` (default `dev`), so an old build stays on disk and rollback needs no rebuild.
```sh
export CALENDAR_VERSION=0.1.0                 # the tag you run now; first deployment: build it as in step 3
docker compose -f deploy/compose.host-tailscale.yaml exec calendard calendard backup     # safety copy first
# get the new source (git pull or copy the new files), then build and switch to the new tag:
CALENDAR_VERSION=0.2.0 docker compose -f deploy/compose.host-tailscale.yaml up -d --build
docker compose -f deploy/compose.host-tailscale.yaml exec calendard calendard doctor
curl -s -H "Authorization: Bearer cal_..." http://127.0.0.1:8787/v1/hello      # "version":"0.2.0"
# roll back: same command with the old tag, no --build (the old image is still there)
CALENDAR_VERSION=0.1.0 docker compose -f deploy/compose.host-tailscale.yaml up -d
```
Verified with tags `verify` then `verify2` and back: only the calendard container is recreated, the `data` volume (database, tokens, backups) stays, and `/v1/hello` reports the running version. `calendard` has no `version` subcommand; use `/v1/hello` or the `listening` log line. Migrations run at start and first copy the database to `calendar.db.pre-migrate-<time>`; a rollback after a migration that changed the schema needs that copy (or the newest backup) restored as in `docs/ops/RUNBOOK.md`: not exercised here, because the test images share one schema. To use the published image instead of building, see `docs/ops/ci.md`.

## 10. Common failures

| Symptom | Cause | Fix |
|---|---|---|
| Google: `Error 400: redirect_uri_mismatch` | the redirect URI in the Web client differs from `CALENDAR_PUBLIC_URL` + `/auth/google/callback` (http vs https, trailing slash, wrong tailnet or host name, edited env but hub not restarted) | make both identical (the error page shows the URI the hub sent); after editing `calendard.env` run `$COMPOSE up -d --force-recreate calendard`; Google can take a few minutes to apply changes |
| Google: `access_blocked` / "has not completed the verification process" / "can only be used by test users" | consent screen still in *Testing* without your address as test user, or Workspace admin blocks the app | publish to In production (step 1.5); for Workspace see step 1.8 |
| "Google hasn't verified this app" | expected for unverified apps | Advanced, Go to Calendar (unsafe); not an error |
| `invalid_grant` in logs or an account says "Sign in needed" | refresh token revoked, password change with sign-out, 7-day expiry in Testing mode, or client secret rotated against old tokens | Settings, Accounts, Reconnect (same Google account); make sure the app is In production |
| Phone says Unreachable / `curl` times out | phone not on the tailnet, `tailscale serve` not configured or lost, hub down, MagicDNS/HTTPS off | Tailscale app connected; `tailscale serve status` (re-run the step 4 command); `$COMPOSE ps`; `curl https://<name>/health` from the Mac, then from the phone |
| Cert error / `curl: SSL` | HTTPS Certificates not enabled for the tailnet, or wrong host name | enable them in the admin console (step 4); use the exact `DNSName` |
| `401` from the API / app says token invalid | token typed wrong, revoked, or a read-scope token used for an admin action | `$COMPOSE exec calendard calendard token list`; create a new one with `--scope admin` and re-pair |
| `Bind for 127.0.0.1:8787 failed: port is already allocated` / "address already in use" | another process (a dev server, an old `calendard`, a second compose project) holds 8787 | `lsof -nP -iTCP:8787 -sTCP:LISTEN`; stop it (`$COMPOSE down`, or `docker ps`), or set `CALENDAR_HOST_PORT=8788` (and use the same port in the `tailscale serve` target) |
| Container exits at start: `permission denied` reading `/run/calendar-secrets/...`, or `master key missing` | secrets-init did not run or a secret file is empty/misnamed | `$COMPOSE logs secrets-init calendard`; check file names in `deploy/secrets/`; `$COMPOSE up -d --force-recreate secrets-init calendard` |
| `doctor` FAIL on Google file | `google_client.json` not the flat `{client_id, client_secret}` shape | rewrite it as in step 2 |
| Hub missing after a Mac reboot | Docker did not start (no login) | step 3, "Start at boot" |
