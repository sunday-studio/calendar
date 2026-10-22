# Replies / RSVP (boards r-list … r-ro)

Notes: *A banner counts invitations that need a reply. Open one: Yes, Maybe and No sit at the bottom. No and Maybe can carry a note. Repeating invitations ask this one or the series. iCloud and read-only invitations cannot be answered here.* Story 5.4.

## Entry: Home banner + "Reply needed" (r-list)
`Banner` under the header (margin-top 18): **"2 invitations"** (Medium) " need a reply." action **"Review"** → scrolls/focuses the group below. Home shows a first group labelled **"Reply needed"** above Today: two-line rows (`.r.r2`, h72): bar, time or weekday in the mono column ("19:00", "Tue"), title 19 + 14 dim second line "Today · from Ana" / "13:00 · from Maya", trailing amber **"Reply"** (15). Rows that still appear in their day also carry "Reply needed" there. Banner disappears at 0.

## Invitation detail with reply bar (r-detail)
Detail screen as in event-detail.md, with: eyebrow `CALENDAR · INVITED BY MAYA`; rows Where, People ("Maya, Sam, Kofi and 2 more" + right dim "3 yes"), and **"You are free then"** key/value with 16 dim "Nothing else on your board at 13:00." (or amber "Overlaps Dentist (11:45)" when busy). No "More" until replied.
★**ReplyBar** (pinned `FooterBar`): label **"Going?"** (mono caps 11, amber, padding-bottom 4) then three equal buttons, gap 12: **Yes** (primary) · **Maybe** (outline) · **No** (outline). Highlight rule: the current/pending choice is primary; with no reply yet **Yes** is primary. Yes sends immediately (toast); Maybe and No open the note sheet.

## Reply with a note (r-sheet) — sheet
`CalSheet(title: "Can't make it" | "Maybe", message: "Maya gets your reply from [work email].")`; `LabeledField("Note, optional")` focused (placeholder none; e.g. "Clash with the dentist. Can we do 15:00?"); primary **"Send reply"**, outline **"Cancel"**. Yes never asks for a note.

## Repeating invitation (r-series) — sheet before sending
`CalSheet(title: "Reply Yes to")` (title mirrors the choice: "Reply Maybe to", "Reply No to"): radios **This event only** / **All events in the series** (default: the series); primary "Send reply", outline "Cancel". Series info line above the title area: "Every Thursday · 10:00".

## Reply sent (r-sent)
Detail updates in place: first row **"Your reply"** `KeyValueRow` value "Yes" with right amber **"Change"** (reopens the reply bar); reply bar disappears; `ToastView("You said yes to Design review", "Undo")` at **bottom 60** (no FAB on this screen), 6 s. Undo restores the previous reply (calls the provider again). Home row loses "Reply needed" and the banner count decrements.

## Reply failed (r-fail)
Red `Banner` (margin-top 24): **"Your reply wasn't sent."** " Microsoft could not be reached. Nothing changed for Maya." action **"Retry"**; reply bar remains with the attempted choice as primary (No in the board). Retry resends with the same note.

## Read-only / iCloud (r-ro)
Eyebrow `HOME · ICLOUD · INVITED BY ANA`; rows Where and **"Your reply"** value "Not answered" with `CalTag("Reply in Calendar")`; neutral `Banner` (margin-top 24): "iCloud does not let other apps reply to invitations. Open the iCloud Calendar app or reply by email."; `FooterBar` outline **"Open Apple Calendar"** (opens `calshow:`). No reply bar. Same pattern for read-only links: "This calendar is read only." + "Open in {app}".

## States / data
Reply states: needsAction, accepted, tentative, declined (Google `responseStatus`); statuses map to agenda words "Reply needed" / "Maybe" / "Declined". Optimistic UI is **not** used: the row changes after the provider confirms (matching the create flow); show the reply bar in a 35% "sending" state with a `ProgressRule` meanwhile.
Accessibility: the reply bar is a `radiogroup` ("Going? Yes, selected"); toast action reachable by VoiceOver.

## Components (CalendarUI)
★`ReplyBar`; existing `Banner`, `CalSheet`, `ChipToggle`, `LabeledField`, `KeyValueRow`, `CalTag`, `ToastView`, `FooterBar`, `ListRow`.

## Done means
r-list, r-detail, r-sheet, r-series, r-sent, r-fail, r-ro in dark and light match the boards for bar placement, the amber "Going?" label, banner/toast copy and the Yes-primary default.
