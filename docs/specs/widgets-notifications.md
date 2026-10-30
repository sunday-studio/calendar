# Widgets, notifications, Live Activity (v2)

Canvas: page "Widgets and alerts", row **Widgets and notifications v2** (boards `w2-small`, `w2-medium`, `w2-large`, `w2-states`, `w2-modes`, `w2-lock`, `w2-anatomy`, `w2-n-remind`, `w2-n-changes`, `w2-n-other`, `w2-n-long`, `w2-n-rules`, `w2-live`, each with a `-l` light twin). The old boards `w-home` to `w-summary` are superseded and kept only for reference.
Targets: `ios/CalendarWidgets`, `ios/CalendarNotificationService`, `ios/CalendarNotificationContent` (new), shared code in `ios/Packages/Surfaces` (`SurfacesCore` logic, `SurfacesUI` views). Stories 6.1, 6.2, 6.4, 7.5.
Sample day on every board: Tuesday 13 October 2026, 09:52. Events: Ana's birthday (all day), Standup 09:00, Roadmap review 10:00 (Meet), Dentist 11:45, Lunch with Sam 12:30 (Kōya), Design review 14:30 (moved from 11:00), 1:1 Maya 15:00, Gym 17:30 (cancelled), Dinner 19:00 (reply needed). Tomorrow: Standup 09:00, Design sync 11:00, Lunch 12:30, 1:1 Maya 15:00, Dinner 19:30.

## 0. The one constraint: the shell is the system's and it is round
iOS draws every widget, lock accessory, banner, long-look frame, Live Activity card and Dynamic Island in a rounded shape. We cannot make it square. Rev 2 of the boards (owner comments on W2.2, W2.3, W2.6, W2.13) stops fighting the shell: every **filled** shape inside it is concentric to it. Hairlines, row dividers and flip-cell glyphs stay square; 3 pt account bars get a 2 pt radius.

**The concentric rule (all surfaces).** `r_inner = R_outer - inset`, where inset is the gap from the filled shape to the container edge, never below 4 pt for small chips.
- Widget R is 24 on the boards (read it, do not hardcode). Next-event block: inset 8, so r **16**. A control inside that block (Join, Reply): inset 8 from the block edge, so r **8**, height 32.
- Live Activity card R 26: block inset 8, r **18**; Join inset 8 inside it, r **10**, height 36.
- Long look frame R 34: Join is 36 high at inset 16, so r 18 = a capsule, which is exactly concentric.
- Dynamic Island expanded (edge is fully round): Join is a 72 x 36 capsule at inset 24.
- Elements far from every corner (timeline event blocks, tags, skeleton blocks) use the 4 pt floor.
- In code: `ConcentricRectangle` (iOS 26) or `RoundedRectangle(cornerRadius: containerRadius - inset)` where the container radius comes from `ContainerRelativeShape`; never a literal 16.

**Grid and corner-safe rules (one grid per size, boards w2-medium, w2-large, w2-anatomy):**
1. **Content inset 16** from every container edge (accessory rectangular 12, circular 4, Live Activity card 16, Dynamic Island expanded 24). No text or fill closer.
2. **Blocks inset 8, content inside them still at 16** from the container edge (block padding 8). So text in the block and in the rows below share one left edge.
3. **Columns (medium, large, 338 wide):** bar x16 (3 x 14, r 2); time column x27 to x70 (43 pt, mono 12); title x78 (14 Regular); trailing column right-aligned to x322 (= W - 16): status, `+N MORE`, Join. Row height 24; hairlines run x16 to x322.
4. **Vertical:** list header text top at y12 (16 pt line); last row's bottom at least 8 above the container edge and its text at least 16 (row 24, centred text). Medium block layout: block y8 to y56, rows from y58, footer line y129 to y151 (footer text 22 pt line, bottom 7).
5. **Radius is read, not hardcoded.** `ContainerRelativeShape()` for any clip that must follow the container; `@Environment(\.widgetContentMargins)` where available. Banner (about 28), long look (about 34), island expanded (about 44) and Live Activity card (about 26) are system-controlled and only drawn for context.

## 1. Shared rules

