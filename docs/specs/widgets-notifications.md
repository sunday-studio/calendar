# Widgets, notifications, Live Activity (v2)

Canvas: page "Widgets and alerts", row **Widgets and notifications v2** (boards `w2-small`, `w2-medium`, `w2-large`, `w2-states`, `w2-modes`, `w2-lock`, `w2-anatomy`, `w2-n-remind`, `w2-n-changes`, `w2-n-other`, `w2-n-long`, `w2-n-rules`, `w2-live`, each with a `-l` light twin). The old boards `w-home` to `w-summary` are superseded and kept only for reference.
Targets: `ios/CalendarWidgets`, `ios/CalendarNotificationService`, `ios/CalendarNotificationContent` (new), shared code in `ios/Packages/Surfaces` (`SurfacesCore` logic, `SurfacesUI` views). Stories 6.1, 6.2, 6.4, 7.5.
Sample day on every board: Tuesday 13 October 2026, 09:52. Events: Ana's birthday (all day), Standup 09:00, Roadmap review 10:00 (Meet), Dentist 11:45, Lunch with Sam 12:30 (Kōya), Design review 14:30 (moved from 11:00), 1:1 Maya 15:00, Gym 17:30 (cancelled), Dinner 19:00 (reply needed). Tomorrow: Standup 09:00, Design sync 11:00, Lunch 12:30, 1:1 Maya 15:00, Dinner 19:30.

## 0. The one constraint: the shell is the system's and it is round
iOS draws every widget, lock accessory, banner, long-look frame, Live Activity card and Dynamic Island in a rounded shape. We cannot make it square and we do not imitate it with inner rounded cards. The mocks draw the true rounded container on the wallpaper; inside it the app keeps its language: hairline rows, square colour bars, flip cells, mono caps labels, amber = needs you.

Corner-safe rules (all sizes):
1. **No fill or edge contact near a corner.** Content sits in a box inset **14 pt** from every container edge (accessory rectangular 10, circular 5, Live Activity card 12, Dynamic Island expanded 18). The bare geometric minimum for a square corner inside radius r is 0.29 r (7 pt at r 24); 14 leaves visible margin so the square corner never looks clipped.
2. **Bars are inset.** Account bars are 3 pt wide, start at the content inset, never at the edge. Row hairlines run from inset to inset, not edge to edge.
3. **First and last rows are padded for the radius.** Medium: 10 pt above the header, 10 below the last row. Large: 10 above, 24 below the last row. Small: content 14 from the bottom.
4. **The only fills** are flip cells, the highlighted event in the long look (16 pt in from the frame), the Join button (18 pt in from the island edge) and the bright Next block in the Live Activity (12 pt in from the card). None touch a corner.
5. **Radius is read, not hardcoded.** Use `ContainerRelativeShape()` for any clip or stroke that must follow the container, and `@Environment(\.widgetContentMargins)` where available. Canvas radius is 24 pt for small, medium and large (real devices: about 22 to 26 pt, the system may change it). Banner radius (about 28 on the board), long look (about 34), island expanded (about 44) and Live Activity card (about 26) are system-controlled and only drawn for context. Never write `cornerRadius(24)`.

## 1. Shared rules

Type (GT America, from `CalFont`; `CalFont.font(.mono, ...)` for mono): row title 14 Regular (Medium for the next/in-progress row); row time mono 12; status mono 10 caps tracking +.06em; header label mono 10 caps +.14em dim; small widget title 17 Medium, line-height 19, 2 lines; small widget flip cells 20 pt (cell height 30); small widget countdown mono 13; state-card title 19 Medium, line 14. Widgets never use a colour not in `Theme`.
Colours: container background `wgbg` (dark `#111113`, light `#FBFBF9`, adapts to the system appearance, DECISIONS Q8); bars use the account colour (`AccountColor.base` in the container, `.bright` only on the Live Activity block); text `calInk`, secondary `calInk2` (dim); **amber is only for "needs you / soon"**: next-row status, MOVED, OVERLAP, REPLY, sign in, countdowns. Cancelled is struck through and dim, never amber.

