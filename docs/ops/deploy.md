# Deploying calendard

The hub runs on a home server as a Docker Compose stack. Nothing is exposed to the public internet; the phone reaches the hub over the tailnet.

**Start with the [step-by-step guides](../guides/README.md)** (Google Cloud, the hub, HTTPS, the app, pairing, backups) and run `deploy/first-run.sh` to check your setup.

Two variants, same `calendard`:
- `compose.host-tailscale.yaml` (**default**, e.g. a Mac mini that already runs Tailscale): no sidecar and no auth key; `calendard` is published on `127.0.0.1:8787` only and the host's `tailscale serve` provides HTTPS.
- `compose.yaml` (alternative): a Tailscale container shares its network with `calendard` (own tailnet node, needs `secrets/ts_key`); for servers that do not run Tailscale themselves.

## Secrets (`deploy/secrets/`, git-ignored, mode 0600)

| File | What | How to make it |
|---|---|---|
| `master_key` | 32-byte key sealing OAuth refresh tokens in the DB. Hub refuses to start without it. Back it up separately from the DB. | `head -c32 /dev/urandom \| base64 > deploy/secrets/master_key` |
| `google_client.json` | Google Web OAuth client: `{"client_id": "...", "client_secret": "..."}` | Google Cloud Console: enable Calendar API, consent screen External then In production, Web client with redirect URI `https://<host>.<tailnet>.ts.net/auth/google/callback` |
| `ts_key` | (sidecar variant only) Tailscale auth key for the sidecar (reusable, tagged `tag:calendar`) | Tailscale admin console, Settings, Keys. The file holds only the key (`tskey-auth-...`) |
| `apns_key.p8` | APNs auth key (only needed for push), plus Team ID and Key ID in `calendard.env`; also add it to the `secrets-init` command and volumes and set `CALENDAR_APNS_KEY_FILE` in the compose file | Apple Developer, Keys |

Google's file is `deploy/secrets/google_client.json`. All secret files: mode 0600, never committed (`deploy/.gitignore`).

## Environment

The published image with everything as env values (no secret files), and the complete variable list with defaults: [compose-example.md](compose-example.md).

| Variable | Meaning |
|---|---|
| `CALENDAR_ADDR` | listen address, default `127.0.0.1:8787` (the host-Tailscale compose sets `0.0.0.0:8787` inside the container, published on host loopback only) |
| `CALENDAR_DB` | SQLite path, e.g. `/data/calendar.db` |
| `CALENDAR_PUBLIC_URL` | `https://<host>.<tailnet>.ts.net`; the Google redirect URI is this plus `/auth/google/callback` (required when Google is enabled) |
| `CALENDAR_MASTER_KEY_FILE` | `/run/calendar-secrets/master_key` (set by compose) |
| `CALENDAR_GOOGLE_CLIENT_FILE` | `/run/calendar-secrets/google_client.json` (set by compose); unset = Google sign-in disabled (503 `unavailable`) |
| `CALENDAR_BACKUP_DIR` / `_KEEP` / `_HOUR` / `_PLAIN` | nightly backup folder (unset = none), how many to keep (14), local hour (3), `1` = do not encrypt |
| `CALENDAR_APNS_KEY_FILE` or `CALENDAR_APNS_KEY` (value) / `_KEY_ID` / `_TEAM_ID` / `_TOPIC` | push (unset key = push disabled); topic defaults to `studio.thesunday.calendar` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` (+ `OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_TRACES_SAMPLER`) | OpenTelemetry export, off unless an endpoint is set; standard OTEL_* variables, passed through `calendard.env`; see `docs/engineering/observability.md` |
| `CALENDAR_SYNC_FAST` / `_SLOW` | poll cadence with / without the app connected (60s / 5m) |

## Files here

| File | Purpose |
|---|---|
| `compose.host-tailscale.yaml` | default: `calendard` on 127.0.0.1:8787 + host `tailscale serve` (read-only root, no capabilities) |
| `compose.yaml` | alternative: Tailscale sidecar + `calendard` |
| `first-run.sh` | pre-flight checks and the exact next commands (never prints secrets, never overwrites) |
| `serve.json` | sidecar variant only: its `tailscale serve` HTTPS proxy to 127.0.0.1:8787 |
| `calendard.env.example` | non-secret settings; copy to `calendard.env` |
| `backup-offsite.sh` | copy encrypted backups to a NAS / disk, warn when stale |
| `drill.sh` | backup, lose the database, restore, wrong key refused (`make backup-drill`) |
| `RUNBOOK.md` | reconnect, backup, restore, key and token rotation, upgrade, troubleshooting |
| `SMOKE.md` | the manual live checklist (`make smoke`) |

## Compose notes

- Secrets reach the hub through a one-shot `secrets-init` container that copies `master_key` and `google_client.json` into a volume owned by the image's `nonroot` uid (65532, mode 0400), mounted read-only at `/run/calendar-secrets`. Reason: Docker on macOS bind-mounts host files through a VM, and a 0600 file owned by your Mac user can show up unreadable to a non-root container user. Host files stay 0600. After changing a secret: `docker compose -f <file> up -d --force-recreate secrets-init calendard`.
- Persist `/data` (SQLite, WAL; named volume `calendar_data`). `restart: unless-stopped`; healthcheck is `calendard health` (the image is distroless, there is no shell). Images build natively for arm64 on Apple Silicon.
- Docker must start by itself after a reboot for `restart` to matter: Docker Desktop, Settings, "Start Docker Desktop when you sign in" (needs a logged-in user; OrbStack starts at login too, or use auto-login on a headless Mac mini).
- Pair the phone: `docker compose -f deploy/compose.host-tailscale.yaml exec calendard calendard token create --name iphone --kind device --scope admin` prints the secret once. Connecting or removing accounts needs an `admin` token.
- The phone must be on the tailnet while connecting a Google account, because Google redirects the browser session to the hub's `ts.net` URL.