Type (GT America, from `CalFont`; `CalFont.font(.mono, ...)` for mono): row title 14 Regular; row time mono 12; status mono 10 caps tracking +.06em; header label mono 10 caps +.14em dim; block title 15 Medium, block sub mono 10 caps; small widget title 16 Medium, line-height 19, 2 lines; small widget flip cells 22 pt (cell height 36), block flip cells 18 pt (cell 30, 54 pt wide); small widget countdown mono 13 Medium; state-card title 19 Medium. Widgets never use a colour not in `Theme`.
Colours (rev 2, fixes the muddy tints the owner saw on W2.2): the widget reads like Home. **One bright block** for the next event, in its account colour (`AccountColor.bright`: `#7C98F2` blue, `#EE86ac` pink, `#A3C25E` green, the same in dark and light) with ink `#0A0A0B`, flip cells on `#1B1B1E`. **Everything else is neutral**: container `wgbg` (dark `#141416`, light `#FBFBF9`, system appearance, DECISIONS Q8), hairline `wl` (dark white 11 %, light black 10 %), text `calInk`, secondary `calInk2` (dim), event fill `wgev` (dark `#202024`, light `#EFEFEB`). Account bars in rows use `AccountColor.base` (dark `#7C98F2 #EE86AC #A3C25E`, light `#3E63DD #C9507F #6A8A2E`). **Amber `#F0B84A` is only for "needs you"** (OVERLAP, REPLY, sign in, the now line), in both themes, and as a **filled tag** (r 4, ink `#0A0A0B` 10 mono Medium, 2 x 6 padding), never as amber text, because amber text goes brown on light. Informational statuses (MOVED, time left, cancelled) are dim mono text, no fill. Countdowns are ink, not amber. Cancelled is struck through and dim. Text weights: Regular and Medium only. Gnomon class names `.d .n .nx .am .gh .t .s` collide with the w2 sheet, so widget classes are prefixed `z` (the v1 boards inherited a pink padded bar from `.nx`; that was the root of the W2.2 comment).

Ordering inside any list (`TimelineBuilder.relevant`, one place):
1. All-day events never take a row. They are the quiet all-day line: the subtitle of the date header (large), or the footer line after the date (medium), drawn as a 6 pt dot in the account colour plus the name in dim 12 pt text. Several read "Ana's birthday +1" and tap opens the day. Not a chip, not a box.
2. The in-progress timed event, else the next one, is the bright block (shows time left or the countdown). The rows below start after it.
3. Timed events by start, ties by end then title.
4. Cancelled: hidden everywhere (the Gym case). Declined by the user: hidden unless "Show declined" is on, then faded to 45 % opacity. "Maybe" and "needs reply" show normally; needs-reply carries the REPLY status.
5. Status word priority per row, one only: time left or minutes to start (next row), MOVED (changed in the last 24 h), OVERLAP, REPLY.

Overflow: never clip a row. Show **`+N MORE`** (mono 10, trailing edge of the header in large, of the footer in medium) where N counts the timed events that did not fit and are still to come today. When today is exhausted, tomorrow's events fill the free slots under a `TOMORROW · WED 14` label that takes one slot; `+N MORE` then counts tomorrow's remainder.

Countdown text rules:
- Single-event surfaces (small, medium B next block, lock circular/rectangular, Live Activity, island): more than 60 min away: words "in 2 h 38 min" (entry at each boundary; hourly entries are enough). 60 min or less: `Text(timerInterval: now...start, countsDown: true)` in mono, shown as "IN 7:52", and "22:00 LEFT" counting to the end when in progress. Constrain it (`.frame(maxWidth: 64, alignment: .leading)`, `.monospacedDigit()`, `multilineTextAlignment(.leading)`) because `Text(timerInterval:)` otherwise expands to fill the width.
- List rows (medium A, large): words from timeline entries ("8 MIN", "22 MIN LEFT"), never timers (a timer per row costs width and invites different tick phases). Entries: one per boundary, every minute for the last 15 min before the next start, every 5 min from 15 to 60 min, none inside rows beyond 60 min (rows show the clock time only).
- Stale (last sync older than 6 h): drop all countdown and status words, show "SAVED 7 H AGO".
- Under 1 min: "NOW", then the in-progress form.

Timeline entry strategy (extends `TimelineBuilder`): entries at every start and end boundary in the next 8 h plus the cadence above; at most 100 entries; `.after(max(lastEntry, now + 15 min))`; reload on every successful sync (`WidgetCenter.reloadTimelines(ofKind:)`) and when accounts change. Budget: the system gives roughly 40 to 70 reloads a day, so the sync hook is the only extra source. Midnight: an entry at 00:00 re-evaluates "today". The large Day variant needs no extra entries (the now line is drawn from the entry date).

Accessibility: one combined element per widget (`accessibilityElement(children: .ignore)` with a built label) plus one label per row for medium/large because rows are separate `Link`s. Label grammar: "Roadmap review, 10:00 to 10:30, Google Meet, in 8 minutes." Rows: "Design review, 14:30, moved from 11:00, overlaps 1:1 Maya." Statuses are spoken words, never "8 MIN" caps. All colour meaning has a text twin (MOVED, OVERLAP, REPLY, SAVED). Dynamic Type: widgets do not grow; titles clamp to one line (rows) or two (small) and truncate.

