# Design audit 2: conformance of Home, Pairing, Accounts after adoption

_Design lane · conformance pass 1 · source of truth: the Gnomon canvas (boards h-*, p-*, a-*, s-*), rules in `docs/design-audit-1.md` and `docs/specs/`._
Built from the repo HEAD of the working tree (iPhone 17 simulator, fixture hub, ad-hoc signed), dark and light for Home, dark for the rest; screenshots in `docs/audit-shots/r2/`. Light twins of Pairing/Accounts and the error/empty/loading states were **not** captured (fixtures cannot reach them yet: see "Fixtures" below).

## Verdict
Home, Pairing and Accounts now read as the canvas: palette, type, flip cells, Next block, rows, labels, header, Today button, view switch, nav bar, footer bar, banner, status squares all match the boards. What remains is mostly **missing states**, one **content bug** (duplicate live row), **system chrome that breaks the square language**, and small spacing/copy items. The FAB distance is fixed in CalendarUI (below).

## Fixed by the design lane in this pass
| # | Item | Change |
|---|---|---|
| F1 | **FAB position.** Canvas draws the FAB **28pt from the screen edge** (`.fab{bottom:28px}` on a 390×844 frame that includes the home-indicator zone). Built was 28pt above the *safe area* = 62pt from the edge. | `Fab` now aligns to the real bottom edge (flexible clear layer with `ignoresSafeArea(.container, edges: .bottom)`; the fill is a `Rectangle` so it cannot stretch into the safe area). Verified on the simulator: bottom margin 28pt, size 52×52, 24pt from the trailing edge. **Swift: give the agenda scroll a bottom content margin of 104pt (52 + 28 + 24) so the last row can scroll clear of the FAB.** |

## Remaining deltas, ranked

### S1: visible immediately
| # | Screen | Delta | Canvas | Fix (owner) |
|---|---|---|---|---|
| 1 | Home, live event | The in-progress event is shown **twice**: in the Next block and again as the first row under "UP NEXT" (`14:00 Lunch with Sam`). | h-live: the hero event is *not* repeated; "Up next" lists only the following events, "Earlier today" the past ones. | `HomeLogic`: exclude the hero item from the list sections (Swift) |
| 2 | Home, FAB overlap | Last rows hide under the FAB (no scroll inset). | list scrolls clear of the FAB | bottom content margin 104pt (Swift) |
| 3 | Add an account (a-add) | Presented as the **system sheet**: large rounded top corners and a translucent card break "square corners, full-width surfaces". Same for the Home long-press menu (rounded glass popover with red "Forget this server"). | a-add is a full-bleed screen with a close glyph; Settings/actions use `.sh` square sheets | `.presentationCornerRadius(0)` + `.presentationBackground(Color.calBg)` + `.presentationDragIndicator(.hidden)` on the add-account presentation, or `fullScreenCover` (Swift). Replace the context menu with `CalSheet` (see Settings entry below) (Swift, CalendarUI has `CalSheet`) |

