# 2. Run the hub

The hub is one container. You need Docker with Compose v2 on a server, and the release bundle.

## Get the bundle and the image

1. Download `calendar-deploy-<version>.tar.gz` from the [latest release](https://github.com/sunday-studio/calendar/releases/latest)
   and unpack it on the server:

   ```bash
   tar -xzf calendar-deploy-0.1.0.tar.gz && cd calendar-deploy-0.1.0/deploy
   ```

2. If the image is private, log Docker in to GitHub's registry once. Create a token with only the `read:packages`
   scope (GitHub, Settings, Developer settings, Tokens (classic)):

   ```bash
   docker login ghcr.io -u <your-github-user>
   ```

   Public images need no login. The image supports amd64 and arm64.

## Configure it

3. Make the env file and lock it down:

   ```bash
   cp calendard.env.example calendard.env && chmod 600 calendard.env
   ```

4. Create the master key. It encrypts your Google tokens and your backups. **Copy it into your password manager now**:
   without it nothing can be restored.

   ```bash
   head -c32 /dev/urandom | base64
   ```

5. Edit `calendard.env` and set at least:

   ```sh
   CALENDAR_PUBLIC_URL=https://<your-hub-address>     # the address from step 3, no trailing slash
   CALENDAR_MASTER_KEY=<the key from step 4>
   CALENDAR_GOOGLE_CLIENT_ID=<from step 1>
   CALENDAR_GOOGLE_CLIENT_SECRET=<from step 1>
   ```

   Push notifications are optional and can wait: see [push.md](../ops/push.md). Every other setting, with its default, is
   in [compose-example.md](../ops/compose-example.md).

   The bundle already contains the compose file used below. If you would rather not use the bundle, save this as
   `compose.image.yaml` next to `calendard.env` (without the bundle, create `calendard.env` with just the four lines
   above instead of copying the example) and nothing else is needed:

   ```yaml
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

   Keep the `data` volume: it holds your database and backups.

## Start it

6. ```bash
   docker compose -f compose.image.yaml up -d
   docker compose -f compose.image.yaml ps          # calendard shows "healthy" after about 15 seconds
   docker compose -f compose.image.yaml exec calendard calendard doctor
   ```

   `doctor` should show no FAIL. Warnings about "no accounts" and "no backup yet" are normal on a new install.

7. Create the first token. The app uses it to pair. It is printed **once**; save it in your password manager:

   ```bash
   docker compose -f compose.image.yaml exec calendard calendard token create --name iphone --kind device --scope admin
   ```

**Check:** `curl http://127.0.0.1:8787/health` on the server answers `ok`. The hub listens on the server's loopback only,
so nothing is reachable from outside yet. That is the next step.

## Optional: watch the hub (observability)

Skip this on a first install; you can add it any time. The hub can send traces, metrics and logs to any
OpenTelemetry (OTLP) backend, such as Grafana Cloud, Honeycomb, SigNoz or your own collector. It is **off** unless you set
an endpoint: with nothing set, the hub starts no exporter and makes no network calls.

The container logs are always there, with or without this: JSON on stdout, with no tokens and no event titles.

```bash
docker compose -f compose.image.yaml logs -f calendard
```

To turn telemetry on:

1. Add to `calendard.env` (the `OTEL_*` examples for each backend are already in the file, commented out):

   ```sh
   OTEL_EXPORTER_OTLP_ENDPOINT=https://<your-otlp-endpoint>
   OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20<token>    # only if the backend wants a key; a space is written %20
   OTEL_RESOURCE_ATTRIBUTES=deployment.environment=prod
   ```

   The headers hold a secret, so keep the file at `chmod 600`. Use `OTEL_EXPORTER_OTLP_PROTOCOL=grpc` if your backend
   only speaks gRPC. Every other `OTEL_*` setting (sampling, export interval, switching one signal off) is in
   [compose-example.md](../ops/compose-example.md).

2. Restart and check:

   ```bash
   docker compose -f compose.image.yaml up -d
   docker compose -f compose.image.yaml logs calendard | grep telemetry     # "telemetry enabled endpoint=... signals=traces,metrics,logs"
   docker compose -f compose.image.yaml exec calendard calendard doctor     # the telemetry line says "endpoint reachable"
   ```

   Traces show up within seconds. Metrics arrive after one export interval (60 seconds by default).

If the backend is down or slow, the hub keeps working and drops the telemetry until it is back. To switch everything off
again, remove the lines or set `OTEL_SDK_DISABLED=true`. What is recorded, and how to try it locally with a throwaway
Grafana, is in [observability.md](../engineering/observability.md).

If something fails: [troubleshooting](../ops/troubleshooting.md).
