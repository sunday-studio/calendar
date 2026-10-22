# Build Status — three lanes

_Coordinator (session "cordinator") owns this file. Lane agents tick their own rows and append to DECISIONS.md §D. One task at a time per lane; each task ends with a short report to the coordinator._

**Lanes and ownership (no two agents edit the same files):**

| Lane | Agent | Owns | Never touches |
|---|---|---|---|
| Backend | `backend agent: calendar` | `server/`, `api/openapi.yaml`, `deploy/`, server targets in root `Makefile` | `ios/`, `docs/design/` |
| Swift | `swift agent: calendar` | `ios/` except `ios/Packages/CalendarUI` and `ios/Calendar/Resources` | `server/`, `docs/design/` |
| Design | `design agent: calendar` | `docs/design/`, `ios/Packages/CalendarUI`, `ios/Calendar/Resources`, `docs/screens/`, conformance reviews | `server/`; feature view logic |

Contract changes: backend edits `api/openapi.yaml` and messages swift. New component/token needed: swift asks design, design adds it to CalendarUI and replies.

## Done
T01 server skeleton · T02 contract + fixture hub + iOS project · T03 design system v0 · T04 Google OAuth/accounts · T05 iOS pairing/accounts · T06 Google provider + sync · T07 recurrence · T08 SSE/status · T09 read API · T10 iOS core/sync engine · T11 iOS Home + month

## Backend lane
| ID | Task | M | Status |
|---|---|---|---|
| B1 | Write service + Google writes (create/patch/delete/move/RSVP/Meet, scopes, idempotency, 409/412) | M4 | done |
| B2 | Push: APNs sender (fake-testable), device endpoint, reminder overrides, summary push | M5 | done |
| B3 | Tokens admin API, freetime, rule-based quick-add, automations, FTS finish | M6 | done |
| B4 | Hardening: backup/restore, deploy files, runbook, smoke checklist, doc names sync | M7 | done |
| B5 | alfather adapter + MCP server | M8 | done (against a fake; real tool names = H9) |
| B6 | ICS provider | M9 | done |
| B7 | Hardening round 2: MCP/auth rate + size limits, lunch break in free time, all-day reminder time, Google edge cases (goldens), 20k performance budgets + upsert benchmark, `make build-linux` | M7 | done |
| E2E | Fake Google binary, `dev seed-account`, `make e2e-server` / `e2e-up`, fixture `POST /v1/accounts/ics` (docs/engineering/e2e.md) | M7 | done |

## Design lane
| ID | Task | M | Status |
|---|---|---|---|
| D1 | **Conformance audit:** built Home/Accounts/Pairing (simulator + fixture hub) vs hi-fi frames B/A/P → design-audit-1 (removed, recoverable from commit 9c13ffa), ranked deltas | M3 | done (rev 2, vs Gnomon canvas) |
| D2 | Fix CalendarUI components/tokens from audit; add missing states | M3 | done (palette, type, flip cells, Next block, rows, chrome; Feature adoption pending) |
| D3 | Implementation specs for Day, Event detail, Settings, Offline/status (frames C, D, G, K, Y) | M3 | done (specs in docs/specs; components added) |
| D4 | Specs + components: create/edit, RSVP, delete/undo (E, R, D5–D9) | M4 | done (create-edit.md, rsvp.md) |
| D5 | Specs + components: week/drag, widgets, notifications, Live Activity (V, W) | M5 | done (drag-resize.md, widgets-notifications.md) |
| D6 | Specs: quick add, search, tokens (Q, F, Y7–8); app icon + launch screen | M6 | done (quick-add.md, search.md, app-icon-launch.md + assets) |
| D7 | Full visual QA of every screen, light/dark/large type | M7 | done (audit-7 + addendum; remaining QA list in audit-7) |

## Swift lane
| ID | Task | M | Status |
|---|---|---|---|
| S1 | Apply D1/D2 fixes to Home/Accounts/Pairing, then Day view + TimeKit + Event detail | M3 | done |
| S2 | Settings, calendar toggles/colours, offline + status screens | M3 | done |
| S3 | Create/edit/delete/RSVP/move screens | M4 | done |
| S4 | 3-day/Week + drag/resize | M5 | done (Week read-only by design) |
| S5 | Widgets, notifications + NSE, Live Activity | M5 | done (real push/App Group need H4/H5) |
| S6 | Quick add, search, tokens, App Intents | M6 | done (see DECISIONS D S6) |
| S7 | Accessibility, TestFlight config, final fixes | M7 | done (open: custom repeat form, Home Draft row; see DECISIONS D S7) |
| S8 | Haptics service + settings toggle + event→feedback map (tests), wired into snap/lift/paging/FAB/sheets/save/RSVP/undo/conflict/failure | M7 | done |
| S9 | Motion: zoom transition row/hero/block → Event detail, FAB → New event zoom, view-mode cross-fade, animated list changes, staggered first load, Reduce Motion = cross-fade only | M7 | done (frame times not measured; flip-cell animation pref has nothing to gate yet) |
| S10 | Week view REMOVED at user request (views: Agenda, Day, 3 days); view select stays in the header. More haptics (row/invite/hero tap, Today, month toggle, drop); motion simplified (no stagger or list slide, press dim, spring on drop) | M7 | done |
| iOS E2E | XCUITest against the real hub + fake Google (`make ios-e2e-real`, docs/engineering/e2e.md): pair, accounts, Home, month/Day/3-day/Week, detail, create, conflict, RSVP, delete+Undo, hide calendar, quick add, search, pull to refresh, Google refusal, hub offline | M7 | done (passes; 37 screenshots in docs/screens/e2e) |

## CI lane
| ID | Task | M | Status |
|---|---|---|---|
| CI | GitHub Actions ci.yml + release.yml (GHCR multi-arch), dependabot, docs/ops/ci.md | M7 | done (unrun on GitHub; image unbuilt, no Docker here) |

## Rules
1. No screen is built without the design agent's spec; no screen is "done" without a simulator screenshot checked against its hi-fi frame.
2. Lane agents work one task at a time, run their own build/tests, and report to the coordinator in ≤6 lines.
3. Decisions go in DECISIONS.md §D; anything needing the user goes in §B/§C.

_Swift-lane session ended after S4 (usage limit). Coordinator continues S5–S7 with short-lived workers; backend lane B1–B6 complete (backend session ended)._

## Communication rule (user, 2026-10-11)
No progress chatter between agents or to the coordinator. Results go in files (STATUS.md one-liners, DECISIONS.md §D, docs/*). Message the coordinator only when blocked on the user or the build is broken, max 2 lines. Workers' final reply: max 2 lines.