Deep links (extend `SurfaceLink`): `calendar://event/<id>` (existing), `calendar://day/<yyyy-MM-dd>` (new; Today is `day/<today>`, keep `calendar://today` as an alias), `calendar://new` (new, optional `?start=` ISO date), `calendar://accounts`, `calendar://summary?date=` (existing). Taps: small widget = whole widget opens the event (one target); medium and large = each row is a `Link` to its event, the header opens the day, an empty or done state opens `calendar://new` or the day; state cards: sign in and no accounts open `calendar://accounts`. Join is a `Link` with the meeting URL on medium and large only (small has no second target); it appears as the mono tag MEET on the small widget and the row stays a plain event link.

Widget configuration (`AppIntentConfiguration`, one `CalendarWidgetIntent`, shared by Medium, Large and the date widget):
- `Large layout`: Agenda (default) | Day. Large only.
- `Medium layout`: List (default) | Next + list. Medium only.
- `Calendars`: all (default) or a multi-select (`CalendarEntity` query over visible calendars from GRDB).
- `Show declined`: off by default.
- `Show titles on lock screen`: off by default (titles hidden while locked; see section 5).
Entities and the intent live in `SurfacesCore` (no UI), so the timeline builder can filter.

## 2. Widget sizes (boards w2-small, w2-medium, w2-large)
Sizes are for the 390 pt class; the system scales other devices, so lay out with `GeometryReader` fractions only for the Day timeline and read the container size from the environment.

### Small, 158 x 158, inset 16 (`WidgetNextSmall`)
- Block (`AccountColor.bright`, r 16, inset 8): x8 y8 142 x 90. Inside, at 16 from the container edge: flip cells 22 pt (cell 36, 66 pt wide) at (16, 16); on the right, trailing at x142, a `zkn` tag "MEET" (mono 9 caps, 1 pt ink 45 % border, r 4) when the event has a video link; title 16 Medium, top 58 (block top + 50), 2 lines max.
- Below the block, bottom 16: line 1 mono 13 Medium ink countdown ("IN 7:52" / "22:00 LEFT" / "IN 2 H 38 MIN"); line 2 (in person) 12 Regular dim "Kōya · 14 min walk" (travel only with location permission; otherwise the place alone).
- One tap target: the event. Date variant (neutral, no block): mono 12 "TUESDAY" at (16, 16), day number 44 Regular at y34, bottom 16 mono 11 "6 LEFT TODAY" (cancelled and declined excluded); taps open `calendar://day/<today>`.

### Medium, 338 x 158, inset 16: layout A (default, recommended) and B
**A, block + list** (`WidgetTodayMedium`): block 322 x 48 at (8, 8), r 16: flip cells 18 pt (54 wide) at x16, title 15 Medium at x78 (1 line), sub mono 10 caps "IN 8 MIN · MEET" under it, **Join** 32 pt high, r 8, ink fill, `#F4F1EA` 14 Medium, trailing at x322 (a `Link`; absent when there is no link; "Reply" replaces it when the event needs a reply). Rows from y58: **3 rows x 24 pt** on the grid in section 0 (bar x16, time x27, title x78, status right-aligned at x322, `OVERLAP`/`REPLY` as an amber tag, `MOVED` as dim mono). Footer y129 to y151: "TUE 13" + the all-day line on the left, `+N MORE` on the right (mono 10 caps +.14em dim). In progress: same block with "22:00 LEFT · MEET". Evening with fewer than three left: a `TOMORROW · WED 14` label takes one slot (20 pt), tomorrow fills the rest.
**B, next panel + list** (option): panel x8 y8, 132 x 142, r 16, bright: mono 10 "NEXT" at (16, 16); flip cells 18 pt at y34; title 15 Medium 2 lines at y72; bottom 16: countdown mono 11 Medium + Join 28 pt high (r 8, inset 8 from the panel). Right column: its own grid, bar x156, time x167 (38 pt), title x213; header "TUE 13" + all-day line at y12; four rows from y30; `+N MORE` footer right at x322. A needs-you row shows an 8 pt amber dot at x322 (no room for a word). Changed rows are not coloured.
Recommendation: **A is the default** (more events, Join reachable, same look as Home); B is for people who want the big countdown, picked in the widget settings.

