# End-to-end harness (fake Google)

The real hub (real SQLite, sync manager, write service, SSE) runs against `server/cmd/fakegoogle`, a stand-alone fake of Google OAuth and Calendar v3. No credentials, no browser. It does not replace `docs/ops/SMOKE.md` (real Google behaviour).

## Commands
| | |
|---|---|
| `make e2e-server` | builds both binaries, starts them on free ports with a temp DB and key, runs ~75 assertions, tears down, prints `E2E PASS` or `E2E FAIL` (logs are kept in the temp dir on failure; `KEEP=1` keeps them always) |
| `make e2e-up` | leaves the stack running: hub `127.0.0.1:8787`, fake Google `127.0.0.1:8788`, accounts `a@example.com` and `b@example.com` already seeded and syncing. Prints a dev (admin) token; `.e2e/env` holds `CALENDAR_E2E_URL`, `CALENDAR_E2E_TOKEN`, `FAKE_GOOGLE`. Override ports with `E2E_HUB_PORT` / `E2E_FAKE_PORT` (refuses if a port is busy). |
| `make e2e-down` | stops it and removes `.e2e/` |

Needs go, curl, jq, python3. For the iOS fixtures-mode flows there is the separate fixture hub (`calendard serve --fixtures`, `make ios-e2e`).

## How the hub is pointed at the fake
Env overrides (all optional, empty = real Google): `CALENDAR_GOOGLE_AUTH_URL`, `CALENDAR_GOOGLE_TOKEN_URL`, `CALENDAR_GOOGLE_API_BASE` (replaces `https://www.googleapis.com/calendar/v3`), `CALENDAR_GOOGLE_USERINFO_URL`. With `CALENDAR_DEV=1`, a token URL override and no client file, the hub uses a placeholder OAuth client.

`CALENDAR_DEV=1 calendard dev seed-account --provider google --email a@example.com [--name N]` creates the account with a sealed fake refresh token (`fake-refresh-<email>`, subject `fake-sub-<email>`), so sync starts with no consent. It refuses unless `CALENDAR_DEV=1`. Running it again for the same email resets the account to `connecting` (a quick way out of `needs_reauth`). The real connect/reconnect flow also works: the fake's `/auth` approves at once and redirects back.

## The fake
One port, several accounts; the account is picked by the access token. Each account has its own calendars and data, seeded relative to now (Europe/London):
- `a@example.com`: Alex (Work, owner), Team (reader), UK Holidays (reader). Daily weekday Standup with one moved and one cancelled occurrence, Design review with a Meet link and guests, a 2-day all-day Team offsite, overlapping Planning and 1:1, an invitation needing a reply (Quarterly sync), Lunch, Team retro, a bank holiday.
- `b@example.com`: Personal (owner), Family (writer). Climbing (Tue/Thu series), Dentist, a clash with Planning across accounts, a birthday, Family dinner.

It supports list with `syncToken` (410 when expired), insert, get, patch, delete, move, `instances`, etags and `If-Match` (412), and Meet `conferenceData`. The code is `server/internal/provider/google/gtest` (shared with the unit tests: `Fake` is the per-account API, `Multi` the multi-account server, `Seed` the data).

Admin (JSON POST, no auth): `/_admin/fail {account?,method?,path,code,n,retryAfter?}`, `/_admin/reset`, `/_admin/seed {account,cal,id,event | delete:true}` (an outside change), `/_admin/etag-bump {account,cal,id,summary?}` (outside edit, makes the next hub write a 409), `/_admin/revoke {account}` and `/_admin/unrevoke`, `/_admin/expire-sync-tokens {account?}` (next incremental list is 410). `GET /_admin/state?account=` dumps what each account's Google holds plus the writes it received. Calendar ids are the account email, `team-<email>@group.calendar.google.com`, `family-<email>@group.calendar.google.com`.

## What `make e2e-server` asserts
First sync of both accounts; agenda has the seeded events (expanded series, exception moved, cancelled occurrence absent, all-day, overlap, invitation `needsAction`, Meet join, read-only calendar not editable); calendar visibility toggle; create (appears at the fake and in the agenda), idempotent replay and key reuse (422), provider failure on create (502, nothing saved); PATCH with `If-Match`, stale version (412 with `current`), provider-side change (409 with `current`); RSVP; add Meet; delete one occurrence and a whole series; stale delete (412); revoked token to `needs_reauth` in `/v1/accounts`, `/v1/status` and SSE, then reconnect through the fake's OAuth; 410 syncToken recovery and outside delete; SSE `rev` after an outside change.

## iOS end-to-end (real hub + fake Google)
`make ios-e2e-real` (= `scripts/ios-e2e.sh`) runs the XCUITest `CalendarUITests/E2ETests` against the same stack, on a dedicated simulator **Calendar E2E** (created on first use). It starts the hub and fake Google on free ports in a temp dir, builds (`build-for-testing`, derived data in `.e2e-derived/`), uninstalls the app, runs the test, tears down only what it started, and prints `IOS E2E PASS` or `IOS E2E FAIL` (logs are kept on failure; `KEEP=1` always). Needs the e2e requirements plus xcodegen and Xcode. A full run takes about 5 minutes.

The flow (one test, steps build on each other): pair through the UI (type host and token) > accounts list (2 seeded accounts) > Home (seeded events, next-event block or "Done for today") > month, pick a day, Day / 3 days / Week > details (Meet link, repeating series with a moved occurrence, read-only team calendar) > create through the editor (checked in the agenda and at fake Google through `/_admin/state`) > edit after `/_admin/etag-bump` ("changed elsewhere" conflict, re-apply) > RSVP to the invitation (fake Google shows `accepted`) > delete, Undo, delete for real > hide a calendar in Settings and bring it back > quick add "lunch with Sam friday 1pm" (answers the hub's "which Friday?") > search > pull-to-refresh after an outside `/_admin/seed` > Google refuses one write (form kept, Try again works) > hub stopped (offline banner, saved rows stay, + is locked with a toast) and restarted (recovers). Screenshots of every step go to `docs/screens/e2e/`. Waits are explicit (`waitForExistence`, polling with a deadline); the only sleeps are two short settle waits around system sheets.

Knobs: `IOS_E2E_STEPS="offline,search"` runs only the steps whose name contains one of the words (pairing always runs; screenshots are then not cleared); `IOS_E2E_SIM` picks another simulator name; `IOS_E2E_ONLY` another xctest filter. How it works: the script passes `TEST_RUNNER_E2E_HUB/TOKEN/FAKE/SHOTS/CTL` (the test sees them without the prefix); the test talks to the hub and fake Google over HTTP for assertions; for the offline step it touches `$E2E_CTL/hub-down` or `hub-up` and the script (which owns the process) stops or restarts the hub and writes `<name>.done`. The app has DEBUG-only launch args `-e2eReset` and `-e2eHub <url> -e2eToken <token>` for tests that want a known pairing without the UI. The data is seeded relative to now in Europe/London, so what is "next" or "earlier today" depends on the time of day; the test accepts either.

Known gaps: Google being down is not visible in the app (the hub stays reachable and reports the account `active`); the system sign-in sheet for adding an account is not driven.

## Not covered
Real Google quirks (quota, Meet latency, Workspace policies), push (APNs), ICS feeds, backups (`make backup-drill`), real Google sign-in from the app.