### S2: visible on the screen
| # | Screen | Delta | Canvas | Fix (owner) |
|---|---|---|---|---|
| 4 | Pairing failed (p-fail) | Second lines of the three "Check these" rows are **truncated with "…"**; the banner's first sentence is regular weight. | `.r2` rows wrap (`white-space: normal`, 14pt dim); banner `<b>Can't reach hub…</b> Nothing was saved.` (first sentence Medium) | Rows: `lineLimit(nil)` on the sub line (CalendarUI `ListRow` now exposes `subLineLimit`, see below); banner: `AmberBanner("Can't reach 127.0.0.1.", message: "Nothing was saved.")` (Swift) |
| 5 | Home states | **Missing**: first-load skeleton card (h-load: quiet card with `--:--` flip cells at 35% + two skeleton bars, 4 skeleton rows, footnote); no-accounts (h-noacc: quiet card "No calendars yet", `CalButton` "Add an account", label "Works with" + 3 rows with colour bars: Google Calendar / Outlook and Microsoft 365 / iCloud); syncing row (h-sync: `StatusLabel(working) "Syncing 3 accounts"` + mono "2 of 3" right + 2pt `ProgressRule` + footnote); banner state (h-banner: Next block directly under the banner, that account's rows "Stale", footnote); empty (h-empty: "Clear until Thursday" quiet card, "Coming up" rows with weekday in the time column, outline "New event") | boards h-load, h-noacc, h-sync, h-banner, h-empty, h-end | Swift (all components exist: `QuietCard`, `FlipCells(dimmed:)`, `SkeletonRow`, `StatusLabel`, `ProgressRule`, `AmberBanner`, `CalButton`). Copy for the footnotes is in `design-audit-1.md` §1.7 |
| 6 | Add an account | Copy differs: "Google Calendar / Gmail and Google Workspace". Provider rows for unavailable providers show a mono "LATER" tag. | "Google / Sign in with your Google account", "Microsoft / Outlook, Microsoft 365", "iCloud / Uses an app password", "Calendar link / Any .ics link. Read only." | Use canvas copy; keep the "LATER" tag (not in canvas, acceptable until B6/M9; amber-less, `CalFont.label` dim) (Swift) |
| 7 | Accounts | `a-pick` (choose colour and calendars after connecting) not built; a-account, a-reauth, a-remove, a-back also unverified here. | a-pick: email nav title, label COLOUR + five 52×52 swatches (`ColourSwatchRow(size: 52)`), label CALENDARS + `SettingsRow`s with `Checkbox`, footer "Done" | Swift (components exist) |
| 8 | Settings entry | System context menu on long-press of the Today button. | (undrawn: Q5) | **Concrete default below** |

### S3: detail
| # | Screen | Delta | Fix |
|---|---|---|---|
| 9 | Paired | Value column starts at x≈114pt; canvas key column is 90pt wide + 16 gap → value at x=130. | key label `frame(width: 90)` (Swift) |
| 10 | Month | Today is also the selected day, drawn as the ink fill only. Canvas: today = amber number + 1.5pt inset amber ring; selected = ink fill. When both: ink fill **with** the amber ring (2pt bg gap). | `MonthGridView` (Swift); no CalendarUI change |
| 11 | Accounts | Footnote is the first sentence only; canvas adds "iCloud is slower because Apple has no push." | add the sentence when an iCloud account exists (Swift) |
| 12 | Home header | Title and Today button vertical centre ≈ 87pt; canvas h1 centre ≈ 81pt (padding-top 68 + 22pt line). | reduce top padding by ~6pt (Swift), or accept; below visibility threshold |
| 13 | Welcome / form | ✓ copy and layout; light twins not yet captured. | D7 |

## What now matches (checked)
Welcome (flip-cell CALENDAR, 36pt heading, 32ch sub, pinned footer with hint); Pair form (nav "Your server", mono-caps labels, 52pt fields, hint copy, footer "Pair this phone"); failure screen (red bar banner, "Check these", footer Try again / Edit address); Paired (PAIRED flip cells, key/value rows, "Add your first account" / "Later"); Home header (22pt title + chevron, 38pt amber Today button), view switch (Agenda/Day/3 days/Week, 40pt, ink-filled selection), Next block in dark and light (bright account colour both themes, flip cells, 30pt title, outlined Map, "Now · N min left"), mono-caps day labels, 3pt account bars at x=0, status words (Maybe), overlap second line, "Earlier today" at 45%, month grid (Monday first, per-account 5pt squares, legend, selected-day label, Next block hidden), Accounts list (nav with amber "Add", 84pt rows, status squares, hint).

## Settings entry: proposed default for Q5 (concrete)
**Long-press the Today button opens a `CalSheet`** (canvas bottom sheet, square, grip) titled "Calendar", rows: **Settings** · **Accounts** · **Server and sync** (with a `StatusLabel` at the right: OK / Offline / Sign in) · **Forget this server** (destructive, confirms with the delete-style sheet). Plus the VoiceOver custom actions "Settings", "Accounts" on the Today button, and a **first-run hint**: the first time Home appears after pairing, a one-line `ToastView("Press and hold the date for Settings", "OK")` (shown once). Reason: no extra chrome on the Home header (canvas draws none) while giving a discoverable path; problems surface the same destination through banners ("Fix" → Accounts, "Check" → Server and sync). If the user prefers a visible entry, the alternative is a 38pt square gear glyph left of the Today button (same border style), which I would spec in one line.

## Fixtures
To verify the remaining states the fixture hub needs switches (backend/Swift): `--fixtures-state=loading|empty|noaccounts|banner|offline|down|syncing`. Without them the design lane can only check these states in the component gallery or previews. Asked of the coordinator.

## Component changes made in this pass
`Fab` (edge alignment), `ListRow` gets `subLineLimit: Int? = 1` (pass `nil` to wrap, as in the "Check these" rows).

## Addendum (D7 prep): light twins and contrast
Captured in light: Welcome, Pair form, Paired, Add an account (`docs/audit-shots/r2/*-light.png`), plus Home and month earlier. All match the canvas light boards (`p-*-l`, `a-*-l`) in layout, colour and copy. Only difference is the same system-sheet corner on Add an account (item 3), visible as a grey scrim strip with a rounded card.
**Contrast check of the canvas tokens** (WCAG ratios, text on `bg`): light ink2 4.7, amber 5.2, err 5.8; dark dim 5.7, amber 11.0, err 9.0; Next-block ink on the bright account colours 7.2–9.8. All text passes AA. **Non-text boundaries do not:** `border` (canvas `pl`) is **1.22:1** (light) and **1.38:1** (dark) against `bg`, below the 3:1 WCAG 1.4.11 asks of input borders, checkboxes, switches, segment outlines. Disabled/"LATER" text (`faint`) is 1.8 / 2.3 (acceptable: disabled/decorative). The canvas draws these controls this way, so the build matches the board; the issue is the board. Proposal (DECISIONS Q10): keep `border` for hairlines/outline buttons, add `controlBorder` (light `#8F8F89` ≈ 3.1:1, dark `#6B6A65` ≈ 3.3:1) for inputs, checkboxes, switches and the segmented control. Default until the user decides: canvas values.

## Addendum 2: states checked with `--fixtures-state` (dark; shots `docs/audit-shots/r2/s-*.png`)
The app keeps its saved copy; the states below were checked on a paired app with the hub restarted per state.
| State | Observed | Canvas | Delta (ranked) |
|---|---|---|---|
| **banner** | Amber banner "Work needs you to sign in again. Its events are paused." + "Fix" ✓; Work rows marked "Stale" ✓. | h-banner | (S2) Next block must sit **directly under the banner** (no 24pt black gap; banner margin-top 16 under the switch, none between banner and block). (S2) hero event repeated in the list (audit-2 #1). (S3) missing footnote "Events from the work account stay visible but marked stale until you sign in again. Nothing is deleted." |
| **syncing** | Nothing visible. | h-sync: `StatusLabel(working) "Syncing 3 accounts"` + mono "2 of 3" + 2pt `ProgressRule` (66%) + footnote | (S2) syncing row/progress/footnote not shown at all |
| **down** | Banner "Your hub had a problem. the hub is not available" + "Retry"; Next block normal; rows normal. | y-down: red banner **"Can't reach your server."** " This agenda is from 07:12 and may be out of date." + "Check"; Next block quiet with flip cells and "in 8 min · saved copy"; every row "Stale"; footnote "Alerts already scheduled on this phone will still arrive." | (S1) copy is a raw server string with a lowercase sentence after a full stop; (S2) wrong banner copy/action, no quiet saved-copy block, no "Stale" words, no footnote |
| **empty** | Looks like normal: the old rows are still listed and a Next block shows. | h-empty: "Clear until Thursday" quiet card, "Coming up" rows, outline "New event" | (S1, behaviour) when the server returns an empty agenda the app should replace its saved copy; the stale cached rows stay on screen instead |
| **noaccounts** | "No calendars yet / Connect an account to see your day." card ✓ and **primary** "Add an account" button, but the old rows still render below. | h-noacc: card, `CalButton` primary ✓, label "Works with" + three rows with a1/a2/a3 bars (Google Calendar, Outlook and Microsoft 365, iCloud), **nothing else** | (S1, behaviour) cached events must be cleared when accounts are empty; (S2) "Works with" list missing; the gap between card and button is 64pt, canvas 28 |
| **offline / loading** | Not captured (loading needs a first launch with no saved copy; offline needs a dropped connection while the app is running). | y-offline, h-load | to check in the next pass |
Finding to route to the sync-engine owner: **an empty or accounts-less response does not clear the saved copy.** That is a data bug, not a design one, but it makes the empty states unreachable.
