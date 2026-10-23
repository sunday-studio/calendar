# Run it locally and log your issues

Use this to live with the real hub on your own machine before deploying. Data, the master key, logs and your issue log live in `.local/` (git-ignored). `make run` is for quick developer runs; use `scripts/run-local.sh` (or `make run-local`) for this.

## Start
```sh
scripts/run-local.sh              # builds and runs on 127.0.0.1:8787 (PORT=8790 to change)
LAN=1 scripts/run-local.sh        # also reachable from a phone on the same Wi-Fi: http://<mac-ip>:8787
scripts/run-local.sh token iphone # in a second terminal: prints an admin token once
scripts/run-local.sh doctor       # health check
scripts/run-local.sh logs         # follow today's log (.local/logs/calendard-YYYY-MM-DD.log)
```
It refuses to start if the port is busy and tells you who holds it. A master key is generated on first run (`.local/master_key`); keep it if you want to keep the data.

## Demo data (no Google needed)
```sh
CALENDAR_ADDR=100.127.10.66:8790 scripts/run-local.sh demo
```
Runs the real hub against the fake Google used by the end-to-end tests: two accounts (Work, Personal), five calendars and a spread of events (all-day, overlaps, repeating, invitations, Meet links), all arriving through the real sync path. It uses its own database in `.local/demo/` (never your real `.local/calendar.db`) and prints an admin token, also saved in `.local/demo/token`. Data refreshes every 15 s. The fake Google is started for you on port 8788 (`FAKE_PORT` to change). Stop the hub with Ctrl-C; the fake stops with it. Use plain `scripts/run-local.sh` for your real data.

## Phone
Pair the app with `http://<mac-ip>:8787` (Debug build, same Wi-Fi, `LAN=1`) and the token. Release builds only talk HTTPS, so use the Tailscale address instead (see below).

## Real Google sign-in locally
1. Put the OAuth client JSON in `.local/google_client.json` (`{"client_id": "...", "client_secret": "..."}`), made as in `docs/ops/first-run.md` section 1.
2. Redirect URI in Google: `https://<this-mac>.<tailnet>.ts.net/auth/google/callback`.
3. `tailscale serve --bg --https=443 http://127.0.0.1:8787` (check `tailscale serve --help` for your version).
4. Start with `scripts/run-local.sh`; `CALENDAR_PUBLIC_URL` is taken from Tailscale automatically (or export it). The phone must be on the tailnet. Pair the app with the `https://…ts.net` address.

## Log every issue
```sh
scripts/run-local.sh issue "Day view jumps when I add an event"
```
appends an entry to `.local/ISSUES.md` with the time, blanks to fill (expected, steps, device, trace id) and the latest warnings and errors from the hub log. Fill in the blanks when you can, then paste the file to the coordinator. The app's Settings, Server and sync, Sync log screens show the same sync runs.

## See telemetry while you test
Any `OTEL_*` variable you export passes straight through. For a local viewer start `deploy/compose.otel-dev.yaml` and set `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318` (details in `docs/engineering/observability.md`). Every error is recorded with a trace id that also appears in the log line.

## Reset
Stop the hub, then `rm -rf .local` for a clean start (this deletes the database and the master key).