### Large, 338 x 354, inset 16, default **Agenda**, option **Day**
**All-day treatment (recommended, answers "rethink this"):** the date header becomes two lines: top line "TUE 13" left and `+N MORE` right (mono 10 caps, 14 pt), second line the all-day line (dot + "Ana's birthday", 12 Regular dim, 18 pt). It sits on the same x16 edge as the bars, costs 18 pt and never looks like a row. Header block y14 to y48.
**Agenda** (`WidgetAgendaLarge`): header, then the block (same 48 pt block as medium, y54 to y102), then today's rows from y104 (5 rows x 24 on the sample), the `TOMORROW · WED 14` label (20 pt: text then a hairline to x322), then tomorrow's rows (4 on the sample) ending at y340. **9 rows + block**. If today has more than the free slots, tomorrow shrinks first, then `+N MORE`.
**Day** (`WidgetDayLarge`): timeline left x16 to x192 (hour column 26, event area 150), **33 pt per hour**, window starts at the current time rounded down to 15 min (09:45 at 09:52) and runs 8.5 h; hour lines 1 pt `wl`, hour labels mono 9; events `wgev` r 4 with a 3 pt rounded bar at 4 pt inset and title 11 (one line; two lines allowed in a split column); the next event is the bright event (`AccountColor.bright`, ink text, no bar). Overlaps split the column evenly (max 3). Past time hatched (135 degrees, 1 pt every 6 pt), amber 1 pt now line with a 7 pt amber dot at its start (no label). Divider at x204; right column x216 to x322 (106 pt): `UP NEXT` mono 10, then up to five 50 pt items (mono 10 time + tag or dim status, title 13 one line). It lists what the window cannot show (Dinner 19:00). Each block and item is a `Link`; empty space opens the day.
Recommendation: **Agenda is the default** (reads faster, same visual language as Home, no layout maths); Day is the configurable option for people who plan by free gaps.
- iPhone has no extra-large widget. `systemExtraLarge` is iPad only and out of scope; do not add it to `supportedFamilies`.

## 3. States (board w2-states; every size follows the same rules)
Priority (first match wins): no accounts, needs sign in, in progress, next event, done for today, nothing planned. Stale and Private are overlays on the last four.
1. **Nothing planned** (no events today or tomorrow): header "TUE 13"; title 19 Medium "Nothing planned"; line 14 dim "No events today or tomorrow."; bottom hairline row mono 10 "+ NEW EVENT" (link `calendar://new`).
2. **Done for today** (nothing left today, something tomorrow): header "TUE 13" + "DONE"; title 17 Medium "Done for today"; below, `NEXT · TOMORROW` and the first two or three tomorrow rows. Small widget: title 19 Medium "Done for today", line 14 dim "Next: Standup, tomorrow 09:00".
3. **In progress**: the bright block pinned ("22:00 LEFT · MEET", Join); below it a 4 pt rounded progress line (track `wl`, fill ink, inset 16; amber is reserved for needs you); `THEN` and the next two rows.
4. **Account needs sign in**: saved rows stay; the first slot is an **amber block** (44 pt, r 16, inset 8, ink text "Work needs you to sign in", sub mono 10 "SAVED EVENTS BELOW", a black Sign in button 28 pt, r 8); header right "SAVED". Tap opens Accounts. Small: title 17 Medium "Showing saved events", amber tag "Work needs sign in".
5. **Offline or stale** (last sync older than 6 h, `TimelineBuilder.staleAfter`): header right dim "SAVED 7 H AGO", no block, countdown or status words. Not amber.
6. **Loading** (`placeholder(in:)`, gallery): the real row grid with `wl` blocks (r 2: 3 x 14 bar, 43 pt time, title 90 to 170 pt). No shimmer.
7. **Private** (locked, `.privacySensitive()`): bars and times stay, titles become 8 pt `pl` blocks, header right "LOCKED". In code use `.privacySensitive()` on title and place views, and `redactionReasons.contains(.privacy)` to draw the blocks.
8. **No accounts**: title 19 Medium "Connect an account", line 14 dim "Tap to open Calendar" (unchanged from v1).

## 4. Rendering modes (board w2-modes)
- **Full colour**: as drawn.
- **Accented** (`widgetRenderingMode == .accented`): the system flattens everything to white and one tint. Mark `.widgetAccentable()` on the block, account bars, tags and the now line; titles and times stay primary, secondary text uses `.secondary`. Remove `containerBackground` colour (use `.containerBackground(for: .widget) { Color.clear }`). The block takes the tint as its fill with dark ink; flip cells render as 1 pt outlined cells, no dark fill. Account hue is lost, so the title and time carry meaning; the amber tag becomes the tint.
- **Vibrant** (lock screen, StandBy): one tone. The block becomes a quiet lighter fill (no hue), Join inverts (primary fill), bars become white and the needs-you tag is a filled tag in primary; status words stay mono caps. Never rely on colour alone: MOVED, OVERLAP, REPLY are words.
Implement with `@Environment(\.widgetRenderingMode)`; one view tree, three small modifiers in `SurfacesUI`.

