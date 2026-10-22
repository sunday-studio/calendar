# Calendar

A personal calendar that replaces Notion Calendar. A small hub runs on your home server, connects to your
calendar accounts (Google today, ICS links too), and keeps one synced copy. A native iOS app (SwiftUI, iOS 26)
reads and edits it, with widgets, reminders, a Live Activity, quick add by typing or voice, and search.
Apps and scripts reach the same hub through a token API and an MCP server.

## Layout

| Path | What |
|---|---|
| `server/` | Go hub `calendard`: sync, API, push, backups. Module `calendar/server` |
| `api/openapi.yaml` | The wire contract between hub and app |
| `ios/` | Xcode project (generated from `ios/project.yml`), app, widgets, notification extension |
| `ios/Packages/` | `CalendarCore` (models, sync, DB), `CalendarUI` (design system), `Feature*`, `Surfaces` |
| `design/` | Design canvas sources and exports |
| `deploy/` | Compose file, env example, runbook, smoke checklist |
| `docs/` | Specs, design audits, screenshots, CI, scripting, alfather |

## Run the hub

Fixture mode: no accounts, no secrets; the iOS app can pair with it and show sample data.

    cd server && go run ./cmd/calendard serve --fixtures
    # other states: --fixtures-state=normal|loading|empty|noaccounts|banner|offline|down|syncing

Real mode: needs a master key and Google OAuth client; see `deploy/README.md` (env vars `CALENDAR_*`).

    make build          # server binary
    docker compose -f deploy/compose.yaml up -d --build
    make smoke          # checklist in deploy/SMOKE.md (needs a real Google account)

## Build and test the iOS app

Needs Xcode 26.6, XcodeGen (`brew install xcodegen`) and an iPhone 17 Pro simulator.

    make ios-gen        # regenerate Calendar.xcodeproj from project.yml
    make ios-build
    make ios-test       # package tests, unit tests, UI tests incl. accessibility audits
    make ios-e2e        # CalendarCore sync against the fixture hub

Pair the app with the hub from the first screen (hub address and a `cal_...` token). Install on a
device and ship through TestFlight: see `docs/testflight.md`.

## Docs

| Doc | Read it for |
|---|---|
| `PLAN.md`, `RESEARCH.md`, `MILESTONES.md` | What and why, milestones M0 to M9 |
| `ENGINEERING.md` | Conventions for server and iOS |
| `STATUS.md` | Task board per lane |
| `DECISIONS.md` | Decisions made, things only you can do (section B), open questions (C), log (D) |
| `docs/specs/` | Screen specs (day, detail, settings, create/edit, drag, widgets, quick add, search) |
| `docs/design-audit-*.md` | Design conformance audits |
| `docs/accessibility.md` | VoiceOver, Dynamic Type, Reduce Motion, audit results and known gaps |
| `docs/testflight.md` | Signing, archive and upload steps |
| `docs/ci.md` | GitHub Actions |
| `docs/scripts.md`, `docs/alfather.md` | Tokens, automations, MCP, the alfather adapter |
| `deploy/RUNBOOK.md`, `deploy/SMOKE.md` | Operating the hub |
