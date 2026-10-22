# Design audit 7: D7 visual QA (light / dark / large type / widgets)

_Design lane · iPhone 17, fixtures normal. Shots `docs/audit-shots/r3/*`, widgets `docs/screens/widgets-{dark,light}.png`. Earlier audits (2–6) hold for per-screen deltas; this one adds what D7 covers: light mode, Accessibility XL type, widgets, contrast._

## Coverage
Checked: Home (light, light + AX-XL), Settings home (light), widgets gallery (light, dark). Dark mode of every screen was checked in audits 2–6; light mode of Pairing/Accounts in audit-2 addendum. **Not re-run this pass** (use `--fixtures-state` and `--fixtures-write` flags): light mode of Day/3 days/Week, Detail, editor sheets, Move, People, g-* subscreens, Server/diagnostic, empty/noaccounts/loading/offline states; AX-XL beyond Home. Widgets in the real WidgetKit surface (only the in-app gallery was reviewed).

## Fixed by the design lane in this pass (CalendarUI)
| # | Issue (Accessibility XL) | Fix |
|---|---|---|
| 1 | View switch truncated ("Age…", "3 da…") | `SegmentedControl` text: `minimumScaleFactor(0.7)`, type capped at `.xxxLarge` |
| 2 | Today button date shown as "…" | `TodayButton`: capped at `.xLarge`, scale 0.6 (fixed 38pt box) — verified ("11") |
| 3 | List titles truncate to one line even when there is room | `EventRow` and `ListRow`: at accessibility sizes titles wrap to 3 lines; `EventRow` moves the status word under the title |
| 4 | `ChipToggle` "MON" wrapping (earlier pass) | already fixed |

## Remaining, ranked
### S1
1. **Notification permission prompt appears on first launch**, over Home, before the user has any context. Ask when it matters: after pairing completes or the first time Alerts is opened ("Allow alerts so reminders arrive on this phone"), with a one-line explanation screen/row first. (Swift/worker: `UNUserNotificationCenter.requestAuthorization` call site.)
2. **Reply-needed rows on Home** (Feature view, not `EventRow`) truncate title and sub line at large type ("Quarte…", "Fri 16 Oct…"): use `lineLimit(nil)` for accessibility sizes and put "Reply" under the title, as `EventRow` now does.
### S2
3. Banner and Next-block at AX-XL: banner wraps into two large lines (acceptable), "Done for today" card text wraps correctly ✓.
4. Home first-run keyboard: none.
5. Widgets: layout, flip cells, colours, amber status and "4 LEFT" header match w-home/w-states in both themes. The left account bar of the small widgets is clipped against the gallery edge (x=0, intentional in the canvas; in WidgetKit add 0pt leading padding to the bar only, not the content). Widgets use system rounded containers (WidgetKit requires them): acceptable deviation from "square corners", record in DECISIONS.
6. Time-zone label in Settings changes between "Amsterdam" and "London" across runs (simulator zone). Cosmetic.
### S3
7. Contrast: control borders 1.2–1.4:1 (Q10, still open), light amber chip text fixed earlier.
8. Light mode Home/Settings: tokens match the canvas light set (bg #F1F1EE, surface white, ink #0A0A0B, amber #8A5A00); no stray dark-only colours found.

## Next (for the worker)
Run light + AX-XL on the not-covered list with `docs/audit-shots` naming `r4/`; fix Feature views per audits 2–6; keep CalendarUI changes in sync with `docs/specs/`.

## D7 final addendum (2026-10-12)
**CalendarUI changes made:** `Theme.controlBorder` (Q10 resolved, unit-tested ≥ 3:1) applied to inputs, switches, checkboxes, radios, segmented control, chips, outline buttons; AX-XL handling (earlier). `make ios-test` DesignSystemTests: 8 tests green (separate derived-data path while the worker builds). Open questions Q4, Q8, Q10, Q11 now carry a recommended default in DECISIONS §C.

**Quick add (checked dark, normal type):** Entry is a full-screen "New event" (canvas h-new/q-type are bottom sheets); placeholder "Add an event" ✓ (no alfather yet), mic button, rows New event / Find free time / Block focus time ✓, live parse line "Fri, Oct 16 · 13:00 to 14:00" amber + place ✓, ambiguity screen "One thing to check. I won't guess." + "Which Friday?" radios + Continue ✓ (q-ambig).
### Feature code (worker): ranked
1. **Underline colours**: recognised words are underlined in ink; canvas colours them (who cobalt, when amber, where moss) and underlines only the recognised words ("at" and the title words are underlined too). Use `UnderlineSpan.attributed` per recognised range; set the underline colour from `AccountColor.cobalt`/`Theme.accent`/`AccountColor.moss`.
2. Entry is a full screen, not the h-new bottom sheet: present with `.calSheet` over Home (scrim, grip) and add the "TYPE OR SAY IT" caption (when alfather is configured) instead of "READS AS YOU TYPE"; the rows' second text should read "Full form" / "Ask alfather" / "2 hours, today".
3. Preview banner after ambiguity should be the neutral kind (`AmberBanner(kind: .neutral)`), not a bar-less amber.
4. Notification permission: move off first launch (S1 above).
5. Not captured in this pass (needs the worker's flags or device): voice (SpeechInput), degraded/alfather-down, free-time slots/book/none, Search, API tokens, a-ics read-only link, drag/resize on 3 days, widgets in WidgetKit, notification copy, app icon + launch on device. Specs for each are in `docs/specs/`; check them against the `Done means` lists.

## Accessibility items from the S7 worker (fixed in CalendarUI)
- `SegmentedControl`: hit area now 44pt (visual canvas height 40 → 44; `contentShape` on each segment).
- `EventRow`: time column scales with Dynamic Type (`ScaledMetric`, capped at 110pt) so "10:00" does not clip at AX5.
- `Fab`: `accessibilityIdentifier("fab")` (callers may still override with their own).