## 5. Lock screen accessories (board w2-lock, rev 2)
The system renders accessories in one vibrant tint, so the design is **monochrome and text-led**: hierarchy comes from size and weight, never from an account colour or a vertical/horizontal colour bar (removed, owner comment on W2.6). Two small glyphs are allowed: a 11 pt clock glyph (ring with two hands) and a progress ring.
- **Circular 76 x 76** (`AccessoryCircularView`): ring (4 pt wide, 4 pt inset) = elapsed share of the 30 min before the start, then of the event; centre mono 26 Medium minutes ("8"), mono 9 caps "MIN" (or hours "2 H" past 60 min; "LEFT" when running). `Gauge` with `.gaugeStyle(.accessoryCircularCapacity)`. Second circular: events left today ("6 LEFT"), ring = share left.
- **Rectangular 172 x 76** (`AccessoryRectangularView`): inset 12. Line 1 (y10): clock glyph + mono 11 Medium "10:00", right-aligned mono 10 dim "IN 8 MIN" / "22 MIN LEFT" (running: "NOW" replaces the time). Line 2 (y28): title 17 Medium, one line. Line 3 (y52): 12 Regular dim, place or "Google Meet". States: done ("TUE 13", "6 DONE", "Done for today", "Next 09:00 Standup"), needs sign in (clock glyph with a dot, "ACCOUNT", "Sign in to Work", "Saved events shown"), nothing planned ("TUE 13", "Nothing planned", "No events today").
- **Inline** (`AccessoryInlineView`): glyph + one line, about 30 characters: "10:00 Roadmap review · 8 min"; drop the countdown first, truncate the title second. Done: "Done for today". Needs sign in: "Calendar: sign in".
- Redaction: with `.privacySensitive()` titles become "Event" and places are hidden while locked; time and countdown stay. The intent option "Show titles on lock screen" (default off) lifts the redaction for that widget.

## 6. Notifications (boards w2-n-*)
iOS draws the banner shell, icon, time, stack and grouping chrome. We supply content and categories. **Banner text is the system font**; GT America appears only in the thumbnail, the long-look view and widgets.

Copy budgets (visible characters; the system truncates by width, so the news goes in the first 30 characters of the title): title 40, subtitle 40, body 90 (about two lines), morning summary body 3 lines of 42, action titles 14 (verb first), thumbnail 144 x 144 px PNG (48 pt at 3x), glyph kept 24 px from the edge because the system rounds the image.
Thumbnail (`UNNotificationAttachment`, generated into the temp dir by the app for local notifications and by the NSE for remote ones, using the same drawing code as `FlipCells`): square account bright colour field, centred flip-cell start time (mono Medium ~ 11 pt at 48 pt). Variants: grey and dimmed (cancelled), amber field (overlap), amber square on black (sign in), no thumbnail (summary, so its body gets the full width).
Threads (`threadIdentifier`): `event.<occurrenceId>` for reminder, starts-now, moved, changed; `day.<yyyy-MM-dd>` for all-day and overlap; `invite.<eventId>`; `accounts`; `summary`. `summaryArgument` = event title, `summaryArgumentCount` = 1. A new alert for an event reuses the request identifier so it replaces the old one (8 min reminder replaced by starts-now).

Interruption levels: **time sensitive**: starts-now for any event with a link or place; moved or cancelled when the old or new start is within 60 min. **Active**: reminder before an event (including the one with a link; this changes v1, which made link reminders time sensitive), all-day eve reminder, later moves and changes, overlap, invitation, sign in, morning summary. **Passive**: any type the user switched off in Settings (the NSE sets `.passive` and removes the sound). Add `relevanceScore` 1.0 for starts-now, 0.7 reminders, 0.3 summary. The app needs the Time Sensitive Notifications entitlement.

