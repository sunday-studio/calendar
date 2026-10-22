# Day, 3 days, Week (boards v-day, v-day2, v-dayempty, v-three, v-week)

Notes from the canvas: *Hatched past, amber now line, overlaps side by side. 3.3.* · *Second time zone: two hour columns, offset shown once, not per row. 6.5.* · *3 days: the main place to move events on a phone.* · *Week: for reading, tap a day to open it.* · *Empty day: plain line and a hint to tap and hold an hour.*

## Frame
1. **Header** `.hd` (same as Home): padding-top 68, side 24. Title 22 Medium + chevron (down; up when the month grid is open): Day = **"Sun 11 Oct"**, 3 days / Week = **"October 2026"**. Trailing `TodayButton` (38 square, amber mono date).
2. **Switch** ★`SegmentedControl` (`.sg`): padding 14/24/0; one 1pt `border` box, 4 equal segments h40, 15pt dim, selected = ink fill with bg text. Items: Agenda · Day · 3 days · Week. *(Home boards h-* do not draw it; see DECISIONS Q7: default is to show it on Home too.)*
3. **All-day strip** (Day only): a `.r.t1` row h44, margin-top 14: bar top/bottom 10, "All day" mono 15 dim in a 72pt column (no wrap), title 16. One row per all-day event.
4. **Timeline** ★`TimelineView`, margin-top 14 (20 on the empty day): hour rows `.hr` **64pt** each, 1pt `line` top border, hour label mono 11 Regular dim ("09:00") at x=16, y=−9, on a `bg` plate with 0/4 padding. Default visible window 09:00–19:00; scroll covers 00:00–24:00; first open scrolls so *now* sits ~1/4 down (today) or 08:00 at the top (other days).
5. **FAB** (`Fab`), bottom 28 trailing 24.

## Event block ★`TimelineEvent` (`.ev`)
- `top = (start − gridStart) × 64 + 2`, `height = duration × 64 − 2` (min 30 for 30 minutes; shorter events keep 30 and show only the title).
- Fill `surface`; **3pt left border in the account colour**; padding 6/10; title 14 Regular ink, line-height 1.25, 1 line when height < 46 else up to 2; second line 12 dim: start time, plus " · moved" / " · reply needed" when a status applies (same words as the agenda, lowercased). No rounded corners, no outline.
- Day view: x = 64 (gutter), width = 310 (to 16 from the right edge). **Overlaps share the width in lanes** with a 6pt gap: two lanes = 152 each at x=64 and x=222; three lanes = 98 each.
- States: **declined** = 50% opacity + strikethrough; **maybe/tentative** (`.tn`) = diagonal hatch fill (135°, 1pt `line` every 6pt) instead of `surface`; **cancelled** hidden unless "Show declined events" is on (Appearance); **lifted** (drag, D5) = 1pt ink outline; **ghost** (old slot while dragging) = 1pt dashed `border`, no fill.
- Tap → Event detail. Long-press (0.4 s) on an empty hour → New event prefilled with that hour (empty-day hint says so); long-press on an event lifts it (D5).

## Past and now
- Past = `.past`: hatch (1pt `line` lines every 7pt, 135°, opacity .8) over the full width from the top of the grid to the now line (today only).
- Now = `.now`: 1pt amber line across the full width with ★ label chip at x=8 (mono Medium 11, `#0A0A0B` on amber, 0/5 padding, height 18, centred on the line). Updates every minute; no animation.
- 3 days / Week draw the now line and the past hatch **only in today's column**.

## Second time zone (v-day2) — Settings › Time zones
- Above the grid, mono 11 +.1em caps dim labels, **each over its own hour column** (x=16 and x=62): use **short labels of at most 4 characters** — the time-zone abbreviation ("CEST", "EDT") or a 3-letter city code ("AMS", "NYC") — because the two hour columns are only 46pt apart (the canvas "ACCRA / NEW YORK" overflows). Padding 14/16/8.
- Main hours at x=16 (as above); second-zone hours at **x=62 in `faint`** ("05:00"). Event x shifts to **116**, width 258 (two lanes 126 + 6 gap).
- An event that only matters in the other zone adds a 12pt dim second line: "09:00 for Ana in New York". Offsets shown once (the column header), never per event.

## 3 days (v-three)
- Header row `.cols`: grid 40pt gutter + 3 equal columns; each `.ch` h52: day number 17 Medium ink (**amber for today**), weekday 12 dim ("Sun") centred.
- Event width = `(width − 40)/3 − 4`, x offset 2; overlaps split that width into lanes with 3pt gap (≈ 49 each for two). Titles 14 truncate to one line when narrow; only the title is shown.
- Gutter hour labels: mono 11 at x=2 (canvas draws them at x=16 and lets the plate cover the column edge; use x=2 to avoid clipping).
- This is the main drag surface (D5).

## Week (v-week)
- Gutter 32; 7 columns; header letters M T W T F S S (Monday first, per Week-starts setting) with numbers 17 Medium; today amber.
- Hour labels two digits ("09") at x=2. Event text 10pt, padding 2/3, title truncated; tap a day header or an event cell opens **Day** for that date. Reading only (no drag).

## Empty day (v-dayempty)
Grid without events. Text block at x=24, y≈300: **"Nothing planned"** 22 Medium, then 16 dim "Tap and hold an empty hour to add an event there." (padding-top 8).

## Loading and error
- First load: hour rows draw immediately; events fade in (150 ms, off for Reduce Motion). No spinner.
- Offline / stale: the `Banner` from the offline spec sits between switch and grid; events from a stale account carry the "stale" word in their second line.

## Data
`DayViewModel` / `DayLogic` already exist (FeatureHome). Needs per event: start/end (civil in the viewing zone), allDay, account colour, status word, title; plus `userTzid` and optional second zone from Settings.

## Components (CalendarUI)
★`SegmentedControl`, ★`TimelineView` (hour grid + gutter + scroll), ★`TimelineEvent`, ★`NowRule` (BrassLine in `.timeline` style), ★`DayHeaderCell`. `HatchedShade` and `BrassLine` already exist.

## Done means
Screenshots of Day (populated, empty, second zone), 3 days, Week in dark and light match boards v-day / v-day2 / v-dayempty / v-three / v-week (`-l` for light) in block positions, 64pt hour pitch, lane widths, hatch and now line.
