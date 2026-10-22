# Event detail and its sheets (boards d-*)

Notes from the canvas: *Account colour, time, join bar, place, people, reminder. Tap an event; the bar below the time is Join or Map. More opens actions. People opens the guest list grouped by reply. Repeating events ask this, following or all. Read only calendars show details but no actions.*

## Event detail (d-detail, d-nolink)
Pushed screen, no system bar.
1. **Nav** `ScreenNav` with empty title, back glyph, trailing text **"More"** (amber 17). Hidden for read-only events (no trailing).
2. **Eyebrow** (padding 4/24/0, row gap 10): 14×14 square in the account colour + mono 11 +.1em UPPERCASE dim `CALENDAR · ACCOUNT EMAIL` ("WORK · [WORK EMAIL]").
3. **Title** 30 Medium (`CalFont.display`), line-height 1.15, side 24, margin-top 14. Wraps (no ellipsis).
4. **When** 18 Regular ink (padding-top 10): "Sun 11 Oct · 10:30 to 11:30". All-day: "Sun 11 Oct · All day". Multi-day: "Thu 15 Oct to Fri 16 Oct".
5. **Relative** 16 amber (padding-top 2): "in 8 min" / "Now · 22 min left" / "in 2 h 8 min"; omitted for past events and for events more than a day away (show nothing).
6. ★**JoinBar** (`.joinbar`): full width, margin-top 16, padding 16/24, fill = the account's **bright** colour, text `#0A0A0B` 18. Left: provider name ("Google Meet", "Microsoft Teams", "Zoom") or **"Place · N min walk"** when there is no link. Right: inline button padding 9/22, `#0A0A0B` fill, `#F4F1EA` 16: "Join" or "Map". Both absent → no bar. Tap Join → Join sheet; Map → Apple Maps (`q=` place).
7. **Rows** ★`KeyValueRow` (`.kv`, margin-top 8, first row has a top hairline): **Where** (18) · **People** (value left: "Maya, Sam, Ana and 2 more"; right dim 15 "4 yes · 1 pending"; chevron-less, whole row opens People) · **Reminder** ("10 minutes before") · **Notes** (16 dim; "None" when empty). Rows with no data are omitted except Notes. Hairline under each row.
8. Read-only (`d-ro`): eyebrow `WORK, READ ONLY LINK` plus `CalTag("Read only")`; rows Where and **Calendar** (16 dim: "Added from a link. Calendar can show this event but not change it, reply to it or delete it."); `FooterBar` with outline button "Open in Outlook" (name of the provider app).
9. **Repeating** (`d-recur`): no join bar needed unless a link exists; rows **Repeats** (value "Every weekday" + `CalTag("Series")`), **This one** (16 dim "Tue 13 Oct is the 2nd of 40 events"), People, Reminder; hint 13 dim at the bottom "Editing or deleting will ask whether the change is for this event, this and following, or all."

## Sheets ★`CalSheet` (`.sh`)
Bottom sheet: scrim `Theme.scrim`, sheet fill `surface`, **1pt `border` top**, no corner radius, padding 12/0/34, **grip 32×3 `border` colour centred, 14 below**. Optional header `.hh` 20 Medium (padding 0/24/12) and sub `.hs` 14 dim (lh 1.4). List rows `.li`: min h56, padding 0/24, 1pt `line` top border (first row none), 19pt, optional right 14 dim detail; destructive row text `Theme.red`. Radio `.rd`: 20pt circle, 1pt `border`; on = 6pt ink ring. Buttons inside a sheet: `CalButton` stacked gap 10 inside padding 0/24. Present with the settle curve (350 ms, fade under Reduce Motion), dismiss by scrim tap or swipe.

| Sheet | Board | Content |
|---|---|---|
| Actions | d-actions | rows: Edit · Duplicate · Move to another calendar *(right: calendar name)* · Copy join link *(only with a link)* · Share details · **Delete** (red) |
| Delete | d-del | hh "Delete Lunch with Sam?" · hs "It is removed from your Personal calendar in Google. Sam gets a cancellation." (second sentence only when guests exist) · `danger` "Delete event" · `outline` "Keep it" |
| Delete repeating | d-delrec | hh "Delete Standup" · radios This event / This and following / All events (default This event) · `danger` "Delete" · `outline` "Cancel" |
| Join | d-join | hh "Open in Google Meet?" · hs the URL (14 dim, middle truncation) · rows "Open in the Meet app" / "Open in the browser" / "Copy link" · `outline` "Cancel" |

## Move (d-move)
Full screen, modal: `ScreenNav("Move to", leading: .close)`; sub 16 dim "Lunch with Sam is in Personal now."; per account a section label (account email) then rows `.li` with 14×14 colour square + calendar name + radio (current calendar preselected, read-only calendars omitted). When a calendar of a **different account** is selected show a `Banner` (neutral, no bar colour change): "Moving to another account makes a copy there and removes this one. Guests may get a new invitation." Footer `FooterBar`: primary "Move", disabled (35%) until the selection differs from the current calendar.

## Deleted + undo (d-deleted)
After Delete returns to Home: the row is gone; `ToastView("Lunch with Sam deleted", "Undo")`, inset 12, **bottom 96**, visible 6 s (stays while VoiceOver focus is on it); Undo recreates the event (backend `undelete`/re-insert) and the row returns without animation jump (settle).

## States
- Loading: title and when render from the list row immediately; rows below fill in (no spinner).
- Failure to load detail: `Banner(error)` "Couldn't load this event." + Retry; the cached fields stay visible.
- Offline: see offline-status (More dimmed, edit/reply/delete blocked with toast "Can't edit while offline").
- Accessibility: whole screen reads title, when, relative, join bar action; Join/Map buttons ≥ 44pt hit height; People row is a single button "People, 4 yes, 1 pending".

## People (d-people)
`ScreenNav("People")`, sub 16 dim "Roadmap review · 6 invited". Groups with `label` headers: **Organiser**, **Going · N**, **Not answered · N**, **Declined · N** (zero-count groups still show, header only). ★`PersonRow` (`.who`): min h52, padding 0/24, hairline under; 36×36 square initials box (`sunk` fill, 1pt `border`, initials 14 dim), name 17, right status 15: "Yes"/"Organiser" dim, "Pending" amber, "No" dim. "You" first in Going.

## Components (CalendarUI)
★`JoinBar`, ★`CalSheet` (+ `SheetRow`, `RadioMark`), ★`PersonRow` (+ `InitialsBox`). Existing: `ScreenNav`, `KeyValueRow`, `CalTag`, `CalButton`, `FooterBar`, `ToastView`, `Banner`.

## Done means
Detail (video, in person, repeating, read only), People, the four sheets, Move and the deleted toast, dark and light, match the d-* boards for spacing, the join-bar colour (account bright value) and copy.
