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
| `ios/` | Xcode project (generated from `ios/project.yml`), app, widgets, notification extension, Swift packages |
| `deploy/` | Compose files, env example, first-run and verify scripts |
| `scripts/` | End-to-end, release and iOS archive scripts |
| `docs/` | All documentation, indexed in [docs/README.md](docs/README.md) |
| `VERSION`, `Makefile` | Product version (read by the release flow), common tasks |

## Quickstart

Hub in fixture mode (no accounts, no secrets; the iOS app can pair with it):

    cd server && go run ./cmd/calendard serve --fixtures

Real mode: [docs/ops/first-run.md](docs/ops/first-run.md), then `make smoke`.

iOS app (Xcode 26.6, XcodeGen, an iPhone 17 Pro simulator):

    make ios-gen && make ios-build && make ios-test

Pair the app with the hub from the first screen (hub address and a `cal_...` token). Shipping through
TestFlight: [docs/ops/testflight.md](docs/ops/testflight.md). Cutting a release: [docs/ops/RELEASING.md](docs/ops/RELEASING.md).

## Docs

Start at [docs/README.md](docs/README.md).