Ordering inside any list (`TimelineBuilder.relevant`, one place):
1. All-day events first, shown as the header chip (medium, large) or a first group row in the agenda only when more than one; never as timed rows.
2. In-progress timed event pinned to the first timed row (shows time left).
3. Timed events by start, ties by end then title.
4. Cancelled: hidden everywhere (the Gym case). Declined by the user: hidden unless "Show declined" is on, then faded to 45 % opacity. "Maybe" and "needs reply" show normally; needs-reply carries the REPLY status.
5. Status word priority per row, one only: time left or minutes to start (next row), MOVED (changed in the last 24 h), OVERLAP, REPLY.

Overflow: never clip a row. Show **`+N MORE`** (mono 10, right end of the header) where N counts the timed events that did not fit and are still to come today. When today is exhausted, tomorrow's events fill the free slots under a `TOMORROW · WED 14` label that takes one slot; `+N MORE` then counts tomorrow's remainder.

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

### Small, 158 x 158, inset 14 (`WidgetNextSmall`)
- Top: 3 x 30 account bar then flip cells 20 pt (start time "10:00"), 8 pt gap, at (14, 14).
- Title: top 56, 17 Medium, 2 lines max.
- Bottom (bottom 14): line 1 amber mono 13 countdown ("IN 7:52" / "22:00 LEFT" / "IN 2 H 38 MIN"), on the right a `zkn` tag "MEET" (mono 9 caps, 1 pt border) when the event has a video link; line 2 (in person) 12 Regular dim "Kōya · 14 min walk" (travel only with location permission; otherwise the place alone).
- One tap target: the event. Date variant: mono 13 "TUESDAY", day number 36 Regular, bottom mono 11 "6 LEFT TODAY" (cancelled and declined excluded); taps open `calendar://day/<today>`.

### Medium, 338 x 158, inset 14: layout A (default, recommended) and B
**A, list of five** (`WidgetTodayMedium`): header 14 pt at y 10 (mono 10 caps: "TUE 13", all-day chip, `+N MORE` right); rows start at y 28; **5 rows x 24 pt** = 120; bottom 10. Row: 3 x 14 bar, 8 gap, time mono 12 in a 36 pt column, title 14 (1 line, truncates), status mono 10 amber right. Hairline `line` between rows inset 14. The all-day chip: 14 pt tall, mono 9 "ALL DAY" + 3 x 12 bar + title 11 Regular, 1 pt `pl` border; several all-day events collapse to "ALL DAY · 3".
**B, next block + list** (option): left 126 pt: mono 10 "NEXT"; bar + flip cells 20 pt at y 32; title 15 Medium 2 lines at y 70; bottom countdown mono 13 + MEET tag. 1 pt divider at x 146. Right x 158 to 324: header, then four 24 pt rows without status words (a changed row colours its time amber instead).
Recommendation: **A is the default** because the request was to fit more (five events against three, and the all-day line is free); B is for people who want the one big countdown, picked in the widget settings.

### Large, 338 x 354, inset 14, default **Agenda**, option **Day**
**Agenda** (`WidgetAgendaLarge`): header (y 10, 14 pt) as medium; group label `TODAY` (24 pt slot, no hairline) then rows; `TOMORROW · WED 14` label then rows; **11 timed rows x 24 pt** (6 today + 5 tomorrow on the sample) plus two group labels and the header = 330 pt, 24 pt left at the bottom for the radius. If today has more than the available slots, tomorrow shrinks first, then `+N MORE`.
**Day** (`WidgetDayLarge`): timeline left x 14 to 218 (204 pt: 22 pt hour column, 182 pt event area), **30 pt per hour**, window 10 h starting at the current hour (09:00 to 19:00 at 09:52); hour lines 1 pt `line`, hour labels mono 9. Events: square block `wgev`, 3 pt account bar, title 11 (one line), time as a second 10 pt line only when the block is at least 30 pt tall. Overlaps split the column evenly (max 3). Past time hatched (135 degrees, 1 pt every 6 pt), amber 1 pt now line with a mono 9 label on a filled amber chip. Right column x 232 to 324 (92 pt): `UP NEXT` mono 10, then up to six 46 pt items (mono 10 time + amber status, title 12 up to one line); it lists what the window cannot show (Dinner 19:00). Taps: each block and item is a `Link`; empty space opens the day.
Recommendation: **Agenda is the default** (reads faster, same visual language as Home, no layout maths); Day is the configurable option for people who plan by free gaps.
- iPhone has no extra-large widget. `systemExtraLarge` is iPad only and out of scope; do not add it to `supportedFamilies`.

