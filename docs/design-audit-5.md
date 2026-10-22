# Design audit 5: create/edit, RSVP, People, delete flow

_Design lane · conformance pass 4 · boards e-new, e-when, e-repeat, e-fail, e-discard, r-detail, r-sheet, d-people · iPhone 17, dark, richer fixtures. Shots `docs/audit-shots/r2/{e0,e2,e3,e4,e5,e6,r0-r4}-*.png`._
**Not reached (light mode everywhere; Calendar sheet, Meet row, Guests screen, Saving state timing, conflict, scope sheet, series sheet, delete sheet/undo toast, r-sent/r-fail/r-ro, d-recur, d-ro, d-delrec).** The save against the fixture hub fails immediately ("This event no longer exists"), so success/saving/undo states could not be seen; fixtures need a write path (backend B1) for those.

## Matches
New event form (e-new): nav "New event" + X + amber Save (35% until a title is typed ✓), mono-caps TITLE label with focused ink-border field, 60pt rows, calendar square, All day switch, Starts/Ends/Repeat/Reminder/Video call (amber "Add Google Meet")/Place/People, amber "Add notes". Date/time sheet (e-when): day segments, xl flip-cell time with −15/+15, Length chips, "Ends 16:30.", Done. Repeat sheet (e-repeat): radios, Never preselected. Failure (e-fail): red banner + "Retry", form kept, "Try again" / "Keep as draft". Discard sheet: copy and buttons exact. People (d-people): Organiser / Going · 5 / Not answered · 3 / Declined · 1 groups, initials boxes, amber "Pending". RSVP: "Your reply · Yes · Change", reply bar "GOING?" amber with Yes primary, note sheet "Can't make it" with optional note, Send reply, Cancel.

## Ranked deltas
### S1
| # | Screen | Delta | Fix |
|---|---|---|---|
| 1 | Save failure copy | **"This event no longer exists."** is shown for a *create* (404/unsupported write). Canvas: "Google didn't accept the event. Nothing was saved. Your changes are still here." Raw server reasons must not become the headline. | Map errors to the canvas sentence ("{Provider} didn't accept the event.") and put the reason in a second line only when it is user-actionable (Swift) |
| 2 | Date/time sheet (e-when) | Day strip shows full labels "Sat 10 Oct · Sun 11 Oct · Mon 12 Oct · Tue 13 Oct": cramped, 4 segments at 15pt with the selected one clipped near edges. Canvas: "Mon 12 · Tue 13 · Wed 14 · Thu 15" and the selection is the 3rd. | Short labels ("Sun 11"), and window of −2…+1 days around the value (Swift) |
| 3 | People row on detail | Value truncates ("Priya, Aisha, Ed and…") while the right text "5 yes · 3 pending · 1 no" competes. | Left: first names only up to 2 + "and N more"; right: "5 yes · 3 pending" (drop "1 no" past 2 clauses) (Swift) |

### S2
| # | Screen | Delta | Fix |
|---|---|---|---|
| 4 | Detail eyebrow | Reads "CALENDAR" (not "CALENDAR · ACCOUNT EMAIL") (audit-3 #4 still open); title is 30pt and wraps to four lines: fine, but time line sits tight under it (10 → 14pt) | Swift |
| 5 | Detail join bar | Left text "Meet" (canvas: "Google Meet") | provider display name (Swift) |
| 6 | Detail, "Your reply" | Row's right "Change" works; hairline above next row is double (two 1pt lines 20pt apart) | remove the extra divider after `KeyValueRow` when followed by another (Swift) |
| 7 | Repeat sheet | "Every week on Sun" (canvas: "Every week on Wednesday"), weekday abbreviated; "Custom ›" row missing (known) | full weekday name (Swift) |
| 8 | Edit form top banner | After failure the Title label sits 8pt under the banner and the field lost its focus ring (canvas keeps focus). | 16pt gap; keep first-responder (Swift) |
| 9 | Agenda | Long titles truncate with an ellipsis in the single-line row ("All hands: quarterly results,…") ✓ per canvas; overlapping rows each show "Overlaps with X" ✓; "Cancelled: Brown…" strikethrough + "Cancelled" ✓ (but the title text is "Cancelled: Brown bag", status word duplicates it: acceptable) | — |

### S3
Time stepper buttons "−15/+15" use a real minus sign ✓; Length chips wrap to a second line (canvas fits 5 on one line at 12pt): reduce chip padding to 8 and gap 6 to fit; "Add notes" is amber 17pt: canvas has no such row (spec addition, fine).

## Not built yet (skip per coordinator)
Move screen, Duplicate, Reply-needed group on Home, custom repeat, free-time hint, Draft row.
