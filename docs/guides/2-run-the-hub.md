---
title: Run the hub
description: Run the Calendar hub with Docker Compose on your own server.
step: 2
readTime: 10 min
requires: Docker with Compose v2 on a server
---

# 2. Run the hub

Run the hub with Docker Compose on a server you control. It is one container, one volume and one env file.

**Where to run it.** On a server your phone can reach: a small VPS, a home server, or a machine on your Tailscale network.
It needs Docker with Compose v2. You also need the Google client id and secret from [step 1](1-google-cloud.md).

The hub ships as one image. It listens on the server's loopback only, so nothing is reachable from outside until you add
HTTPS in [step 3](3-https.md).

## Get the files

You need two files side by side in one folder: `compose.image.yaml` and `calendard.env`. There are two ways to get them.
Pick one.

|  | Release bundle | Write the files yourself |
|---|---|---|
| Best for | Starting from the tested defaults | Panels such as Portainer, or no shell access to unpack |
| You get | `compose.image.yaml`, `calendard.env.example`, helper scripts | Only the two files below |
| Env file | Copy the example and fill it in | Create it with the four required lines |

### Option 1: Release bundle

1. Download `calendar-deploy-<version>.tar.gz` from the [latest release](https://github.com/sunday-studio/calendar/releases/latest)
   and unpack it on the server:

   ```bash
   tar -xzf calendar-deploy-0.1.0.tar.gz && cd calendar-deploy-0.1.0/deploy
   ```

2. Make the env file and lock it down:

   ```bash
   cp calendard.env.example calendard.env && chmod 600 calendard.env
   ```

### Option 2: Write the files yourself

Create a folder, then save these two files in it. Fill in the env file as described in [Configure it](#configure-it).

```yaml title="compose.image.yaml"
name: calendar

services:
  calendard:
    image: ghcr.io/sunday-studio/calendar-hub:${CALENDAR_VERSION:-latest}   # pin an exact version, e.g. 0.1.0
    pull_policy: always
    env_file: ./calendard.env
    environment:                      # fixed by this layout; everything else lives in calendard.env
      CALENDAR_ADDR: 0.0.0.0:8787     # inside the container
      CALENDAR_DB: /data/calendar.db
      CALENDAR_BACKUP_DIR: /data/backups
    ports:
      - "127.0.0.1:${CALENDAR_HOST_PORT:-8787}:8787"   # loopback only; the next step puts HTTPS in front
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

```sh title="calendard.env"
CALENDAR_PUBLIC_URL=https://<your-hub-address>
CALENDAR_MASTER_KEY=<the key from Configure it>
CALENDAR_GOOGLE_CLIENT_ID=<from step 1>
CALENDAR_GOOGLE_CLIENT_SECRET=<from step 1>
```

Then lock the env file down:

```bash
chmod 600 calendard.env
```

The compose file runs the container read-only with all capabilities dropped. Keep the `data` volume: it holds your database
and backups.

### Private image

If the image is private, log Docker in to GitHub's registry once. Create a token with only the `read:packages` scope
(GitHub, Settings, Developer settings, Tokens (classic)):

```bash
docker login ghcr.io -u <your-github-user>
```

Public images need no login. The image supports amd64 and arm64.

## Configure it

Create the master key. It encrypts your Google tokens and your backups.

```bash
head -c32 /dev/urandom | base64
```

> [!WARNING]
> Copy the master key into your password manager now. Without it nothing can be restored, and a backup is unreadable.

Edit `calendard.env` and set these four values. The hub will not start without them.

| Variable | What it is |
|---|---|
| `CALENDAR_PUBLIC_URL` | The address your phone uses to reach the hub, from [step 3](3-https.md). Must be `https`, with no trailing slash. |
| `CALENDAR_MASTER_KEY` | The key you just made. |
| `CALENDAR_GOOGLE_CLIENT_ID` | From [step 1](1-google-cloud.md). |
| `CALENDAR_GOOGLE_CLIENT_SECRET` | From [step 1](1-google-cloud.md). |

Everything else is optional and has a working default. The ones you are most likely to change:

| Variable | Default | What it does |
|---|---|---|
| `CALENDAR_BACKUP_HOUR` | `3` | Hour of the nightly backup, server local time. |
| `CALENDAR_BACKUP_KEEP` | `14` | How many backups to keep. |
| `CALENDAR_SYNC_FAST` | `60s` | Sync interval while the app is connected. |
| `CALENDAR_SYNC_SLOW` | `5m` | Sync interval otherwise. |
| `CALENDAR_VERSION` | `latest` | Image tag. Set an exact version, such as `0.1.0`, in production. Read by the compose file. |
| `CALENDAR_HOST_PORT` | `8787` | Port on the server's loopback. Read by the compose file. |

Push notifications can wait: see [push.md](../ops/push.md). Every setting, with its default, is in
[compose-example.md](../ops/compose-example.md).

## Start it

```bash
docker compose -f compose.image.yaml up -d
docker compose -f compose.image.yaml ps
docker compose -f compose.image.yaml exec calendard calendard doctor
```

`calendard` shows `healthy` after about 15 seconds. `doctor` should show no FAIL. Warnings about "no accounts" and "no backup
yet" are normal on a new install.

Create the first token. The app uses it to pair.

```bash
docker compose -f compose.image.yaml exec calendard calendard token create --name iphone --kind device --scope admin
```

> [!IMPORTANT]
> The token is printed once. Save it in your password manager.

## Verify it is running

- [ ] `docker compose -f compose.image.yaml ps` shows `calendard` as `healthy`.
- [ ] `curl http://127.0.0.1:8787/health` on the server answers `ok`.
- [ ] `calendard doctor` shows no FAIL.
- [ ] You saved the master key and the first token.

Nothing is reachable from outside yet. That is the next step: [put HTTPS in front of it](3-https.md).

## Day to day

Run these in the folder with `compose.image.yaml`.

| Command | What it does |
|---|---|
| `docker compose -f compose.image.yaml ps` | Shows the container and whether it is healthy. |
| `docker compose -f compose.image.yaml logs -f calendard` | Follows the log. JSON, with no tokens and no event titles. |
| `docker compose -f compose.image.yaml restart` | Restarts the hub after you change `calendard.env`. |
| `docker compose -f compose.image.yaml exec calendard calendard doctor` | Checks the key, database, accounts, backups and telemetry. |
| `docker compose -f compose.image.yaml down` | Stops it. The data stays in the `data` volume. |

To update to a new version, see [Keep it running](6-keep-it-running.md#updating).

## Optional: watch the hub

Skip this on a first install; you can add it any time. The hub can send traces, metrics and logs to any OpenTelemetry (OTLP)
backend, such as Grafana Cloud, Honeycomb, SigNoz or your own collector. It is **off** unless you set an endpoint: with
nothing set, the hub starts no exporter and makes no network calls. The container logs above work with or without it.

1. Add to `calendard.env`. The `OTEL_*` examples for each backend are already in the example file, commented out.

   ```sh
   OTEL_EXPORTER_OTLP_ENDPOINT=https://<your-otlp-endpoint>
   OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20<token>    # only if the backend wants a key; a space is written %20
   OTEL_RESOURCE_ATTRIBUTES=deployment.environment=prod
   ```

   The headers hold a secret, so keep the file at `chmod 600`. Add `OTEL_EXPORTER_OTLP_PROTOCOL=grpc` if your backend only
   speaks gRPC.

2. Restart and check:

   ```bash
   docker compose -f compose.image.yaml up -d
   docker compose -f compose.image.yaml logs calendard | grep telemetry     # "telemetry enabled endpoint=... signals=traces,metrics,logs"
   docker compose -f compose.image.yaml exec calendard calendard doctor     # the telemetry line says "endpoint reachable"
   ```

   Traces show up within seconds. Metrics arrive after one export interval (60 seconds by default).

| Variable | Default | What it does |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | none | Turns telemetry on for traces, metrics and logs. |
| `OTEL_EXPORTER_OTLP_HEADERS` | none | `key=value` pairs for the backend's auth. A secret. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | Or `grpc`. |
| `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` | `parentbased_always_on` | For example `parentbased_traceidratio` and `0.25`. |
| `OTEL_METRIC_EXPORT_INTERVAL` | `60000` | Milliseconds. |
| `OTEL_TRACES_EXPORTER`, `OTEL_METRICS_EXPORTER`, `OTEL_LOGS_EXPORTER` | on | `none` switches that signal off. |
| `OTEL_SDK_DISABLED` | off | `true` turns everything off, even with an endpoint set. |

If the backend is down or slow, the hub keeps working and drops the telemetry until it is back. What is recorded, and how to
try it locally with a throwaway Grafana, is in [observability.md](../engineering/observability.md).

## If something fails

See [troubleshooting](../ops/troubleshooting.md).