## 3. States (board w2-states; every size follows the same rules)
Priority (first match wins): no accounts, needs sign in, in progress, next event, done for today, nothing planned. Stale and Private are overlays on the last four.
1. **Nothing planned** (no events today or tomorrow): header "TUE 13"; title 19 Medium "Nothing planned"; line 14 dim "No events today or tomorrow."; bottom hairline row mono 10 "+ NEW EVENT" (link `calendar://new`).
2. **Done for today** (nothing left today, something tomorrow): header "TUE 13" + "DONE"; title 17 Medium "Done for today"; below, `NEXT · TOMORROW` and the first two or three tomorrow rows. Small widget: title 19 Medium "Done for today", line 14 dim "Next: Standup, tomorrow 09:00".
3. **In progress**: header "NOW" + amber "22:00 LEFT" (timer); event title 19 Medium with 22 pt bar; 3 pt progress line (track `line`, fill amber, inset 14); `THEN` and the next two rows.
4. **Account needs sign in**: saved rows stay; first slot is an amber line (7 pt amber square, mono 10 "WORK NEEDS SIGN IN · OPEN CALENDAR"); header right "SAVED". Tap opens Accounts. Small: title 17 Medium "Showing saved events", amber 14 "Work needs sign in. Open Calendar."
5. **Offline or stale** (last sync older than 6 h, `TimelineBuilder.staleAfter`): header right dim "SAVED 7 H AGO", no countdown or status words. Not amber.
6. **Loading** (`placeholder(in:)`, gallery): the real row grid with square `pl` blocks (3 x 14 bar, 32 pt time, title 90 to 170 pt). No shimmer.
7. **Private** (locked, `.privacySensitive()`): bars and times stay, titles become 8 pt `pl` blocks, header right "LOCKED". In code use `.privacySensitive()` on title and place views, and `redactionReasons.contains(.privacy)` to draw the blocks.
8. **No accounts**: title 19 Medium "Connect an account", line 14 dim "Tap to open Calendar" (unchanged from v1).

## 4. Rendering modes (board w2-modes)
- **Full colour**: as drawn.
- **Accented** (`widgetRenderingMode == .accented`): the system flattens everything to white and one tint. Mark `.widgetAccentable()` on the account bar, countdown, status words, now line and flip-cell outline; titles and times stay primary, secondary text uses `.secondary`. Remove `containerBackground` colour (use `.containerBackground(for: .widget) { Color.clear }` plus `.widgetAccentedRenderingMode(.fullColor)` only for none). Flip cells render as 1 pt outlined cells in the tint, no dark fill. Account hue is lost, so the title and time must carry meaning; amber becomes the tint.
- **Vibrant** (lock screen, StandBy): one tone. Bars become white; status words stay mono caps and get weight 500; the next row stays Medium. Never rely on colour alone: MOVED, OVERLAP, REPLY are words.
Implement with `@Environment(\.widgetRenderingMode)`; one view tree, three small modifiers in `SurfacesUI`.