Cases (exact copy; sample values shown):
| Case | Title | Subtitle | Body | Actions (category) |
|---|---|---|---|---|
| Reminder, link | Roadmap review in 8 min | 10:00 · Google Meet | Work calendar. Tap Join to open Google Meet. | Join (foreground), Snooze 10 min, Open (`calendar.reminder.join`) |
| Reminder, in person | Lunch with Sam at 12:30 | Kōya · leave by 12:16 | 14 min walk from where you are. | Snooze 10 min, Open (`calendar.reminder`) |
| Reminder, no travel info | Lunch with Sam at 12:30 | Kōya | (none) | same |
| All-day, 09:00 the day before | Tomorrow: Ana's birthday | All day · Personal | Also tomorrow: Standup at 09:00. | Open (`calendar.reminder`) |
| Starting now | Roadmap review starts now | Google Meet | 10:00 to 10:30 | Join, Snooze 10 min, Open (`calendar.reminder.join`) |
| Moved | Design review moved to 14:30 | Was 11:00 · Work | Today, 3 h 30 min later. | View, OK (`calendar.event.moved`) |
| Cancelled | Gym at 17:30 cancelled | Home | Removed from today. | none (`calendar.event`) |
| Changed (place, link, title) | Lunch with Sam: place added | 12:30 · Kōya | 14 min walk. Leave by 12:16. | View, OK |
| Overlap heads-up | Overlap at 15:00 | Design review and 1:1 Maya | Both run from 15:00 to 15:30. Move one? | Open day, Dismiss (`calendar.conflict`) |
| Invitation | Sam invited you to Dinner | Tue 13 Oct, 19:00 | You are free then. Reply from here. | Accept, Maybe, Decline (`calendar.invite`) |
| Sign in | Work needs you to sign in | (none) | Its events are paused until you do. | Sign in, Later (`calendar.account.reauth`) |
| Secret expiry | Server secret expires in 12 days | (none) | Renew the Microsoft client secret on your server. | none |
| Morning summary 07:30 | Today: 7 events, first at 09:00 | (none) | line 1 "First: 09:00 Standup, 10:00 Roadmap review"; line 2 "Design review moved to 14:30"; line 3 "Dinner needs a reply · free 16:00 to 19:00" | Open today, Mute (`calendar.summary`) |
Rules: moved says the new fact first and "Was" in the subtitle; the overlap alert fires once per pair after a move or accept and never for all-day events; if "free from X to Y" cannot be computed the third line is only the reply count; when nothing needs attention the summary has two lines. Snooze is **10 minutes** (v1 was 5): change `NotificationCategory.snoozeSeconds` to 600 and the action title to "Snooze 10 min".

**Long look** (`ios/CalendarNotificationContent`, a `UNNotificationContentExtension`, `UNNotificationExtensionDefaultContentHidden = YES`, `UNNotificationExtensionInitialContentSizeRatio` about 1.0): the frame, header (icon, "CALENDAR", time) and action buttons are the system's. Our view is 366 pt wide, padding 16, hairline rows: mono 10 caps meta line ("TUE 13 · 09:52", "NEXT 3 HOURS"); title row (3 x 42 bar r 2, title 20 Medium, subline 13 dim, countdown mono 20 ink via `Text(timerInterval:)` on the right); mini timeline of the next 3 hours at **44 pt per hour** from now (hour lines `pl`, labels mono 9, amber now line); the notified event as the only bright fill (account bright colour, black text 13 Medium, r 4 because it is far from every corner); other events as `wgev` blocks 12 pt, r 4; for the reminder category a compact **Join** capsule (36 pt high, ink fill, 15 Medium, r 18 = concentric to the r 34 frame at inset 16), trailing in a footer row whose left side reads "Opens Google Meet" in dim 13. Never full width. Data comes from the shared GRDB (App Group). The invitation long look shows the day around the event (17:30 to 20:30) with the new event dashed in amber and "REPLY NEEDED"; Accept, Maybe, Decline are the category's system buttons. If the database cannot be opened the extension shows the title and subtitle from `request.content` only.

**Notification service extension** (`NotificationService`, board w2-n-rules): hub pushes carry `k`, ids and counters only. Rewrite order: (1) saved agenda lookup, (2) hub fetch inside the remaining budget (keep the existing ~25 s cap, but start the hub call only if the agenda lookup misses), (3) on any failure or `serviceExtensionTimeWillExpire`, the generic text. Generic text: events (moved, cancelled, changed, sync): title **"Your calendar changed"**, body "Open Calendar to see what is new."; reauth: "An account needs you to sign in" / "Open Calendar to sign in."; summary: "Your day is ready" / "Open Calendar to see today." Generic and enriched alerts use the same thread id so the enriched one replaces a generic one. Quiet types: `.passive`, no sound. The extension also attaches the thumbnail (download-free, drawn locally) and sets `interruptionLevel`, `relevanceScore` and `categoryIdentifier`.

