# View select (replaces the Agenda / Day / 3 days / Week segmented switch)

Canvas change (2026-10-16, artifact `9NRPFcFQbsNXewTJMLRkfZ`, page **Day and week**, board **v-select**, light twin `v-select-l`): the four-segment switch under the header is replaced by **one select field**. Boards `v-day, v-day2, v-dayempty, v-three, v-week, v-drag, v-resize, v-fail` and Home `h-main` now draw the select. Supersedes the switch parts of `day-view.md` and DECISIONS Q7.

## Closed state (revised: no extra row)
- Lives **in the header row, beside today's date button**: right group = select + `TodayButton`, gap 8, vertically centred with the title. The row under the header is gone, so everything below moves up ~58pt.
- Field: **h38** (same as the 38pt date button), padding 0/12, 1pt control border (`Theme.controlBorder`), fill `surface`, **16pt `ink` value** + down chevron, 10pt apart, no "VIEW" label (it is spoken, not drawn).
- Width budget on 390: title ≤ ~170, select ≈ 100 ("Agenda" widest), date button 38, gaps 8/8. At large Dynamic Type cap the select text at `.xLarge` and let the title shrink (`minimumScaleFactor(0.6)`), never wrap the header.
- Accessibility: one button, label "View, Day", hint "Opens the list of views".

## Open state (board v-select)
Bottom sheet over the dimmed screen (`.calSheet`): grip, header **"View"** (20 Medium), four `SheetRow`s with radios: **Agenda · Day · 3 days · Week**, current one selected. Tapping a row selects, dismisses and switches the screen. Scrim tap dismisses. Day/3 days/Week stay disabled (35%, not selectable) only while unbuilt.

## Behaviour
- Selection persists per launch (as the switch did); Home's title still opens the month/date picker, unaffected.
- Replaces `SegmentedControl(HomeMode.titles, ...)` in `HomeView`. `SegmentedControl` stays in CalendarUI for other uses (Settings › Appearance, token forms).
- New CalendarUI component: **`SelectField(label:value:action:)`** (closed field) and reuse `CalSheet`/`SheetRow` for the list. (Design lane adds `SelectField`; Swift wires it.)
- Hit target: draw 38pt, but extend the tappable area to 44pt (`contentShape` + padding).

## Done means
Home and the three timeline views show the closed select; tapping opens the sheet; screenshots (light + dark) match `v-day`, `v-select` and `h-main`.
