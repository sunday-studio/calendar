# Running the published image with Docker Compose

The simplest install: pull the released image, put every setting in one env file, no secret files and no build. Use this when you cannot or do not want to mount files (a panel such as Portainer, a NAS, a VPS). The Mac-mini walkthrough with secret files is [first-run.md](first-run.md).

## What you need

- Docker with Compose v2 on the server, and a token that can read the private image (`docker login ghcr.io`, token scope `read:packages`).
- The release bundle (`calendar-deploy-X.Y.Z.tar.gz` from the GitHub Release), which has `deploy/compose.image.yaml` and `deploy/calendard.env.example`.
- HTTPS for the hub's public address. Google only accepts an https redirect URI. With Tailscale: `tailscale serve --bg --https=443 http://127.0.0.1:8787` gives `https://<host>.<tailnet>.ts.net`. Any reverse proxy that terminates TLS in front of `127.0.0.1:8787` works the same way.
- The Google OAuth client id and secret, and the APNs key (`.p8`) with its key id and team id.

## The compose file

`deploy/compose.image.yaml`:

```yaml
name: calendar

services:
  calendard:
    image: ghcr.io/sunday-studio/calendar-hub:${CALENDAR_VERSION:-latest}   # pin an exact version in production, e.g. 0.1.0
    pull_policy: always
    env_file: ./calendard.env
    environment:                      # fixed by this layout; everything configurable lives in calendard.env
      CALENDAR_ADDR: 0.0.0.0:8787     # inside the container; the published port below is what limits exposure
      CALENDAR_DB: /data/calendar.db
      CALENDAR_BACKUP_DIR: /data/backups
    ports:
      - "127.0.0.1:${CALENDAR_HOST_PORT:-8787}:8787"   # loopback only; `tailscale serve` (or a reverse proxy) fronts it
    volumes:
      - data:/data                    # the database and the nightly encrypted backups
    read_only: true
    tmpfs: [/tmp]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "calendard", "health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 15s
    stop_grace_period: 20s

volumes:
  data:
```

If your host has no Tailscale and you want the hub reachable on a LAN address, change the port line to `"<lan-ip>:8787:8787"` and put TLS in front of it. Do not publish plain http to the internet.

## The env file

`calendard.env` next to the compose file, `chmod 600`. Start from `deploy/calendard.env.example`. A complete example with fake values:

```sh
# --- Required ---
CALENDAR_PUBLIC_URL=https://calendar.your-tailnet.ts.net     # no trailing slash; Google redirect URI is this + /auth/google/callback
CALENDAR_MASTER_KEY=<output of: head -c32 /dev/urandom | base64>
CALENDAR_GOOGLE_CLIENT_ID=1234567890-abc.apps.googleusercontent.com
CALENDAR_GOOGLE_CLIENT_SECRET=GOCSPX-...

# --- Push notifications (leave the four empty to run without push) ---
CALENDAR_APNS_KEY=<output of: base64 -i AuthKey_XXXX.p8 | tr -d '\n'>
CALENDAR_APNS_KEY_ID=XXXXXXXXXX
CALENDAR_APNS_TEAM_ID=YYYYYYYYYY
CALENDAR_APNS_TOPIC=dev.casprine.calendar

# --- Tuning (defaults shown) ---
CALENDAR_SYNC_FAST=60s
CALENDAR_SYNC_SLOW=5m
CALENDAR_BACKUP_KEEP=14
CALENDAR_BACKUP_HOUR=3
CALENDAR_ICS_POLL=15m

# --- Telemetry (off unless an endpoint is set) ---
OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp.example.com
OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20<token>
OTEL_SERVICE_NAME=calendard
OTEL_RESOURCE_ATTRIBUTES=deployment.environment=prod
```

`env_file` takes values as is: no quotes, no `export`, one line each, so the `.p8` goes in as base64 on one line. Compose interpolates `$` in env files, so wrap a value that contains one in single quotes (base64 never does).

## First start

```sh
docker login ghcr.io
docker compose -f compose.image.yaml up -d
docker compose -f compose.image.yaml exec calendard calendard doctor     # every check should be ok; warnings say what is off
tailscale serve --bg --https=443 http://127.0.0.1:8787
curl -fsS https://calendar.your-tailnet.ts.net/health
```

Then in the iPhone app pair with the hub address and its first token (created as in [first-run.md](first-run.md), steps 5 and 6). Register `https://calendar.your-tailnet.ts.net/auth/google/callback` as the redirect URI for the Google client before connecting an account.

Update: set `CALENDAR_VERSION` (or edit the image tag), then `docker compose -f compose.image.yaml pull && docker compose -f compose.image.yaml up -d`. The hub copies the database before applying a migration. Backups, restore and rollback: [RUNBOOK.md](RUNBOOK.md).

## Every variable

Secrets as values (a `_FILE` variant exists for each, for the secret-file composes; the file wins if both are set).