## 5. Lock screen accessories (board w2-lock)
- **Circular 76 x 76** (`AccessoryCircularView`): ring (4 pt, 5 pt inset) = elapsed share of the 30 min before the start, then of the event; centre mono 22 Medium minutes ("8"), mono 8 caps "MIN" (or hours "2 H" past 60 min; "LEFT" when running). `Gauge` with `.gaugeStyle(.accessoryCircularCapacity)`. Second circular: events left today ("6 LEFT").
- **Rectangular 172 x 76** (`AccessoryRectangularView`): 3 x 56 bar (inset 10), line 1 mono 10 caps "NEXT · IN 7:52" / "NOW · 22:00 LEFT", line 2 title 16 Medium (1 line), line 3 mono 11 "10:00 · MEET" or the place. States: done ("Done for today", line 3 "NEXT 09:00 STANDUP"), needs sign in ("Work needs sign in", "OPEN CALENDAR"), nothing planned.
- **Inline** (`AccessoryInlineView`): one line, about 30 characters: "10:00 Roadmap review · 8 min"; drop the countdown first, truncate the title second. Done: "Done for today". Needs sign in: "Calendar: sign in".
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

**Long look** (`ios/CalendarNotificationContent`, a `UNNotificationContentExtension`, `UNNotificationExtensionDefaultContentHidden = YES`, `UNNotificationExtensionInitialContentSizeRatio` about 1.0): the frame, header (icon, "CALENDAR", time) and action buttons are the system's. Our view is 366 pt wide, padding 16, square rows: mono 10 caps meta line ("TUE 13 · 09:52", "NEXT 3 HOURS"); title row (3 x 42 bar, title 20 Medium, subline 13 dim, countdown mono 20 amber via `Text(timerInterval:)` on the right); mini timeline of the next 3 hours at **44 pt per hour** from now (hour lines `pl`, labels mono 9, amber now line); the notified event as the only filled block (account bright colour, black text 13 Medium, 16 pt in from the frame); other events as `wgev` blocks 12 pt; for the reminder category a full-width **Join** button 44 pt (ink fill, 16 Medium, square). Data comes from the shared GRDB (App Group). The invitation long look shows the day around the event (17:30 to 20:30) with the new event dashed in amber and "REPLY NEEDED"; Accept, Maybe, Decline are the category's system buttons. If the database cannot be opened the extension shows the title and subtitle from `request.content` only.

**Notification service extension** (`NotificationService`, board w2-n-rules): hub pushes carry `k`, ids and counters only. Rewrite order: (1) saved agenda lookup, (2) hub fetch inside the remaining budget (keep the existing ~25 s cap, but start the hub call only if the agenda lookup misses), (3) on any failure or `serviceExtensionTimeWillExpire`, the generic text. Generic text: events (moved, cancelled, changed, sync): title **"Your calendar changed"**, body "Open Calendar to see what is new."; reauth: "An account needs you to sign in" / "Open Calendar to sign in."; summary: "Your day is ready" / "Open Calendar to see today." Generic and enriched alerts use the same thread id so the enriched one replaces a generic one. Quiet types: `.passive`, no sound. The extension also attaches the thumbnail (download-free, drawn locally) and sets `interruptionLevel`, `relevanceScore` and `categoryIdentifier`.

