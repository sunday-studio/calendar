# Design audit 4: Settings, Server and sync, offline/stale/syncing/down states

_Design lane · conformance pass 3 · boards g-home…g-week, y-*, h-sync, h-banner, h-load, h-noacc, h-empty · iPhone 17 (own simulator), dark; backend `--fixtures-state` flags used._
Shots: `docs/audit-shots/r2/{g0,g1,t-down,t-syncing,t-banner}-dark.png`.

## Blocking finding (S0)
**Settings rows built with `NavigationLink(value:)` do nothing when tapped** (Calendars, Default calendar, Time zones, Working hours, Week starts, Alerts, Appearance, Server and sync). The one `Button` row (Accounts) works, and the Today long-press sheet → Settings works. Tried: taps at row centre, text and chevron, 2.5s waits, after removing the accessibility modifiers on `NavRow` (so it is not the component). Likely cause: `.navigationDestination(for: SettingsRoute.self)` is registered inside the pushed `SettingsPage` (SettingsScreens.swift:110) instead of at the app's `NavigationStack` root (HomeRoot.swift:45), or `SettingsRoute` links are pushed on a path typed for `HomeRoute`. Fix: register the SettingsRoute destination next to `HomeRoute`'s in `HomeRoot` (or give Settings its own `NavigationStack`). **Because of this I could not review the nine g-* subscreens, Server and sync, the sync log or the diagnostic checklist.** Please fix, tell me, and I will run them (light + dark).

## Settings home (g-home): matches
Nav "Settings", mono-caps groups (Calendars / Time / Alerts and look / Server), 60pt rows with hairlines, values dim with chevrons, `Calendars` sub-line "Show, hide, colour", Server row with host sub-line and a green `OK` status square. Deltas: (S3) "Alerts: None" while the canvas default is "10 min" (value mapping when the on-device pref is unset: show the effective default, not "None"); (S3) Today sheet lacks the "Server and sync" row of the spec (fine: it is in Settings).

## Home states
| State | Result vs board | Deltas |
|---|---|---|
| **banner** (h-banner) | ✓ Banner copy "Work needs you to sign in again. Its events are paused." + "Fix"; Next block directly under it; Work rows "Stale". | (S3) banner sits flush under the view switch: add 16pt top margin; footnote ("Events from the work account stay visible…") not visible; (S3) subject "Work" is Medium ✓ |
| **down** (y-down) | ✓ Red banner "Can't reach your server. This agenda is from 15:09 and may be out of date." + "Check"; quiet saved-copy card with flip cells and "in 2 h 20 min · saved copy"; every row "Stale"; FAB dimmed. | (S3) 16–20pt gap between banner and the quiet card (canvas y-down has 20); first sentence should be Medium; footnote "Alerts already scheduled on this phone will still arrive." not visible |
| **syncing** (h-sync) | ✓ amber `StatusLabel` "Syncing 3 accounts", mono "2 of 3", 2pt progress at 66%. | (S3) footnote "Your server checks each account about once a minute…" not visible |
| **normal after reinstall** | ✓ new fixture data (overlaps, long titles) renders; 3-lane Day view uses 98pt lanes. | (S3) Interview title truncates in the Stale list because the status word competes; acceptable |
| **empty / noaccounts / loading / offline** | not re-checked this pass | re-run after the cache-clearing fix (audit-2 addendum 2) |

## Also checked with the richer fixtures
Day view with three overlapping events (Design review / Interview / Roadmap sync): three lanes of ~96pt side by side, titles tail-truncated to one line, matches `day-view.md`. Day second-zone labels now "AMS / EDT" ✓ (audit-3 #3 fixed). Agenda shows two overlap lines per row as specified.

## Next
Waiting for the NavigationLink fix to review the g-* subscreens and the y-* screens; light mode for the state screens in the following pass.

## Addendum: g-* subscreens and Server screens (after the Swift navigation fix)
Navigation now works. Reviewed in dark on iPhone 17 against the fixture hub: Calendars, Default calendar, Time zones, Working hours, Alerts, Appearance, Server and sync, diagnostic. **Not captured:** Calendar colour (g-colour), Week starts (g-week), Sync log (needs an account row with data), light mode of all of these. Shots `docs/audit-shots/r2/s-*-dark.png`.

### S1
| # | Screen | Delta | Fix |
|---|---|---|---|
| 1 | Calendars (g-cals) | The 14×14 **colour square is drawn on top of the calendar name** (it overlaps the first letters: "Alex", "Team"). Canvas: square, then name, gap 16. | Put the square in the row's leading slot (`HStack` with the title, not an overlay) (Swift) |
| 2 | Working hours (g-work) | The **"MON" chip wraps to "MO / N"** and is taller than the others (7 chips do not fit at 12pt padding). | `ChipToggle` now never wraps (`lineLimit(1)`, `fixedSize`, padding 10) — shipped in CalendarUI; use `HStack(spacing: 6)` for the 7 chips (≈337pt fits 342pt) (Swift) |
| 3 | Alerts (g-alerts) | **No radio is selected** although Settings shows "10 min". | selected = effective lead time (default 10) (Swift) |
| 4 | Server and sync (y-status) | Shows **"Unreachable"** and **"No accounts yet."** while the hub answers (Settings row says OK, Home loads). The diagnostic then shows a red banner "127.0.0.1 didn't answer." above three green rows and "Everything answers": contradictory. Probable cause: the status/sync-runs request fails against the fixture hub, and the screen treats that as the server being down. | Distinguish "status unavailable" from "server unreachable": reachability = `hello`; if `/v1/status` is missing show address + version only and hide the accounts group; the red banner appears only when a check fails (Swift/backend: confirm the fixtures serve /v1/status and /v1/sync/runs) |

### S2
| # | Screen | Delta | Fix |
|---|---|---|---|
| 5 | Calendars | "Default" sub-label under the first calendar of **every** account; canvas marks only the single default calendar. | derive from Settings › Default calendar (Swift) |
| 6 | Default calendar (g-default) | No account group labels and no colour squares. | label per account (mono caps email) + 14pt square before each name (Swift) |
| 7 | Working hours | From / To are inline `TimeStepper`s with xl cells (≈ 120pt tall each); canvas `.tg` rows with a small (18pt) flip-cell value on the right. | `SettingsRow("From") { FlipCells("09:00", size: .small) }`, tap opens the stepper in a `CalSheet` (Swift); lunch-break row still skipped |

### S3
Time zones preview row is plain "10:30 Roadmap review" (canvas: mono time column, title, "06:30 in New York" second line); "Reduce motion" shows text "Off" (fine, reflects the OS flag); From/To labels are mono caps (canvas 18pt titles); Server group "Version" row absent (follows #4).

### Matches
Settings home; Time zones (Main/Second column/Preview/hint, offset "6 hours behind" correct for Amsterdam/New York in October); Appearance (theme segment, Flip cells toggles, Agenda toggles); Alerts copy and toggles; Default calendar copy and radios; Server address key/value and "Sync now" footer; diagnostic row layout and statuses (`Yes` / `Not needed` squares).
