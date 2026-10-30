# Troubleshooting

Start with `docker compose -f compose.image.yaml exec calendard calendard doctor`. It checks the settings, the database,
Google, push, backups and telemetry, and tells you what is wrong. Logs are JSON with no tokens and no event titles:
`docker compose -f compose.image.yaml logs -f calendard`.

| Symptom | Cause | Fix |
|---|---|---|
| Google: `Error 400: redirect_uri_mismatch` | The redirect URI in the Google client differs from `CALENDAR_PUBLIC_URL` plus `/auth/google/callback` (http vs https, a trailing slash, a different host, or the env edited but the hub not restarted) | Make both identical; the error page shows the URI the hub sent. After editing `calendard.env` run `docker compose -f compose.image.yaml up -d --force-recreate`. Google can take a few minutes to apply a change |
| Google: `access_blocked`, "can only be used by test users", or "has not completed the verification process" | The consent screen is still in *Testing*, or a Workspace admin blocks the app | Publish to **In production** ([step 1](../guides/1-google-cloud.md)); for Workspace ask the admin to allow your client id |
| The app says "Calendar access wasn't allowed" (`scope_missing`), or an account shows an error and the log says `HTTP 403 ... insufficient authentication scopes` | On Google's permission screen the box for your calendars was left unticked (Google starts with the boxes unticked for an app that is not Google-verified), or, much less often, a Workspace admin limits what apps may ask for. Hubs before 0.1.1 connected such an account anyway and then failed every sync | Remove the account (Settings, Accounts) and connect it again and tick "Select all" on the permission screen. If the calendar box is missing or greyed out, the admin has to allow your OAuth client id |
| "Google hasn't verified this app" | Expected for an unverified app | **Advanced**, **Go to Calendar (unsafe)**. It is not an error |
| `invalid_grant` in the log, or an account says "Sign in needed" | The refresh token was revoked, the Google password changed with sign-out, or the app is still in Testing (tokens expire after 7 days) | **Settings, Accounts, Reconnect**; make sure the app is In production |
| The phone says Unreachable, or `curl` times out | The phone is not on the tailnet, `tailscale serve` is missing, the hub is down, or MagicDNS/HTTPS is off | Tailscale connected on the phone; `tailscale serve status`; `docker compose ... ps`; `curl https://<address>/health` from the server, then from the phone |
| Certificate error | HTTPS Certificates are not enabled for the tailnet, or the wrong host name | Enable them in the Tailscale admin console; use the exact `DNSName` |
| `401` or "token invalid" in the app | The token was mistyped, revoked, or has too small a scope | `docker compose ... exec calendard calendard token list`; create a new one with `--scope admin` and pair again |
| Container exits at start: `master key missing` | `CALENDAR_MASTER_KEY` is empty, or not 32 bytes in base64 or hex | Generate one with `head -c32 /dev/urandom | base64` and set it; never change the key of an existing database |
| `Bind for 127.0.0.1:8787 failed` | Another process holds the port | `lsof -nP -iTCP:8787 -sTCP:LISTEN`, or set `CALENDAR_HOST_PORT=8788` and use the same port in `tailscale serve` |
| The hub is gone after a server reboot | Docker did not start at boot | Enable Docker's start at boot; the compose file already restarts the hub (`restart: unless-stopped`) |
| Calendar changes arrive slowly | The app is closed and iOS limits background work | Open the app, or enable push ([push.md](push.md)); the hub itself polls every 1 to 5 minutes |

More: [RUNBOOK.md](RUNBOOK.md) (backups, restore, rotating keys and tokens).