## 7. Live Activity (board w2-live)
Starts **30 minutes before** an event with a link or place, ends when the event ends (`.after(end + 5 min)` so "Ended 10:30" stays briefly). Presentations:
- **Lock screen / banner**: system card (radius about 26, `activityBackgroundTint` = `wgbg`). Inside, inset **12**: bright Next block (account bright colour, square, padding 12 x 14) with flip cells 22 pt (cell 36), title 20 Medium (1 line), line mono 13 "IN 7:52 · Google Meet" (before) or "22:00 LEFT · Google Meet" (running), and a square Join (`#0A0A0B` fill, `#F4F1EA` 15, padding 8 x 18, a `Link`). Below, a 3 pt progress line inset 12 (track `line`, fill amber; share of the 30 min lead, then of the event). Ended: no bright block, a plain row (bar, title 18 Medium, "Ended 10:30", mono "DONE"). Maximum height 160 pt.
- **Dynamic Island** (shell system-drawn, black): compact leading = 3 x 16 bar + mono 12 Medium "10:00"; compact trailing = amber mono 12 timer; minimal = 9 x 9 account square, amber under 10 min; expanded (about 371 x 176, radius about 44): leading bar + flip cells 18 pt, trailing amber mono 20 timer, centre title 17 Medium + dim 13 "Google Meet", bottom a square Join 44 pt tall inset 18 (account bright fill, black text). If no link: no Join; show the place and travel hint instead.
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
- `LiveActivity.swift`: `ContentState` unchanged; add `joinKind` to the island expanded fallback (no Join → place); end policy `.after(end + 5 min)`; amber under 10 min flag.
- `ThumbnailRenderer.swift` (new, `SurfacesCore` or `SurfacesUI`): renders the 144 x 144 flip-cell PNG for attachments.
`ios/Packages/Surfaces/Sources/SurfacesUI`
- `WidgetViews.swift`: replace `TodayMediumView` (3 rows) with `MediumListView` (A, 5 rows) and `MediumNextListView` (B); add `AgendaLargeView`, `DayLargeView`, `WidgetRow` (inset bar, inset hairline), `AllDayChip`, `OverflowLabel`; update `NextSmallView` (tag, travel line, timer text), `DateSmallView` (LEFT TODAY), accessory views per section 5, privacy/redaction handling, state cards for the 8 states, accented and vibrant modifiers. Every clip uses `ContainerRelativeShape`.
- `MeetingLiveViews.swift`: lock card with inset bright block and ended row; island regions per section 7.
- `WidgetGallery.swift`: cases for all sizes, states, modes and light/dark.
- `NotificationContentView.swift` (new): the long-look SwiftUI view (mini timeline, Join, invitation variant), used by the content extension.
`ios/CalendarWidgets`
- `CalendarWidgets.swift`: switch `NextEventWidget`, `TodayWidget`, `DateWidget` to `AppIntentConfiguration`/`CalendarWidgetIntent`; add `.systemLarge` to the Today widget (kind `dev.casprine.calendar.today`, families medium and large); wire `widgetRenderingMode`; remove container colour in accented mode; keep `systemExtraLarge` out. Add `CalendarWidgetIntent.swift` (new). Info.plist unchanged; entitlements keep the App Group.
`ios/CalendarNotificationService`
- `NotificationService.swift`: thumbnail attachment, subtitle, interruption level and relevance, generic text via `NotificationRewriter.generic`, agenda lookup before the hub call.
`ios/CalendarNotificationContent` (new target)
- Info.plist (`UNNotificationExtensionCategory` = the five categories, default content hidden), `NotificationViewController.swift` hosting `NotificationContentView`, App Group entitlement.
`ios/CalendarApp` (outside this spec's lanes, listed for the coordinator): Time Sensitive Notifications entitlement; register the new categories at launch; `onOpenURL` for `day` and `new`; reply actions call the hub.

## 10. Done means (Swift side)
- [ ] Medium shows 5 rows and large 11 on the sample day exactly as `w2-medium` A and `w2-large` Agenda, dark and light, with `+N MORE`, the all-day chip and tomorrow fill; unit tests on `TimelineBuilder` for ordering (all-day, in-progress pinned, cancelled hidden, declined hidden/faded), overflow, entry cadence, stale.
- [ ] Day-large and Next+list variants render from the intent option; gallery snapshots for every size x state x {full, accented, vibrant} x {dark, light}.
- [ ] No hardcoded corner radius anywhere in `Surfaces` or the extensions (`grep cornerRadius` is empty or uses `ContainerRelativeShape`); nothing filled within 14 pt of a corner.
- [ ] Countdown uses `Text(timerInterval:)` for single-event surfaces within 60 min and words in rows; stale drops them.
- [ ] Lock accessories render circular, rectangular, inline in all states; locked state redacts titles (verified on device or simulator with privacy on).
- [ ] All 13 notification cases produce the exact copy of section 6 within the 40/40/90 budgets (a unit test asserts lengths); thumbnails attached; categories registered; Snooze is 10 min; starts-now is time sensitive, link reminders are active.
- [ ] Long look shows the mini timeline and Join on a real device or simulator; invitation long look with three replies; reply actions reach the hub.
- [ ] NSE returns "Your calendar changed" on timeout and offline, replaces it with the enriched text, and sets passive for switched-off types.
- [ ] Live Activity lock card and island (compact, expanded, minimal, ended) match `w2-live`; ends 5 min after the event.
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
