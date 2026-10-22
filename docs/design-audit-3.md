# Design audit 3: Day, 3 days, Week, Event detail, Actions, Delete

_Design lane · conformance pass 2 · source of truth: Gnomon canvas boards v-day, v-day2, v-three, v-week, d-nolink, d-actions, d-del and `docs/specs/{day-view,event-detail}.md`._
Built from the working tree on iPhone 17 (own simulator), fixture hub `--fixtures-state=normal`. Captured: Day (dark + light, second time zone on), 3 days (dark), Week (dark), Detail in person (dark + light), Actions sheet (dark), Delete sheet (dark). Shots: `docs/audit-shots/r2/{v1,v2,v3,d1,d2,d3}-*.png`.
**Not captured** (no fixture data or not reached): People (event without guests), repeating/read-only/video-call detail, join sheet, move sheet, deleted toast, delete-repeating sheet, 3 days/Week light, drag/resize.

## Verdict
Strong: the timeline, Next-style join bar, key/value rows, sheets and tokens all read as the canvas, in both themes. Remaining issues are one **copy bug**, 3-day **title wrapping** (CalendarUI option already shipped), a few **missing rows/labels**, and spacing items. No structural rework needed.

## Ranked deltas

### S1
| # | Screen | Delta | Fix (owner) |
|---|---|---|---|
| 1 | Delete sheet | Message reads **"It is removed from your  calendar in Google."** — the calendar name is missing (double space). Canvas: "It is removed from your **Personal** calendar in Google. Sam gets a cancellation." (second sentence only with guests). | Fill `calendarName` (fall back to omitting the word, never an empty slot); add the guest sentence when `attendees` exist (Swift) |
| 2 | 3 days | Titles **wrap mid-word** ("Portf / olio…", "Clien / t ki…"). Canvas v-three: one line, tail ellipsis, no second line. | `TimelineColumn(..., singleLine: true)` (shipped; Swift to pass it) |

### S2
| # | Screen | Delta | Canvas | Fix (owner) |
|---|---|---|---|---|
| 3 | Day, second zone | Header labels **collide**: "AMSTERDAMNEW YORK". | Two 54pt columns "ACCRA" / "NEW YORK" aligned over the two hour columns. With the 46pt gap between hour columns a long city name cannot fit. | Use short labels (≤ 4 chars: IATA-style "AMS", "NYC"; or the tz abbreviation "CEST", "EDT") and put each over its own hour column (x=16 and x=62) (Swift; rule added to `day-view.md`) |
| 4 | Detail | Eyebrow reads generic **"EVENT"**. | `CALENDAR · ACCOUNT EMAIL`, e.g. "PERSONAL · CASPRINE.001@GMAIL.COM" | Use `calendarName` and `accountName` (both exist in the API since T09) (Swift) |
| 5 | Detail | **Reminder** row missing; People row absent when no guests (acceptable). | rows Where · People · **Reminder ("10 minutes before")** · Notes | add Reminder from `reminders`/default (Swift) |
| 6 | Detail join bar | Left text is the place only; **no walking time**. | "Kōya · 14 min walk" | add "· N min walk" when location permission and a route estimate exist; otherwise the place alone is fine (Swift, optional) |
| 7 | Actions sheet | "Move to another calendar" has no right-hand detail. | right 14 dim: current calendar name ("Work") | add detail (Swift) |

### S3
| # | Screen | Delta | Fix |
|---|---|---|---|
| 8 | Day | Event blocks end **4pt** from the right edge; canvas leaves 16 (x=64+310 = 374 of 390; second zone: 116+258=374). | `TimelineColumn` trailing padding 16 in Day view (Swift; CalendarUI unchanged) |
| 9 | Day | Opens scrolled so *now* is ~8% from the top; spec says ~25%. | scroll anchor: now − 1 hour... (Swift) |
| 10 | 3 days | The *now* label chip is missing in today's column (canvas shows "10:22" chip at x=2). | pass `nowLabel` to today's `TimelineColumn` (Swift) |
| 11 | Week | Matches v-week (header letters, 2-digit hours, 10pt titles, hatch in today only). No deltas beyond the same now-line label (none in canvas) ✓. | — |
| 12 | A11y (canvas token) | The *now* chip in **light** is `#0A0A0B` text on `#8A5A00`: **3.3:1** at 11pt (< 4.5). Dark is fine (11:1). | Proposal: in light mode draw the chip text in `bg` (`#F1F1EE`, 5.3:1). One-line change in `BrassLine`; I will make it (design lane) unless the user objects: logged under Q10 |

## What matches (checked)
Day: header "Sun 11 Oct ⌄", view switch, hour rows at 64pt with mono labels on bg plates, second-zone labels in `faint`, hatch from the top to the now line, amber now line with chip, event blocks (surface fill, 3pt account bar, 14pt title + 12pt dim time), FAB 28pt from the edge. 3 days: header cells (amber today, 12 weekday), lanes for overlaps, declined strike at 50%, hatch only in today's column. Week: M T W T F S S header, two-digit hours, 10pt titles, today's hatch. Detail: back glyph, amber "More", 14pt account square + eyebrow, 30pt title, 18pt when, amber relative line, join bar in the account bright colour with a black inline Map button, mono-caps key/value rows with hairlines, Notes "None". Actions: grip, 56pt rows, red Delete, scrim. Delete: title, danger-outline "Delete event", outline "Keep it".

## Notes for the Swift lane
All items above are Feature-side except the single CalendarUI change I made (`singleLine`, shipped) and item 12 (mine). Re-capture request: once the fixtures include a guest list and a repeating event, I will check People and the delete-repeating sheet.