## 7. Live Activity (board w2-live)
Starts **30 minutes before** an event with a link or place, ends when the event ends (`.after(end + 5 min)` so "Ended 10:30" stays briefly). Presentations:
- **Lock screen / banner**: system card (radius about 26, `activityBackgroundTint` = `wgbg`). Inside, inset **8**: the bright Next block (`AccountColor.bright`, **r 18 = 26 - 8**, height 60, padding 8 so content sits at 16) with flip cells 22 pt (cell 36, 66 wide), title 20 Medium (1 line), line mono 12 caps "IN 7:52 · GOOGLE MEET" (before) or "22:00 LEFT · MEET" (running), and a **compact Join** trailing on the same row (36 high, **r 10 = 18 - 8**, ink fill `#0A0A0B`, `#F4F1EA` 15 Medium, a `Link`). Not full width. Below, a 4 pt rounded progress line inset 16 (track `wl`, fill ink; share of the 30 min lead, then of the event). Ended: no bright block, a plain row (bar, title 18 Medium, "Ended 10:30", mono "DONE"). Maximum height 160 pt.
- **Dynamic Island** (shell system-drawn, black): compact leading = 3 x 16 rounded bar + mono 12 Medium "10:00"; compact trailing = ink mono 12 timer; minimal = 9 pt round dot in the account colour (amber when the event needs you); expanded (about 371 x 132, radius about 44): leading bar + flip cells 18 pt, trailing mono 20 timer, below left title 17 Medium + dim 13 "Google Meet", and on the title row, trailing at inset 24, a **72 x 36 capsule Join** (account bright fill, black text). No full-width bar. If no link: no Join; show the place and travel hint instead.
The `Text(timerInterval:)` ticks without updates; push or local updates happen only on changes (move, cancel, end).

## 8. Data from the shared GRDB (App Group)
Widgets, accessories, the content extension and the NSE read the saved agenda only (never the network, except the NSE fallback fetch). Needed per occurrence: `id`, `eventId`, `title`, `start`, `end`, `allDay`, `status` (confirmed, cancelled), `myResponse` (accepted, tentative, declined, needsAction), `location`, `joinURL`/`joinKind`, `calendarId` (for the Calendars filter), `accountId` → `AccountColor`, `calendarName`, `prevStart` (kept 24 h for MOVED and "Was 11:00"), overlap computed in `SurfacesCore`. Plus `lastSyncAt`, account `needsSignIn`, and `AlertPrefs`. Travel time is not stored in GRDB: it is computed by the app when it plans a reminder (MapKit ETA, only with permission) and carried in the notification content; widgets show no travel time unless a cached `travelMinutes` exists on the occurrence (optional column, nullable).