| Variable | Default | Meaning |
|---|---|---|
| `CALENDAR_MASTER_KEY` / `CALENDAR_MASTER_KEY_FILE` | none, required | 32 bytes, base64 or hex. Seals OAuth refresh tokens and encrypts backups. Hub refuses to start without it. Keep a copy outside the server: without it the DB and backups cannot be read. |
| `CALENDAR_GOOGLE_CLIENT_ID` + `CALENDAR_GOOGLE_CLIENT_SECRET` / `CALENDAR_GOOGLE_CLIENT_FILE` | none | The Google OAuth client. Unset = Google sign-in disabled. Needs `CALENDAR_PUBLIC_URL`. File form is JSON `{"client_id":"…","client_secret":"…"}`. |
| `CALENDAR_APNS_KEY` / `CALENDAR_APNS_KEY_FILE` | none | The `.p8` auth key: base64 of the file, the PEM itself, or the PEM on one line with `\n`. Unset = push disabled. |
| `CALENDAR_APNS_KEY_ID`, `CALENDAR_APNS_TEAM_ID` | none | From the Apple developer portal; required when a key is set. |
| `CALENDAR_APNS_TOPIC` | `dev.casprine.calendar` | The app's bundle id. |

Server and storage.

| Variable | Default | Meaning |
|---|---|---|
| `CALENDAR_PUBLIC_URL` | none | The https address clients and Google use. Required when Google is enabled. |
| `CALENDAR_ADDR` | `0.0.0.0:8787` in the image | Listen address inside the container. The compose file pins it. |
| `CALENDAR_DB` | `/data/calendar.db` | SQLite file. The compose file pins it. |
| `CALENDAR_BACKUP_DIR` | `/data/backups` in the compose | Nightly backup folder. Unset = no backups. |
| `CALENDAR_BACKUP_KEEP` | `14` | How many backups to keep (1 to 3650). |
| `CALENDAR_BACKUP_HOUR` | `3` | Local hour of the nightly backup (0 to 23). |
| `CALENDAR_BACKUP_PLAIN` | off | `1` = do not encrypt backups. Not recommended. |

Sync and feeds.

| Variable | Default | Meaning |
|---|---|---|
| `CALENDAR_SYNC_FAST` | `1m` | Poll cadence while an app is connected. |
| `CALENDAR_SYNC_SLOW` | `5m` | Poll cadence otherwise. |
| `CALENDAR_ICS_POLL` | `15m` | Poll cadence for ICS subscription feeds. |
| `CALENDAR_ICS_ALLOW_PRIVATE` | off | `1` = allow ICS feeds on loopback and private addresses. |

MCP, abuse limits and alfather.

| Variable | Default | Meaning |
|---|---|---|
| `CALENDAR_MCP_DISABLE` | off | `1` = do not serve the hub's own `POST /mcp`. |
| `CALENDAR_MCP_RATE` | `120` | `/mcp` calls per token per minute. |
| `CALENDAR_MCP_MAX_BODY` | `262144` | Bytes accepted on `/mcp`. |
| `CALENDAR_AUTH_FAIL_LIMIT` | `20` | Invalid-token attempts per client per window before blocking. |
| `CALENDAR_AUTH_FAIL_WINDOW` | `1m` | That window. |
| `CALENDAR_ALFATHER_URL` | none | MCP endpoint of alfather for natural-language event entry. Off unless set. |
| `CALENDAR_ALFATHER_PARSE_TOOL` | `parse_event` | Tool name to call there. |
| `CALENDAR_ALFATHER_TOKEN` | none | Bearer token for it. |

Durations are Go durations (`30s`, `5m`, `1h`).

Telemetry (OpenTelemetry). Off unless an OTLP endpoint is set; with none set the hub makes no exporter and no network calls. Standard variables only; the full behaviour is in [observability.md](../engineering/observability.md).

| Variable | Default | Meaning |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | none | Turns telemetry on for traces, metrics and logs. |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `_METRICS_ENDPOINT`, `_LOGS_ENDPOINT` | none | Per-signal full URLs; one alone enables only that signal. |
| `OTEL_EXPORTER_OTLP_HEADERS` | none | `key=value,key2=value2` for the backend's auth. A space inside a value is `%20`. It is a secret. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | Or `grpc`. |
| `OTEL_SERVICE_NAME` | `calendard` | Service name in the backend. |
| `OTEL_RESOURCE_ATTRIBUTES` | none | e.g. `deployment.environment=prod`. |
| `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` | `parentbased_always_on` | e.g. `parentbased_traceidratio` and `0.25`. |
| `OTEL_METRIC_EXPORT_INTERVAL` | `60000` | Milliseconds. |
| `OTEL_TRACES_EXPORTER`, `OTEL_METRICS_EXPORTER`, `OTEL_LOGS_EXPORTER` | on | `none` switches that signal off. |
| `OTEL_SDK_DISABLED` | off | `true` = everything off even with an endpoint. |

`calendard doctor` prints the telemetry line with the endpoint host and masked headers, and probes the endpoint for 2 seconds. A collector that is down is a warning, never a failure, and never slows the hub.

Development only (do not set in production): `CALENDAR_DEV`, and `CALENDAR_GOOGLE_AUTH_URL`, `_TOKEN_URL`, `_API_BASE`, `_USERINFO_URL`, which point the hub at a fake Google.
