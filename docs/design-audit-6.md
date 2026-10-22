# Design audit 6: re-verification + Move, Reply-needed group, invitations banner

_Design lane · conformance pass 5 · dark only, iPhone 17, fixtures normal. Shots `docs/audit-shots/r2/{v6,m1,m2,m3}-*.png`. Write-dependent states (move result, duplicate result, undo toast) wait for the fixture write flags._

## Re-verified fixed
- Detail eyebrow now "ALEX (WORK) · ALEX@NORTHWIND.EXAMPLE" ✓ (audit-3 #4, audit-5 #4).
- Actions sheet: Edit · **Duplicate** · Move to another calendar · Share details · Delete ✓ (right-hand current-calendar detail on "Move" still missing: audit-3 #7).
- Join/Map bar: place "Boardroom" + Map ✓. People row "Priya and Sam · 1 yes · 2 pending" ✓ (audit-5 #3). Grammar: **"1 yes"** is fine; "1 pending" etc. fine.
- Home invitations banner "1 invitation needs a reply." + Review ✓; "REPLY NEEDED" group above Today with a weekday time column ("Fri"), title, 14pt dim second line "Fri 16 Oct · 09:00", amber "Reply" ✓ matches r-list.
## Not re-verified
Audit-5 #1 failure headline, #2 day strip, #6 double hairline, light mode (all states), Calendar sheet, Guests screen: need writes in fixtures.

## New screens vs d-move
Move to (d-move): ✓ close glyph, title, sub "Quarterly planning is in Alex (Work) now.", account labels, calendars with 14pt squares and radios, current preselected, "Move" disabled at 35% until the selection changes, cross-account notice **"Moving to another account makes a copy there and removes this one. Guests may get a new invitation."** (banner under the sub) appears on selecting another account, "Move" becomes primary. All copy exact.
### Deltas
| # | Sev | Delta | Fix |
|---|---|---|---|
| 1 | S3 | Rows are 128pt apart (each calendar sits in a tall block), canvas rows are 56pt (`.li`). Account groups look spaced out. | `SheetRow` height 56 and group label padding-top 20 (Swift: remove the extra bottom padding per group) |
| 2 | S3 | Reply-needed rows list the date in the title line and mono "Fri" at the time position; canvas shows time "19:00"/"Tue" there and the date-time in the sub line only when different day: acceptable. | — |
| 3 | S3 | Banner kind: the move notice uses the amber attention bar; canvas uses the neutral banner (no coloured bar) because it is information. | `AmberBanner(kind: .neutral)`: **added to CalendarUI now** (3pt bar in `border` colour) (Swift to use) |

## Next
S4 drag/resize: will review against docs/specs/drag-resize.md (lift, dashed ghost, chip "Mon 12 · 15:45", 28×3 handle, rollback toast two-line at bottom 60) in 3 days view.

## Addendum: write flows with `--fixtures-write` (dark)
Shots `docs/audit-shots/r2/{w1,x2,x4,x5}-*.png`.
| Flow | Result | Delta |
|---|---|---|
| Create, `fail` | ✓ **Fixed**: headline "Google didn't accept the event. Nothing was saved. Your changes are still here." + Retry, Title keeps focus, 16pt gap, "Try again" / "Keep as draft" (audit-5 #1, #8). | — |
| RSVP Yes, `ok` (reached by accident) | ✓ r-sent: "YOUR REPLY · Yes" with amber "Change", reply bar gone, People counts update (2 yes · 1 pending), single hairline (audit-5 #6 ✓), toast "You said yes to Quarterly planning" + Undo. | (S3) toast sits ~130pt above the screen edge; canvas `bottom: 60` (no FAB on this screen). Use the 60pt offset here, 96pt only where the FAB is visible |
| Duplicate / Move / Delete confirm / undo toast / scope sheets / conflict / slow | **Not verified**: my taps raced the More sheet (opened late on this build) and I stopped before wasting more runs. | next pass |
Observation for Swift: right after pushing the detail, the first tap on "More" is ignored for ~1 s (the sheet only opens on a second tap). Likely the push transition: enable the button after the screen appears or make it respond immediately.
