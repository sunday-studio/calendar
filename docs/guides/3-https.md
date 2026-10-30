# 3. Put HTTPS in front of it

Google only redirects to an `https` address, and your phone should reach the hub without exposing it to the internet.
The simplest way is Tailscale: the hub is reachable only from your own devices, with a real certificate.

## With Tailscale (recommended)

1. Install Tailscale on the server and on your iPhone, and sign both in to the same tailnet.
2. In the Tailscale admin console, **DNS**: turn on **MagicDNS** and **HTTPS Certificates**.
3. On the server, publish the hub on your tailnet:

   ```bash
   tailscale serve --bg --https=443 http://127.0.0.1:8787
   tailscale serve status
   ```

   Do not use `tailscale funnel`: that puts it on the public internet. The serve setting survives reboots.
4. Find your address (the server's `DNSName`, without the trailing dot):

   ```bash
   tailscale status --json | jq -r .Self.DNSName
   ```

5. Use it in two places: `CALENDAR_PUBLIC_URL=https://<that address>` in `calendard.env` (then
   `docker compose -f compose.image.yaml up -d --force-recreate`), and as the redirect URI
   `https://<that address>/auth/google/callback` in the Google client from [step 1](1-google-cloud.md).

## Without Tailscale

Any reverse proxy that terminates TLS and forwards to `127.0.0.1:8787` works (Caddy, nginx, Traefik). Keep the hub
itself bound to loopback. Your phone must be able to reach the address; a VPN is a good idea, since the hub holds the
keys to your calendars.

**Check:** open `https://<your-address>/health` in Safari on your iPhone (with Tailscale connected). It answers `ok` with
no certificate warning.
