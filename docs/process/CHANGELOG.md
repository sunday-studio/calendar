# Changelog

All notable changes to Calendar (the hub and the iOS app ship under one version) are recorded here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/). See [RELEASING.md](../ops/RELEASING.md) for how a release is cut.

## [Unreleased]

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
