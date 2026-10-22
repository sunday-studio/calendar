# Settings (boards g-home … g-week)

Notes: *Grouped, plain rows. Avatar or gear opens Settings (entry point still open: DECISIONS Q5). Accounts and calendars: show, hide, colour, default. Time: zones, working hours, week start. Alerts decide what is scheduled on the phone. Server and sync leads to the status screens.* Stories 4.1–4.3, 6.2, 6.5, 7.4.

## Shared row ★`ToggleRow` / `NavRow` (`.tg`)
Min h60, padding 10/24, 1pt `line` underline (first row of a group also has a top hairline), gap 16. Title 18 Regular; optional second line 13 dim. Trailing (`.v`): value 16 dim + chevron-right glyph (`CalGlyph(.forward)` 9pt, dim) for navigation rows; a ★`CalSwitch` for toggles; `StatusLabel` + chevron for server health.
Group labels use the mono-caps `label` style with padding 32/24/10 (first group on a screen: top padding 12).
**★`CalSwitch` (`.sw`)**: 46×26, square, 1pt `border`; thumb 18×18 square `ink2` at x=3,y=3. On: fill `ink`, border `ink`, thumb `bg` at x=23. Animates 150 ms (instant for Reduce Motion). Hit area = the whole row (≥ 44pt).
Radio list rows use the sheet row `.li`/`.rd` (see event-detail).

## Screens
| Board | Nav title | Content (exact copy) |
|---|---|---|
| **g-home** | Settings | **Calendars**: "Accounts · 3 connected" › · "Calendars · Show, hide, colour" › · "Default calendar" value "Personal" › — **Time**: "Time zones" value "Accra · New York" › · "Working hours" "09:00 to 18:00" › · "Week starts" "Monday" › — **Alerts and look**: "Alerts" "10 min" › · "Appearance" "System" › — **Server**: "Server and sync · hub.tail3a9f.ts.net" `StatusLabel(ok,"OK")` › (becomes `bad` "Sign in"/"Offline" when something needs attention) |
| **g-cals** | Calendars | Per account: label = account email, rows with 14×14 account-colour square, name (+ 13 dim "Default" on the default calendar), `CalSwitch`. Group "Read only links": dim-grey square, "Work, read only · Updates slowly". Hint 13 dim: "Hiding a calendar only hides it here. Tap a name to change its colour." Tapping the name → g-colour. Optimistic toggle with rollback + toast on failure (CalendarsViewModel exists) |
| **g-colour** | calendar name | sub 16 dim account email; label "Colour"; **eight** 44×44 squares gap 10 (wrap), selected = 2pt `bg` gap + 1pt `ink` ring, in `AccountColor.allCases` order; hint "By default a calendar uses its account's colour. Pick another to tell it apart."; label "Preview" + one live `EventRow` ("18:00 Dinner with Ana"); `ToggleRow` "Use the account colour" |
| **g-default** | Default calendar | sub "New events go here unless you pick another."; per account label + radio rows with colour square; hint "Read only calendars are not listed here." |
| **g-tz** | Time zones | **Main**: "Accra · Follows this phone" value "GMT"; **Second column**: toggle "Show a second time zone"; row "New York · 4 hours behind" value "Change" ›; **Preview**: a two-line row "10:30 Roadmap review / 06:30 in New York"; hint "The second zone appears in Day and 3 day views, and as a small line in event details." |
| **g-alerts** | Alerts | **Before an event** radios: At the start · 5 minutes · 10 minutes · 30 minutes; **Also tell me** toggles: "Join button on the alert · When an event has a call link", "When an event moves or is cancelled", "When an account needs sign in", "New invitations" (default off); hint "Alerts are scheduled on this phone from your saved agenda, so they still arrive offline." |
| **g-work** | Working hours | sub "Used when you ask alfather to find free time."; label "Days": seven ★`ChipToggle` (`.tag` look: mono 11 caps, 1pt border, pad 6/12; on = ink fill with bg text) Mon…Sun; label "Hours": rows "From" `FlipCells(.small)` "09:00", "To" "18:00", "Lunch break · Treated as busy" "12:30" (tap opens a time wheel; ±15 min steps); hint "Free time suggestions stay inside these hours." |
| **g-look** | Appearance | **Theme** `SegmentedControl` System · Dark · Light; **Flip cells**: "Animate changes · Cells flip when a time changes" (default on), "Reduce motion · Follows the phone setting" (reflects, not overrides, the OS flag); **Agenda**: "Show declined events", "Show week numbers", "24 hour time" (default on) |
| **g-week** | Week starts | radios Monday · Sunday · Saturday; label "Preview" with a weekday header row (Mon…Sun re-ordered live); hint "Changes the month grid, the week view and working days." |

## Behaviour
- Changes save on interaction (no Save button), except the server-validated working hours which validate on leaving the screen (SettingsViewModel has draft/validate; map hub field errors to the field's `hint` in red).
- Theme change applies immediately via `preferredColorScheme`; "Animate changes" gates `FlipCells` animation app-wide (`@AppStorage`/Settings).
- 24-hour time off → times render "2:30 PM" in the same mono 15 (agenda column widens to 56pt).
- Accessibility: switches expose "on/off"; radios expose selected; the Preview rows are decorative (hidden from VoiceOver).

## Components (CalendarUI)
★`CalSwitch`, ★`ToggleRow`/`NavRow`, ★`ChipToggle`, ★`SheetRow`/`RadioMark` (shared with event-detail), ★`ColourSwatchRow`; existing `SegmentedControl` (★ from day-view), `FlipCells`, `ScreenNav`, `StatusLabel`.

## Done means
All nine screens in dark and light match the boards in row heights (60), switch geometry (46×26 square), copy, and ordering.
