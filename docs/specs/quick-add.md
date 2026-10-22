# Quick add and free time with alfather (boards q-type … q-none)

Notes: *Plus, then type or say it. alfather reads as you type. A preview shows what was understood; **nothing is created until you tap Create**. If something is unclear it asks, never guesses. If alfather is down, the form and simple rules still work. Free time: ask in words, see slots, book one.* Stories 7.3, 7.4, 5.1. Backend: B3 (rule-based quick-add + alfather). Until B3 lands the screens run on the rule-based parser only.

## Entry
FAB (or long-press on an empty hour) opens the **new-event sheet** (h-new board): scrim, `CalSheet` with a `LabeledField`-style input *"Add with alfather"* + right mono 11 caps dim **"TYPE OR SAY IT"**, then rows **New event · Full form**, **Find free time · Ask alfather**, **Block focus time · 2 hours, today**. Without alfather configured the placeholder is **"Add an event"** and the right label is omitted. Tapping the field raises the keyboard and morphs into the typing sheet.

## Typing (q-type)
Sheet anchored **above the keyboard** (`bottom = keyboard height`), over the dimmed Home (scrim covers everything above the sheet). Contents: grip; input box (`in f`: 1pt `ink` border, `surface`, min h56, padding 14/16, 17pt, multi-line, amber caret); below it a row (padding 14/24/0): `CalTag("alfather")` left, mono 11 caps dim **"READS AS YOU TYPE"** right. Return key = "Go" → Preview. Parsing runs after a 400 ms pause (debounced, cancels in flight); recognised words get a **2pt underline** while typing (see preview colours). Dictation key works like voice.

## Speaking (q-voice)
Sheet from the bottom (padding-bottom 44): header "Listening" 20 Medium; ★`VoiceMeter` (8 bars, 6pt wide, 4pt gap, max height 48, amber for the active/recent bars, `faint` for older); live transcript 20pt ink (padding-top 18, wraps); buttons side by side gap 10: outline **"Cancel"**, primary **"Done"**. Words appear live (SFSpeechRecognizer on-device when available). Done → Preview. Mic permission denied → neutral banner "Allow the microphone in Settings, or type it instead." with a Settings link.

## Preview (q-parse) — full screen
`ScreenNav("Add with alfather", leading: .close)`. The user's text 19pt ink, line-height 1.5, with ★`UnderlineSpan`s: the **who** in `a1` (cobalt), the **when** in **amber**, the **where** in `a3` (moss) — 2pt inset-bottom underlines (never background fills). Label **"I understood"** (padding-top 28). `KeyValueRow`s: **Title** "Lunch with Sam" · **When** (amber value) "Fri 16 Oct · 13:00 to 14:00" · **Where** "Kōya, 49 Frith St" · **Calendar** (square + name, right dim "Default") · **People** "Sam Boateng". Rows that were not found are omitted (never "unknown"). `FooterBar`: primary **"Create"**, outline **"Edit details"** (opens the create form prefilled). Create follows the create-flow rules (provider first, saving state) and on success returns Home with the Undo toast (q-done).

## Needs a choice (q-ambig)
`ScreenNav("Add with alfather", leading: .close)`, the text, neutral `Banner`: **"Two things to check."** " I won't guess." Then label per question: **"Which Sam?"** radio rows with initials box + name (`SheetRow` with `PersonRow` look), **"Which Friday?"** radios "This Friday, 16 Oct" / "Next Friday, 23 Oct", plus an optional row **"What time?"** with amber "Pick" (opens the Starts sheet). `FooterBar` primary **"Continue"**, disabled (35%) until every question has an answer. Never preselect a guess except where the board shows it ("This Friday" preselected as the nearest; "Which Sam" preselects the most recent attendee but the user must still tap Continue).

## Created (q-done)
Home with the new row in its day (label "Fri 16 Oct") carrying status word **"New"** (dim) and `ToastView("Added Lunch with Sam, Fri 13:00", "Undo")` at bottom 96 (FAB present). "New" clears after the next app launch or 24 h.

## alfather unreachable (q-down)
`ScreenNav`, the text kept in the field style, red `Banner`: **"alfather isn't answering."** " Your words are kept. Nothing was created." Two explanatory rows (`.r.r2`): **"Try again · alfather is on your home server"**, **"Fill in the form instead · Calendar can guess simple things like Fri 13:00 without alfather"**. `FooterBar` primary **"Try again"**, outline **"New event form"** (prefilled with the rule-based guess). Offline vs alfather-down: offline shows "You're offline" banner instead and disables Create.

## Find free time: ask (q-ask)
`ScreenNav("Find free time", leading: .close)`; `LabeledField("Ask alfather")` multi-line, focused, text "when am I free for 2 hours this week?"; label **"Try"** with three suggestion rows (17pt dim, tap fills the field): "Free for an hour tomorrow afternoon" · "A 30 minute gap before Friday" · "Two hours of focus next week". While searching: `ProgressRule` (indeterminate) and hint 13 dim **"Looking across 3 accounts, inside your working hours."**

## Free slots (q-slots)
`ScreenNav("Free for 2 hours")` (title from the query), sub 16 dim "This week, inside 09:00 to 18:00. 3 slots found." **Best fit** = a Next-style block in the **cobalt/`a1` bright colour (not an account colour: neutral "highlight")**, padding 22/24, title 26 Medium "Tue 13 · 14:00 to 16:00", footer 18 "Best fit, nothing nearby", inline filled **"Book"** button. Other slots: `.r.r2` rows (title 19, sub 14 dim reason "After Standup" / "Between two meetings", trailing amber **"Book"**). Label "Tue 13 at a glance" + ★`DayGlance` (h36, `surface`, 1pt `line`; busy blocks in account colours at proportional positions over 09–18, the proposed slot as a **1pt dashed amber outline**, labels 09 12 15 18 mono 11 dim). Tapping a slot row selects it for the glance bar; Book → Book sheet.

## Book the slot (q-book)
`ScreenNav("Book the slot", leading: .back)`; the slot block (`a1` bright, title 26, footer "2 hours"); `LabeledField("Title")` (focused, default "Focus time"); rows Calendar (default, square + chevron), **Show me as** "Busy", Reminder; `FooterBar` primary **"Book it"**, outline **"Back to results"**. Success → Home with toast "Booked Focus time, Tue 14:00" + Undo.

## No gap (q-none)
Title "Free for 4 hours"; quiet card `QuietCard`: **"No 4 hour gap"** / dim "Not this week, inside 09:00 to 18:00."; label **"Closest"** + two `.r.r2` rows ("Mon 19 · 09:00 to 13:00 · Next week", "Wed 14 · 09:00 to 11:00 · Only 2 hours, this week") each with amber "Book"; outline button **"Ignore working hours"** (re-runs the search without the working-hours limit).

## Components (CalendarUI)
★`UnderlineSpan` (AttributedString helper: colour-coded 2pt underlines), ★`VoiceMeter`, ★`DayGlance`; existing `CalSheet`, `CalTag`, `Banner`, `KeyValueRow`, `LabeledField`, `FooterBar`, `QuietCard`, `ListRow`, `ProgressRule`.

## Done means
Typing, speaking, preview, ambiguity, created (with New + Undo), alfather-down, ask, slots (with glance bar), book, no-gap — dark and light — match q-* boards, including the colour-coded underlines and "Nothing is created until you tap Create".
