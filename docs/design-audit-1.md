# Design audit 1: Home, Pairing, Accounts vs the Gnomon canvas

_Design lane · D1 · rev 2 · supersedes rev 1, which compared against the wrong source._

## Source used

**The Gnomon canvas** (https://claude.ai/artifact/9NRPFcFQbsNXewTJMLRkfZ, 214 boards, `project/gnomon.css`, notes in `project/canvas.json`; last published 2026-10-11 05:54 CEST). It is the user's named source of truth.
`design/src/*.mjs` + `design/*.html` (2026-10-10 20:04, Newsreader/brass/radius 10–30, dial, dock, segmented switch) is the **earlier iteration** and is **not** used for conformance. Evidence it is older: canvas is newer by ~10 h, has a different wordmark treatment (flip cells), a different type system (GT America), a different palette, and its own system note "Square corners. Full width surfaces." The old `design/` folder should be marked superseded (logged in DECISIONS §D).
Rev 1's findings about a header segmented switch, daylight strip, dock, dial, radius and rounded cards **do not hold** and are withdrawn. Findings that still hold are re-derived below from the canvas.

Method: built `Calendar.app` (iOS 26.5 simulator) against `calendard serve --fixtures`; screenshots in `docs/audit-shots/` (dark is the canvas default theme; light also captured for Home). Canvas numbers are CSS px of the 390×844 frame = pt. Boards compared: `h-main, h-month, h-live, h-nolink, h-dense, h-end, h-banner, h-sync, h-load, h-empty, h-noacc, p-welcome, p-pair, p-check, p-fail, p-done, a-list, a-add, a-pick, a-reauth, a-account` + `s-controls, s-rows, s-type`.

## Verdict in one paragraph

The T03 build got the **structure** right (rows, full-width, square corners, one bright block, amber for attention, hairline lists). It got the **identity layer** wrong: the palette is a different one (green-tinted chalk/ink + brass, canvas is neutral near-black/off-white + amber), the type is a different system (serif display, italics, heavy mono; canvas is one sans at 17–30 Regular/Medium, mono only for times and labels), and the signature element of the canvas, the **split-flap "flip cells"**, does not exist in the app. On top of that, Home's header, the Next block internals, the row anatomy, the section labels, the FAB and every nav bar differ. Severity: **S1** visible in the first second on every screen · **S2** visible on the screen · **S3** detail.

## 0. Global deltas (fix once in CalendarUI)

### G1 · Palette (S1) — `Tokens.swift`
Canvas `.dk` / `.lt` (gnomon.css). Built values in brackets.

| Role (canvas var → `Theme` name) | Dark | Light |
|---|---|---|
| bg → `bg` | `#0A0A0B` [#0E1411] | `#F1F1EE` [#EFF1EA] |
| p1 → `surface` (cards, inputs, banners, sheets) | `#141416` [#161E1A] | `#FFFFFF` [#F8F9F4] |
| p2 → `sunk` (skeleton, range) | `#1B1B1E` [#1E2923] | `#F7F7F5` [#E4E8DE] |
| pl → `border` (input/outline-button/segment borders) | `#2A2A2D` | `#DCDCD6` |
| line → `line` (row hairlines) | `#1D1D20` [#2B3830] | `#DEDEDA` [#D3D8CA] |
| ink → `ink` | `#F4F1EA` [#E9EEE3] | `#0A0A0B` [#16221D] |
| dim → `ink2` | `#8B8A84` [#9AA89E] | `#6B6B66` [#56645B] |
| faint → `ink3` (placeholder, other-month days) | `#4D4D4A` [#5E6D64] | `#B4B4AD` [#8E9B93] |
| amber → `amber`/`accentInk` (**the only accent**; now line, status words, links, banner bar, today) | `#F0B84A` [brass #E6B54E] | `#8A5A00` [#80560A] |
| strike → cancelled text | `#6B6A65` | `#9A9A94` |
| err → `red` (text, border, error bar) | `#FF8F8F` [#F0866B] | `#B3261E` [#A5361F] |
| pbg/pink → primary button fill / text | = ink / = bg | = ink / = bg |
| scrim | `rgba(0,0,0,.62)` | `rgba(10,10,11,.34)` |
| `wp` (widget wallpaper mock) | `#16161A` | `#D9D9D3` |

Account colours (`a1 a2 a3`, plus picker extras): cobalt `#3E63DD`/`#7C98F2`, rose `#C9507F`/`#EE86AC`, moss `#6A8A2E`/`#A3C25E`, ochre `#E0A94A`, mauve `#B28CE0` — **already match `AccountColor`**. New rule: the **Next block always uses the bright (dark-set) value in both themes with ink `#0A0A0B` text** (`a1b/a2b/a3b`); today's light mode draws it with the darker light value and near-white text, which is wrong. Remove: brass `accent`, `amberTint`, `redTint`, `green/greenTint` (canvas "ok" status = moss `a3` square), `inverse*`. Success/OK colour is `a3` (moss) only as the 8×8 status square.

### G2 · Typography (S1) — `Typography.swift`
Canvas: **GT America** Regular/Medium + **GT America Mono** Regular/Medium. *"Nothing heavier than Medium. Size carries the hierarchy."* No serif, no italics anywhere. Scale (pt):

| Token | Spec |
|---|---|
| `nextTitle` | 30 Medium, line 44, single line ellipsis |
| `bigTitle` (screen H1 on pairing, welcome 36) | 30 Medium, lh 1.15 (welcome 36) |
| `screenTitle` (home header) | 22 Medium |
| `navTitle` | 18 Medium, centred |
| `listTitle` | 19 Regular |
| `body` | 17 Regular (buttons 17 Medium) |
| `status` | 15 Regular, amber (dim for cancelled/declined/stale) |
| `sub` (row 2nd line, hint) | 14 / 13 Regular dim |
| `time` | Mono 15 Regular dim, 48pt column |
| `label` | Mono 11 Regular, +0.14em, UPPERCASE, dim |
| `flip` | Mono Medium 26 (cell h44) · `.s` 18 (h30) · `.xl` 44 (h60) |

**Font licensing (needs the user):** GT America is a commercial family; the canvas embeds the user's own uploads. Until licensed files are placed in `ios/Calendar/Resources/Fonts/`, the stand-in is **Schibsted Grotesk Regular/Medium + DM Mono Regular/Medium** (both OFL, already bundled) mapped to the same tokens; swapping = editing 4 PostScript names in `CalFont.Face`. Newsreader is retired from UI (keep file until the swap is confirmed). Logged in DECISIONS §C.

### G3 · Flip cells — the signature (S1, missing) — new `FlipCells` in CalendarUI
`.fl`: each **character** (digits, colon, letters) is its own cell: Mono Medium, colour `#F4F1EA`, cell fill `#26262A` (inside the Next block `#1B1B1E`), 2pt gap between cells, a 2pt `#050505` split line across the vertical middle of every cell, no corner radius. Sizes: 26/h44 (Next block time), 18/h30 (`s`), 44/h60 (`xl`, wordmark). Used for: Next-block start time, event-form time pickers (flip with ±15), Welcome wordmark "CALENDAR", Paired "PAIRED". Animation (Settings › Appearance "flip animation"): changed digits flip top-half-down 0.25 s settle; off for Reduce Motion and when the setting is off. API: `FlipCells(text:, size: .regular/.small/.xl, onDarkCell: Bool)`.

### G4 · Shape rules (S2) — already mostly right
Square corners everywhere, full-width surfaces, hairlines `1pt line`, `1pt border` outlines. Confirmed in build. Exceptions to keep: radio `.rd` is a 20pt circle (`li .rd`). **Remove** the iOS 26 system glass nav buttons (round chrome) seen on Accounts: canvas nav is bare (below).

### G5 · Buttons (S1) — `PrimaryButton`/`SecondaryButton`
Canvas `.bt`: full width, **h52**, no radius, 17 Medium. `bt` ink fill / bg text; `bt o` transparent, 1pt `border`, ink text (built: sunk fill + line border, wrong); `bt a` amber text + 1pt amber; `bt e` err text + 1pt err; disabled `.35` opacity; `sm` h40, auto width, pad 18, 15pt. Built is 48 high, filled. Footer pattern `.ft2`: pinned bottom, bg fill, 1pt `line` top, padding 16/24/**34**, buttons stacked gap 10. **No brass/accent-filled button exists**; rev-1 "brass" variant is withdrawn. Fix: `CalButton(_, kind: .primary/.outline/.amber/.danger, size:)` + `FooterBar { … }`.

### G6 · Nav bar (S1) — new `ScreenNav`
`.nv`: h56 below the status bar; leading 44×44 chevron (`i-b`, 1.75pt, no container), centred 18 Medium title, trailing 44 text action in amber 17 ("Add") or X (`i-x`) for modal flows. Built: system `NavigationStack` toolbar with glass circle buttons + large serif title at left (Accounts screenshot). Fix: `.toolbar(.hidden, for: .navigationBar)` on Accounts/Pairing screens (Home already does) and add `ScreenNav(title:, back:, action:)`. **Feature change.**

### G7 · Banner `.bn` (S2) — `AmberBanner`
Surface `p1`, 1pt `line` top and bottom, full width (margin-top 16), padding 14/24, 15pt, name in Medium, **3pt bar** at left inset 12 top/bottom (amber, or err for `e`), action text right-aligned amber Medium ("Fix", "Retry"). Built: amber-tint fill, full-height bar, stacked title/message, underlined "Sign in". Fix tokens + layout; copy: "**[account]** needs you to sign in again. Its events are paused." / action "Fix".

### G8 · Status `.stt`, tags, toast, skeleton (S3)
`.stt`: 8×8 **square** + 13 dim label; ok = moss, wr = amber, bd = err. `.tag`: mono 11 caps .08em, 1pt `border`, pad 2/8 ("Read only", amber variant "Reply needed"). Toast `.ts`: inset 12, bottom 96, ink fill/bg text, pad 14/16, 16pt, action `u` Medium (built full-width strip, brass action). Skeleton `.sk`: 14pt flat `p2` bar, no radius.

## 1. Home (boards h-main, h-live, h-nolink, h-dense, h-end, h-empty, h-noacc, h-banner, h-sync, h-load)

Screenshots: `b1-home-light.png`, `b1-home-dark.png`.

### 1.1 Header (S1)
Canvas `.hd`: padding 68/24/0, flex-between. Left: **"October 2026" 22 Medium** (one style, no italic) + CSS chevron down (up when month open), gap 8. Right: **38×38 square button**, 1pt `line` border, `p1` fill, **amber mono 15 date number of today ("11")**, label "Jump to today". That is the only trailing control.
Built: 32 serif "October" + 32 italic "2026" + chevron; a "+" glyph; a 32pt **account-colour square avatar "W"**; a 2pt grey rule under the header.
Fix: `HomeView.titleRow` → 22 Medium title, remove "+" (moves to FAB), remove avatar square, add `TodayButton(day:)`; remove the `ProgressLine` rule when idle. **Settings/Accounts access:** canvas Home has no entry (flow-set says "avatar or gear" without drawing one). Interim: long-press the Today button or the title opens a menu; asked in DECISIONS §C. **Feature change.**

### 1.2 Next block (S1) — boards h-main, h-live, h-nolink, h-end, h-empty
Canvas `.nx`: full width, margin-top 20, padding 22/24, bg = account colour (bright value), text `#0A0A0B`, flex row gap 16: left **flip cells** (event start time), right column gap 10: title 30 Medium (1 line, ellipsis), footer row 18pt space-between: left text ("in 8 min", "Now · 22 min left", "Kōya · 14 min walk"), right **action**: `jn` filled `#0A0A0B` with `#F4F1EA` 16pt text, pad 7/20 ("Join"), or `jn o` outlined 1pt `#0A0A0B` transparent ("Map", "Details"). No kicker line, no dial, no "IN PROGRESS", no mono bold countdown, no meta line, no full-width button.
Variants: **live** footer "Now · N min left" + Join stays until the event ends; **no link** → `Map` (outlined) + "place · N min walk"; **quiet** (`nx q`): `p1` fill, 1pt `line` top/bottom, ink text, no flip cells, title "Done for today"/"Clear until Thursday", footer dim "Next: Standup, tomorrow 09:00"; **loading** quiet card with `--:--` flip cells at 35% and two skeleton bars.
Built: account-colour block ✓, but 24 serif title, 12 caps kicker, bold mono "Started 29 min ago · 31 min left", meta line, full-width inverted button "Directions", near-white text in light.
Fix: rewrite `NextEventBlock(time:, title:, footerLeft:, action: .join/.map/.details/.none, color:, quiet:)` using `FlipCells`. **Feature change (`HomeView.hero`/`HomeLogic`):** pass start time string, a single footer string (`in 8 min` / `Now · 22 min left` / `place · N min walk`), action kind; add the quiet states (done today, clear until X).

### 1.3 Section labels (S1)
Canvas `.lb`: padding 32/24/10, **Mono 11, +0.14em, UPPERCASE, dim**: "Today", "Tomorrow", "Up next" (live), "Earlier today", "Coming up", "Tue 13 Oct". Built: 20 serif "Today" + 20 italic "Oct 11" + brass **square** marker + mono "4 events". Fix: `DaySectionHeader` → label only; drop count, italic date, today marker. Date form for non-relative days: "Tue 13 Oct".

### 1.4 Rows (S1) — `.r`
Canvas: h58 (r2: min 72 two-line), padding 0/24, 1pt `line` bottom border **full width** (first row of a list also has a top border `t1`). **Account bar `.ac`: 3pt wide, absolutely at the screen's left edge (x=0), top 15 / bottom 15 (28pt tall).** Then: time Mono 15 **dim**, 48pt column (start time only, no end time) · title 19 Regular, one line ellipsis · status word 15 Regular **amber sentence case** right-aligned ("Moved 11:45", "Reply needed", "Maybe"); dim for "Cancelled", "Declined", "Stale"; cancelled/declined titles line-through in `strike`. Past = whole row opacity .45, **no hatch**. Overlap = second line in the row (`r2`): 14pt err "Overlaps with Roadmap review". All-day rows show "All day" in the time column. Location is **not** shown in the list (it appears in the footer of the Next block and in detail).
Built: bar between time and title and full row height (~46pt); time ink; end time stacked under start; 16 title; location under title; "MAYBE" caps label; hairline inset 20; past = hatched shade at .7; red text with a warning triangle for overlap.
Fix: `EventRow` rebuilt per above; bar position moves to x=0; remove `HatchedShade` background, `end`, `location` from the row (keep in model for a11y label). Keep the overlap line (it is canvas-correct), drop the triangle glyph and `OverlapMarker`. **Feature change:** status mapping to words {Moved HH:MM, Reply needed, Maybe, Declined, Cancelled, Stale, New}; `HomeLogic` moves past events into an "Earlier today" section **after** upcoming ones (not hatched inline); in-progress day uses label "Up next".

### 1.5 FAB (S1, missing)
`.fab`: 52×52 square, bottom 28 right 24, fill `pbg` (ink), glyph `pink` (bg) 18pt plus (1.75pt strokes), no shadow. Opens the new-event sheet (h-new: scrim, `sh` bottom sheet with 32×3 grip, field "Add with alfather · TYPE OR SAY IT", rows "New event / Full form", "Find free time / Ask alfather", "Block focus time / 2 hours, today"). Built: "+" in the header. Fix: `Fab(action:)` in CalendarUI; `HomeView` overlay. **Feature change.**

### 1.6 Month open (S2) — h-month
Canvas: header chevron flips up; **no Next block while open**; grid card = `p1`, 1pt `line` top/bottom, padding 6/8; weekday row 30pt, **"Mon Tue Wed…" 12 dim, week starts Monday (setting g-week)**; day cells h50: number 34×34 box, 17 Regular tabular ink; other-month days `faint`; below the number a **5×5 square per account that has events that day** in that account's colour (max 3, gap 3); **today** = amber number + 1.5pt inset amber ring (not filled); **selected** = ink fill with bg text; legend row `.lg`: 3×12 bars + account names 12 dim, top border; below the card the label "Tue 13 Oct" and that day's rows (no Next block).
Built: extra "October 2026" serif sub-header with ‹ › arrows; one-letter weekdays "M T W"; 17 serif numerals; today **filled chalk**; one grey dot; "Today" outlined button + "A dot marks a day with events"; Next block still shown.
Fix: `MonthGridView` per canvas; month stepping by swipe (arrows removed; "Today" = the header Today button). **Feature change:** per-account day dots from the agenda; hide hero while open; selected-day list label.

### 1.7 Banner, sync, offline, loading, empty (S2)
- `h-banner`: `bn` under the header, **Next block directly under it** (`margin-top:0; border-top:0`), rows of that account get "Stale"; footnote 16 dim "Events from the work account stay visible but marked stale until you sign in again. Nothing is deleted." Built: banner fine in concept (see G7).
- `h-sync`: row `stt wr` "Syncing 3 accounts" + mono "2 of 3", 2pt amber progress `pg` under it; content stays; footnote "Your server checks each account about once a minute. Pulling down asks it to check now." Built: thin line + plain note.
- `h-load`: header without chevron, progress line, quiet Next card with skeletons, four skeleton rows, footnote "Reading calendars from your server. This only happens on the first launch…". Built: four skeleton rows, no card skeleton, no footnote.
- `h-empty`: quiet card "Clear until Thursday / Next: Design review, Thu 14:00", label "Coming up", rows with weekday in the time column ("Thu … 14:00" status), `bt o` "New event". Built: "Nothing planned" line only.
- `h-noacc`: quiet card "No calendars yet / Connect an account to see your day.", `bt` "Add an account", label "Works with" + three rows (bars in a1/a2/a3): Google Calendar, Outlook and Microsoft 365, iCloud.
- Offline/unreachable (y-offline, y-down): in D3 spec. Today's build shows "Can't reach your hub" **twice** (note line + empty-state title) plus "Today — Nothing planned", which is false when the fetch failed. **Feature change:** show the red `bn e` only; keep saved rows marked Stale; never render an empty state when `loadError != nil`.

## 2. Pairing (p-welcome, p-pair, p-check, p-fail, p-done)

Screenshots: `p-welcome-{light,dark}.png`, `p-after-deeplink-dark.png`, `p-paired-dark.png`, `pair-failed-dark.png`.

| # | Sev | Board | Built | Canvas | Fix |
|---|---|---|---|---|---|
| P1 | S1 | Welcome | 14×56 brass bar, serif 32 "Your calendars, / on your own server." vertically centred | `fl xl` flip cells **CALENDAR** at y=150 (x=24); H1 **36 Medium** "Your calendars,<br>on one board." (mt 36); sub 16 dim max 32ch "Calendar reads Google, Outlook and iCloud through your own home server. Nothing leaves your tailnet."; footer `bt` "Connect to your server" + hint 13 dim centred "Needs Tailscale on this phone" | `FlipCells(.xl)`; copy; footer bar. (Backend is Google-only until B6/M9: keep the canvas sentence, it is a statement of intent; flagged in §C) |
| P2 | S1 | Pair form | no nav; 24 serif "Pair with server"; labels 16 sans; chalk button inline | `ScreenNav` "Your server"; sub 16 dim "Enter the address of your hub and an access token made on the server."; mono caps labels (11, +.14em) 8 above fields; fields h52, 1pt `border`, `p1`, 17pt, **focused = 1pt ink**, error = 1pt err; hint under token "Make one on the server under API tokens. This phone gets its own token you can revoke."; pinned `bt` "Pair this phone" | G5/G6, `CalField` restyle (label style, h52, focus/err borders). Keep "Paste from clipboard" as 15 amber text button below the hint (not in canvas: §C) |
| P3 | S1 | Checking | serif "Checking the server"; rows with ✓/spinner glyphs; steps: Reach the server, Secure connection, Token accepted, Version compatible | `ScreenNav` "Your server"; **30 Medium "Checking"**; sub host (16 dim); 2pt amber `pg` 55%; 4 rows (h58): "Reach the server", "Check the token", "Read the server version", "Save this phone" with `stt` **Done / Working / Waiting**; waiting row title dim; footer `bt o` "Cancel" | Replace glyph column with `stt`; step names; progress line; Cancel. (TLS note "Plain http, local address" may live as `sm` second line on step 1, keep: useful) |
| P4 | S1 | Failed | serif "Couldn't pair", full checklist, tinted banner "Couldn't save the pairing", "Things to check: Try again" | `ScreenNav`; red `bn e` "**Can't reach {host}.** Nothing was saved."; label "Check these"; three `r2` rows: "Tailscale is on / Open Tailscale and make sure this phone shows as connected.", "The server is running / The hub container should be up on your home server.", "The address is right / It should end in .ts.net and start with https."; footer "Try again" (`bt`) + "Edit address" (`bt o`) | Copy and layout. Also **bug seen**: a keychain-save failure (unsigned simulator build) is presented as a server failure with a one-item list; spec a distinct line for local failures: `bn e` "Couldn't save the token on this phone." + only "Try again" |
| P5 | S1 | Paired | 14×56 teal bar, serif "Paired", mono "calendard-fixtures dev", chalk "Add an account", outlined "Not now" | flip cells **PAIRED** (xl, y=150); H1 36 "This phone is<br>on your board."; rows `tm` 90pt label + value: Server / Version / Token ("Calendar · this phone"); footer "Add your first account" + `bt o` "Later" | `FlipCells`, key/value rows, copy |

## 3. Accounts (a-add, a-list, a-pick, a-reauth, a-account, a-msadmin, a-ics, a-icloud…)

Screenshot: `accounts-dark.png` (list). Other account screens not reached with fixtures in this run; deltas below are from code + board and flagged ○ unverified.

| # | Sev | Screen | Built | Canvas | Fix |
|---|---|---|---|---|---|
| A1 | S1 | List | glass round back/"+" buttons; 24 serif "Accounts" left; 14 colour **square inset**; email 17; sub "Up to date · Synced 8 minutes ago"; chevron | `ScreenNav` "Accounts" centred, trailing amber text **"Add"**; rows **h84**: 3×44 bar at x=0 (top/bottom 20), title = email 19, sub 14 dim "Google · 4 calendars", right `stt ok` "1 min ago"; hint 13 dim "Your server checks each account about once a minute. iCloud is slower because Apple has no push." | `AccountRow`; ScreenNav; hint |
| A2 | S2 | Needs sign in (a-reauth) | ○ | `bn` "[email] needs you to sign in again." + "Fix", row status `stt bd` "Sign in"; hint "Access was removed or expired. Its events stay on your server and show as stale until you sign in." | G7, `.stt` |
| A3 | S2 | Add (a-add) | ○ | X close, sub 16 dim, **four** rows h84 (Google a1, Microsoft a2, iCloud a3, "Calendar link / Any .ics link. Read only."), chevron `i-r`, hint "Each account gets its own colour. You can change it later." | rows; 4th option appears once B6 ships |
| A4 | S2 | Pick (a-pick) | ○ | email as nav title; label COLOUR + five 52×52 squares (selected = 2pt bg gap + 1pt ink ring); label CALENDARS; rows `tg` h60 with **22×22 square checkbox** (ink fill, bg check); hint; footer "Done" | `ColourSwatchRow`, `CheckRow` |
| A5 | S2 | Account (a-account) | ○ | status `stt bd` + `tag` provider; Colour; Calendars rows with chevron ("Default for new events" sub); Sync `kv` blocks (mono caps 11 label + 18 value; error value err 16); footer "Sign in again" + `bt e` "Remove account" | `KeyValueRow` |

## 4. Fix ownership

**Design lane — CalendarUI (D2), in this order:** (1) `Theme` palette + `AccountColor` bright/light values, remove brass/tints; (2) `CalFont` scale + stand-in faces; (3) `FlipCells`; (4) `CalButton` + `FooterBar`; (5) `ScreenNav`, `TodayButton`, `Fab`; (6) `NextEventBlock` rewrite + quiet variant; (7) `EventRow` (bar at x=0, status words, no hatch) + `DaySectionHeader` label; (8) `Banner`, `Status`, `Tag`, `Toast`, `Skeleton`; (9) `CalField` restyle, `AccountRow`, `CheckRow`, `KeyValueRow`, `ChecklistRow`; (10) update Gallery + `DesignSystemTests` token tests.
All with the existing initialisers kept as thin wrappers where possible so Feature* keeps compiling; breaking API changes are listed to the Swift agent per step.

**Swift lane (Feature\*), after the matching D2 step lands:** hide system nav bars and use `ScreenNav` (Pairing, Accounts); `HomeView` header (title 22, Today button, FAB, no avatar/+), hero data (time string, footer string, action kind, quiet states), section labels, "Earlier today" placement, status words, month grid (Monday-first weekdays, per-account dots, hide hero while open, selected-day label), banner placement + copy, offline/empty-state logic, Pairing copy and step list, checking `stt` rows, paired key/value rows.

## 5. Not covered / next
Day/3-day/Week (v-\*), Event detail (d-\*), Settings (g-\*), Offline/status (y-\*) are D3 specs. Light mode was captured for Home and Welcome only; the rest of the light captures are D7.