## 9. Changes needed in code
`ios/Packages/Surfaces/Sources/SurfacesCore`
- `SurfaceLink.swift`: add `.day(String)` (`calendar://day/<date>`), `.new(start: Date?)` (`calendar://new`); keep `.today` as alias; extend `parse`; app side `onOpenURL` must route both.
- `TimelineBuilder.swift`: `maxRows` 4 → 11; `WidgetRowData` gains `kind` (timed, allDayChip, groupLabel), `statusKind`; `EventState` gains `allDay: [AllDayChip]`, `overflow: Int`, `tomorrow: [WidgetRowData]`, `window` data for Day (start hour, blocks with column index); add ordering rules (hide cancelled, hide declined unless `showDeclined`, pin in-progress); entry cadence (1 min last 15, 5 min last 60); stale rule drops status words; a `WidgetConfig` input (large layout, medium layout, calendar ids, show declined, lock screen titles).
- `WidgetConfig.swift` (new): intent-independent model, plus `CalendarEntity` query helper.
- `SurfaceFormat.swift`: add `moreText(n)`, `leftCompact`, "IN 2 H 38 MIN" and "H:MM" forms; keep minutes helpers.
- `ReminderPlanner.swift`: copy table in section 6 (subtitle with place and leave-by, body travel hint, all-day eve at the configured time, starts-now as its own planned reminder 0 min before with `timeSensitive = true`, link reminders now active); `PlannedReminder` gains `subtitle`, `thumbnail` descriptor, `relevance`; snooze 600 s; categories `calendar.reminder`, `calendar.conflict`, `calendar.invite`, `calendar.event`.
- `NotificationRewriter.swift` / `NotificationEnricher.swift`: new copy (moved "Was 11:00 · Work", changed place/link/title, cancelled), subtitle field on `RewrittenNotification`, thumbnail kind, interruption level and relevance, new `Kind.invite` and `.conflict`, generic text "Your calendar changed".
- `NotificationRouting.swift`: handle new actions (`accept`, `maybe`, `decline` → hub reply, `open-day`, `dismiss`, snooze 10), and routing for `day` and `new`.
- `LiveActivity.swift`: `ContentState` unchanged; add `joinKind` to the island expanded fallback (no Join → place); end policy `.after(end + 5 min)`; `needsYou` flag for the amber dot.
- `ThumbnailRenderer.swift` (new, `SurfacesCore` or `SurfacesUI`): renders the 144 x 144 flip-cell PNG for attachments.
`ios/Packages/Surfaces/Sources/SurfacesUI`
- `WidgetViews.swift`: replace `TodayMediumView` (3 rows) with `MediumListView` (A, block + 3 rows) and `MediumNextListView` (B); add `AgendaLargeView`, `DayLargeView`, `WidgetRow` (inset bar, inset hairline), `NextBlock` (concentric block + Join, shared by small, medium, large, Live Activity), `AllDayLine`, `OverflowLabel`; update `NextSmallView` (tag, travel line, timer text), `DateSmallView` (LEFT TODAY), accessory views per section 5, privacy/redaction handling, state cards for the 8 states, accented and vibrant modifiers. Every clip uses `ContainerRelativeShape`.
- `MeetingLiveViews.swift`: lock card with the concentric bright block and compact Join, ended row; island regions per section 7.
- `WidgetGallery.swift`: cases for all sizes, states, modes and light/dark.
- `NotificationContentView.swift` (new): the long-look SwiftUI view (mini timeline, Join, invitation variant), used by the content extension.
`ios/CalendarWidgets`
- `CalendarWidgets.swift`: switch `NextEventWidget`, `TodayWidget`, `DateWidget` to `AppIntentConfiguration`/`CalendarWidgetIntent`; add `.systemLarge` to the Today widget (kind `com.yourname.calendar.today`, families medium and large); wire `widgetRenderingMode`; remove container colour in accented mode; keep `systemExtraLarge` out. Add `CalendarWidgetIntent.swift` (new). Info.plist unchanged; entitlements keep the App Group.
`ios/CalendarNotificationService`
- `NotificationService.swift`: thumbnail attachment, subtitle, interruption level and relevance, generic text via `NotificationRewriter.generic`, agenda lookup before the hub call.
`ios/CalendarNotificationContent` (new target)
- Info.plist (`UNNotificationExtensionCategory` = the five categories, default content hidden), `NotificationViewController.swift` hosting `NotificationContentView`, App Group entitlement.
`ios/CalendarApp` (outside this spec's lanes, listed for the coordinator): Time Sensitive Notifications entitlement; register the new categories at launch; `onOpenURL` for `day` and `new`; reply actions call the hub.

## 10. Done means (Swift side)
- [ ] Medium shows the block and 3 rows and large the block and 9 rows on the sample day exactly as `w2-medium` A and `w2-large` Agenda, dark and light, with `+N MORE`, the all-day line and tomorrow fill; unit tests on `TimelineBuilder` for ordering (all-day, in-progress pinned, cancelled hidden, declined hidden/faded), overflow, entry cadence, stale.
- [ ] Day-large and Next+list variants render from the intent option; gallery snapshots for every size x state x {full, accented, vibrant} x {dark, light}.
- [ ] No hardcoded corner radius anywhere in `Surfaces` or the extensions (`grep cornerRadius` is empty or derives from `ContainerRelativeShape` minus the inset: block 8, control 8 more); content inset 16; columns on x16 / x27 / x78 / x322 on medium and large (a snapshot test asserts the title x and the trailing edge).
- [ ] Countdown uses `Text(timerInterval:)` for single-event surfaces within 60 min and words in rows; stale drops them.
- [ ] Lock accessories render circular, rectangular, inline in all states, monochrome with no bar; locked state redacts titles (verified on device or simulator with privacy on).
- [ ] All 13 notification cases produce the exact copy of section 6 within the 40/40/90 budgets (a unit test asserts lengths); thumbnails attached; categories registered; Snooze is 10 min; starts-now is time sensitive, link reminders are active.
- [ ] Long look shows the mini timeline and a compact Join capsule on a real device or simulator; invitation long look with three replies; reply actions reach the hub.
- [ ] NSE returns "Your calendar changed" on timeout and offline, replaces it with the enriched text, and sets passive for switched-off types.
- [ ] Live Activity lock card and island (compact, expanded, minimal, ended) match `w2-live`: concentric block, compact Join (never full width); ends 5 min after the event.
- [ ] Deep links `calendar://event/<id>`, `day/<date>`, `new`, `accounts` open the right screen from widget, banner and activity.
- [ ] VoiceOver reads each widget row and banner with spoken statuses; widget and banner labels reviewed.

## 11. Open questions for the user
1. Medium default: list of five (A, recommended) or next block plus list (B)?
2. Large default: Agenda (recommended) or Day?
3. Link reminders become Active and only "starts now" is Time Sensitive (v1 made link reminders time sensitive). OK?
4. Snooze 10 min replaces 5 min.
5. Overlap heads-up and invitation banners are new alerts. Both on by default?
6. Travel time and "leave by": needs location permission and MapKit; fine, or keep the place only?
7. Lock screen: titles hidden until unlock by default (recommended), or shown?
8. Widgets adapt to light and dark (decision Q8 kept) rather than staying dark.
