# Changelog

All notable changes to Calendar (the hub and the iOS app ship under one version) are recorded here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/). See [RELEASING.md](../ops/RELEASING.md) for how a release is cut.

## [Unreleased]

Everything here is built and tested against fakes, like 0.1.0; nothing is verified on a physical device or against real Google.

### Added
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

### Changed
- The image listens on `0.0.0.0:8787` by default (it had a Tailscale address baked in); compose files publish it on loopback only.
- Debug builds of the iOS app allow plain http to Tailscale addresses; Release builds are unchanged.
- Pairing: an address typed without a scheme gets `http://` for private and Tailscale addresses (and `https://`
  otherwise), and a failed pairing shows the attempted URL and the system error code.
- Repo cleanup: all documents moved into `docs/`, `GNUmakefile` folded into `Makefile`, unused server code, iOS
  code, tokens and the Newsreader font removed.

### Fixed
- Quick add, free time and search follow the 24-hour clock preference.

## [0.1.0] - 2026-10-23

First tagged version. Everything below was built and tested against fakes (a fake Google, fixtures, the iOS
simulator); see Known limitations.

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

### Known limitations
- Nothing has been run against real Google yet (needs your Google Cloud project, DECISIONS.md H1 and H6).
- iOS push, widgets and the App Group have not been verified on a physical device; push needs an APNs key.
- The Week view was removed at the author's request; the views are Agenda, Day and 3 days.
- The Docker image and the GitHub workflows have not been run on GitHub; the image has not been built (no Docker daemon was
  available while writing it).
- Custom repeat form and the Home draft row are open items (DECISIONS.md D S7).
- Quick add runs on rules only until the alfather contract is confirmed (H9).

[Unreleased]: https://github.com/sunday-studio/calendar/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/sunday-studio/calendar/releases/tag/v0.1.0
