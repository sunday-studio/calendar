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

If something fails: [troubleshooting](../ops/troubleshooting.md).
