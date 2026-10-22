# Create and edit an event (boards e-new … e-discard; delete/undo = d-5…d-9 in event-detail.md)

Notes: *One form for new and edit. Title first. Calendar defaults to your default; change any time. Time uses flip cells with 15 minute steps. Save writes to the provider first; the board updates when it confirms. If it fails your changes stay: retry or keep a draft. Editing a repeating event asks for scope. Discard asks only when something was typed.* Stories 5.1, 5.2, 5.5, 4.3.

## Form screen (e-new) — modal, same screen for New and Edit
- `ScreenNav(title, leading: .close, trailing: "Save")`. Title = "New event" / "Edit event". "Save" is amber; **disabled (35%) until the title is non-empty**.
- Title field: `LabeledField("Title")` at margin-top 8, focus on open (keyboard up, "Title" placeholder none; the field is the first responder). Return key = "Next" → opens Starts.
- Rows (margin-top 20, first row top hairline) as `SettingsRow` h60: **Calendar** (value: 14×14 colour square + name + chevron) · **All day** (`CalSwitch`) · **Starts** (value "Wed 14 Oct · 09:30") · **Ends** ("10:00"; same day shows time only, other day shows date too) · **Repeat** ("Never ›") · **Reminder** ("10 minutes before ›") · **Video call** (value amber 16 "Add Google Meet" / "Add Teams"; tap adds, shows `CalTag("Google Meet")`, tap tag removes) · **Place** ("Add", opens a text field row) · **People** ("Add" / "Maya, Sam ›") · **Notes** (multi-line field, only after tapping "Add notes" at the bottom).
- All day on: Starts/Ends show the date only; time rows disappear.
- Defaults: calendar = default calendar (Settings); start = next quarter hour after now (or the tapped hour from the Day view long-press), length 30 min (or 1 h when started from a 1 h slot); reminder = Settings › Alerts.
- Editing an existing event shows its values; changed values render in **amber** (e-scope: "09:15" amber) until saved.
- Keyboard: form scrolls; the Save action stays in the nav bar.

## Calendar picker (e-cal) — sheet
`CalSheet(title: "Add to")`: account email labels (mono caps, padding-top 6 first / 20 after), rows `SheetRow` with 14×14 square + name + 13 dim "Default" + radio; current selected. Read-only calendars omitted. Selecting dismisses the sheet.

## Date and time (e-when) — sheet
`CalSheet(title: "Starts")`: **day strip** = `SegmentedControl` of four days around the current value ("Mon 12 · Tue 13 · Wed 14 · Thu 15", scrolls by 1 day when you tap the edge segments; long-tap opens a calendar sheet); **time** = ★`TimeStepper`: outline small buttons "−15" and "+15" (44pt hit) either side of `FlipCells(.xl)` "09:30" (cells flip on change unless Settings › Flip cells is off; haptic tick per step); label "Length" + chips (`ChipToggle`) 15 min · 30 min · 45 min · 1 h · 1 h 30 (single select, wrap); hint 13 dim **"Ends 10:00. You are free then."** (or "Overlaps Dentist at 10:00." in red when it collides); primary "Done". The Ends row opens the same sheet titled "Ends" without the length chips.

## Repeat (e-repeat) — sheet
`CalSheet(title: "Repeat")` rows with radio: Never · Every day · Every weekday · Every week on {weekday} · Every month on the {n}th · **Custom ›** (chevron, pushes the custom form: interval stepper, weekday chips, ends never/on date/after N). Weekday/ordinal text derives from the start date.

## Video call added (e-meet)
Row **Video call** with sub 13 dim "The link is made when you save." and `CalTag("Google Meet")`. On a Microsoft calendar the offer is Teams; when the calendar's provider cannot make a link show `Banner` (neutral, no action): "Meet links need a Google account. This calendar is Microsoft, so Calendar would offer Teams instead." (shown only when the user picked a provider that does not match).

## People (e-guests) — pushed
`ScreenNav("People", leading: .back, trailing: "Done")`; `LabeledField("Add")` focused; label "Suggestions" with `PersonRow`-style rows (initials box, name 17, second line 13 dim email or "from contacts"); label "Added" with rows whose trailing text is dim "Remove"; hint 13 dim "They get an invitation from your {Work} calendar when you save." Suggestions come from recent attendees + Contacts (permission requested on first focus; without it, only recents).

## Saving (e-saving)
Nav trailing "Saving" in dim (no button), `ProgressRule(0.6)` flush under the nav, form at **50% opacity and non-interactive**, hint 13 dim "Writing to Google first. The event shows on your board when Google confirms." (provider name from the calendar). Close (X) stays active and cancels only the *wait*, not the write (toast "Still saving in the background").

## Save failed (e-fail)
Red `Banner` directly under the nav: **"Google didn't accept the event."** " Nothing was saved. Your changes are still here." action **"Retry"**; the form stays editable; `FooterBar` primary **"Try again"** + outline **"Keep as draft"** (draft saved locally, shown in Home as a "Draft" row at the top of Today — status word amber "Draft"). 409/412 conflicts go to the conflict screen, not this one.

## Edit scope (e-scope) — repeating events, on Save
Sheet over the form: `CalSheet(title: "Save changes to")` radios This event · This and following · All events (default This event); `CalButton("Save")` + outline "Cancel". Only shown when the event is a series (or an exception of one) and something other than a per-instance field changed.

## Changed elsewhere (e-conflict) — modal on 409/412
`ScreenNav(<event title>, leading: .close)`; amber `Banner`: **"This event changed while you were editing."** " It was moved at 10:14."; per changed field a label (mono caps: "Start time", "Title") followed by two `KeyValueRow`s: **"Yours"** (ink) and **"Now on Google"** (amber value); `FooterBar`: primary **"Use latest and re-apply my title"** (merges non-conflicting local edits) + outline **"Keep mine"** (overwrites with a force flag).

## Discard (e-discard) — sheet
Tapping X with unsaved input: `CalSheet(title: "Discard this event?", message: "You have not saved it yet.")` with `danger` "Discard" + outline "Keep editing". For Edit: title "Discard changes?". No dialog when nothing was typed or changed.

## Delete / undo
See event-detail.md (d-actions, d-del, d-delrec, d-deleted). Delete of a repeating event uses the same scope sheet pattern as e-scope.

## Accessibility
Every row is a button "Starts, Wed 14 October, 09:30". The flip-cell time announces as a time; ± buttons announce "15 minutes later". Save disabled state announced. Dynamic Type: rows grow, never truncate values to ellipsis before 3 lines.

## Components (CalendarUI)
Existing: `ScreenNav`, `LabeledField`, `SettingsRow`/`NavRow`/`ToggleRow`, `CalSheet`, `SheetRow`, `ChipToggle`, `SegmentedControl`, `FlipCells`, `Banner`, `FooterBar`, `ProgressRule`, `CalTag`, `PersonRow`, `KeyValueRow`. New ★: `TimeStepper`, `FormValueRow` (label + coloured square/changed-amber value helper).

## Done means
New event (empty, filled), calendar sheet, date/time sheet, repeat sheet, video call row, people, saving, failed, scope sheet, conflict, discard — dark and light — match e-* boards in row heights (60), flip-cell time (xl 44/60), copy and amber "changed" values.
