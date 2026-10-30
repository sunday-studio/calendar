# Changelog

All notable changes to Calendar (the hub and the iOS app ship under one version) are recorded here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/). See [RELEASING.md](../ops/RELEASING.md) for how a release is cut.

## [Unreleased]

## [0.1.2] - 2026-10-30

### Added
- `CALENDAR_OTEL_TOKEN`: send a bearer token to the telemetry backend without writing `OTEL_EXPORTER_OTLP_HEADERS`.
  An `Authorization` entry in the headers still wins.
- `CALENDAR_LOG_LEVEL` (`debug`, `info`, `warn`, `error` or `off`, default `info`) for the container log and the exported
  logs, and `CALENDAR_OTEL_LOG_LEVEL` to set the exported logs' level separately.

### Changed
- The app is named "Calendar: SS", because Apple refuses the name "Calendar" (ITMS-90129).
- The run-the-hub guide has the compose file inline, a "where the variables go" section, a verify checklist and an optional
  observability section.

## [0.1.1] - 2026-10-30

### Added
- Step-by-step setup guides (`docs/guides`), push and troubleshooting pages, and `scripts/set-bundle-id.sh` to swap the
  app's bundle id, App Group and push topic for your own.

### Changed
- Adding a Google account turns on only its main calendar; holiday, colleague and shared calendars start off and can be
  switched on in Settings. Calendars the hub already knows keep their setting.
- The app id is now `studio.thesunday.calendar` (App Group `group.studio.thesunday.calendar`) and the hub's default
  `CALENDAR_APNS_TOPIC` follows it. A hub that pushes to a build with another bundle id must set the variable.

### Fixed
- Connecting a Google account that did not grant calendar access (Google starts with the permission boxes unticked for an
  unverified app, or a Workspace admin limits them) is refused with a clear message instead of connecting and then
  failing every sync with a 403.
- Google's public holiday calendars, whose sync token Google rejects every time, are read in full every six hours
  instead of failing and resyncing on every poll.
- The sync indicator on the Home screen no longer blinks or moves the list.

## [0.1.0] - 2026-10-30

First release. The hub and the iOS app were run end to end on a real Google account, over Tailscale, on a physical
iPhone: create, edit, delete with Undo, RSVP, guests, Meet links, all-day and repeating events, single-occurrence
edits and read-only calendars, plus push (silent wake, moved, cancelled and invitation alerts).

### Added
- Hub (`calendard`, Go, SQLite): Google Calendar connection with OAuth/PKCE, encrypted token storage under a
  master key, incremental sync with recurrence expansion, and a read API (agenda, events, calendars, search).
- Write path to Google: create, edit, delete, move, RSVP and Meet links, with idempotency keys and 409/412
  conflict handling.
- ICS (read-only) calendar provider.
- Realtime status and event stream (SSE); bearer tokens with read/write/admin scopes and a token admin API.
- Free-time search, rule-based quick add, automations, and full-text search.
- Push: APNs sender, device registration, per-event reminder overrides and a morning summary.
- MCP endpoint on the hub and an adapter for the alfather parser (built against a fake; the real tool names
  are still to be confirmed).
- Operations: encrypted verified backups (`calendard backup` / `restore`), a pre-migration copy of the database,
  `doctor`, `rekey`, rate and size limits, Docker image (distroless, non-root, linux/amd64 and linux/arm64),
  Compose files for a host-Tailscale and a sidecar-Tailscale setup, a runbook and a smoke checklist.
- iOS app: pairing with a hub, accounts, Home (agenda and month), Day and 3-day views, event detail, create,
  edit, delete with Undo, RSVP, quick add, search, offline and status screens, settings, haptics and motion,
  widgets, notifications with a service extension, Live Activity, App Intents.
- Design system (CalendarUI): tokens, components, light and dark, large-type support.
- CI: server vet, race tests and govulncheck, fake-Google end-to-end, iOS build and tests, multi-arch image
  publishing to GHCR with SBOM and provenance.
- Env-only deployment: `CALENDAR_APNS_KEY`, `CALENDAR_GOOGLE_CLIENT_ID` and `CALENDAR_GOOGLE_CLIENT_SECRET` accept the
  APNs key and the Google client as values (the file variables still win), `deploy/compose.image.yaml` runs the published
  image from one env file, and `docs/ops/compose-example.md` lists every variable including telemetry.
- OpenTelemetry for the hub: traces, metrics and logs over OTLP, off unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set,
  configured only through standard `OTEL_*` variables (see [observability.md](../engineering/observability.md)).
  `calendard doctor` reports and probes it.
- Local run and demo mode: `scripts/run-local.sh` runs the real hub with persistent data, logs and an issue log;
  `scripts/run-local.sh demo` runs it against the fake Google with its own database (docs/ops/local-run.md).
- Rich demo data and scenarios: a `rich` profile for the fake Google (four accounts, every UI state, odd time zones,
  DST edges, long and unusual text) and `scripts/run-local.sh scenario` to trigger sync states such as re-auth,
  outages, rate limits, outside edits and bursts (docs/ops/demo-data.md).
- Widgets and notifications v2 design and spec (docs/specs/widgets-notifications.md); widgets phase 1 (small,
  medium, large agenda, states) is built.
- Documentation: README rewrite with screenshots, architecture and write-flow diagrams, an assets folder
  (`docs/assets`) and `scripts/screenshots.sh`, which regenerates the screenshots from the real app.
- Push alerts name the event: cancelled and invited notifications are rewritten on the phone from a new
  `GET /v1/events/{id}/brief`, so titles never travel through Apple. A new invitation raises an alert (Settings, Alerts,
  New invitations).

### Changed
- The image listens on `0.0.0.0:8787` by default (it had a Tailscale address baked in); compose files publish it on loopback only.
- Debug builds of the iOS app allow plain http to Tailscale addresses; Release builds are unchanged.
- Pairing: an address typed without a scheme gets `http://` for private and Tailscale addresses (and `https://`
  otherwise), and a failed pairing shows the attempted URL and the system error code.
- Repo cleanup: all documents moved into `docs/`, `GNUmakefile` folded into `Makefile`, unused server code, iOS
  code, tokens and the Newsreader font removed.

### Fixed
- Events created in the app are accepted by Google (client-chosen ids are lowercase base32hex).
- The sync cadence is re-read on every wake, so polling no longer stalls on the slow interval.
- The event detail page asks the hub to poll before refreshing.
- A guest typed in the people screen and not submitted is kept when you leave it.
### Known limitations
- Google answers `fullSyncRequired` to the stored sync token on every poll for some calendars, so those calendars
  resync fully each time. Correct, but wasteful; under investigation.
- Offline save, conflict ("Changed elsewhere"), drag and resize, revoking access and moving between calendars have not
  been walked on a real account yet (docs/ops/SMOKE.md).
- Widgets update after every sync and at event boundaries; with the app closed they depend on iOS letting a silent push or
  background refresh through.
- The Week view was removed at the author's request; the views are Agenda, Day and 3 days.
- Quick add runs on rules only until the alfather contract is confirmed (H9).

[Unreleased]: https://github.com/sunday-studio/calendar/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/sunday-studio/calendar/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/sunday-studio/calendar/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/sunday-studio/calendar/releases/tag/v0.1.0
