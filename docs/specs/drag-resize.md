# Drag, resize, rollback (boards v-drag, v-resize, v-fail; Week = day-view.md)

Notes: *Long press lifts an event. The old slot stays dashed. Snaps to 15 minutes. Drag the bottom edge to change length. If the provider refuses, the event slides back and a toast says why.* **3 days is the main place to move events on a phone**; Week is read-only (no drag). Stories 6.3, 5.1.

## Move (v-drag) — Day and 3 days
1. **Lift**: long-press 0.4 s on an event → haptic `.medium`, the block becomes *lifted*: fill `sunk`, 1pt `ink` outline, the only shadow in the system (`0 8 24 rgba(0,0,0,.4)`), z above everything; its old slot becomes a **ghost** (1pt dashed `border`, no fill, title dim). Scrolling is locked in the grid except auto-scroll.
2. **Drag**: block follows the finger, snapping to the **15-minute** grid vertically and to **day columns** horizontally (3 days: 3 columns). A selection haptic ticks on every snap. Auto-scroll when the finger is within 60pt of the top/bottom edge (speed ∝ proximity, max 480pt/s) or the first/last column (pages the 3-day window by one day with a 300 ms settle).
3. **Chip** ★`DragChip` (canvas `.chip`): ink fill, bg text, **mono 12**, padding 5/10, no radius, `top = block.top − 32`, `x = block.x + 2`; text **"Mon 12 · 15:45"** (day + new start time). Follows the block; flips below the block when it would clip the top.
4. **Drop**: release → block settles into the slot (150 ms), the write starts (move = **optimistic**: the block stays in the new slot, ghost disappears, a 2pt `ProgressRule` runs under the header until the provider answers). Drop outside the grid or back onto the original slot cancels with no write.  The calendar never changes by drag.
5. Read-only calendars and invitations you did not organise cannot be lifted: a light haptic `.rigid` and toast "Can't move this event" (reason: "Read only calendar" / "Only the organiser can move it").
6. Repeating events: dropping asks the scope sheet (This event / This and following / All events) before writing.
7. All-day and multi-day events are not draggable in the grid (edit form only).
8. Reduce Motion: no lift animation scaling; snap haptics stay; settle uses fade.

## Resize (v-resize) — Day and 3 days
Canvas shows the active block *lifted* (sunk fill, ink outline) with a **28×3 `ink` handle** centred 6pt from the bottom. Entry = long-press; the handle appears as soon as the lift happens. Dragging the body moves; dragging the handle resizes (the two never combine in one gesture).
- Start never moves; length snaps to 15 min; **minimum 15 min** (height 30); maximum until 23:45.
- Chip below the block (top = block.bottom + 6): **"1 h 15 min · ends 13:45"** (length, then end time; the chip shows the length, not the start).
- Drop writes (optimistic, same ProgressRule). Haptic tick per snap.

## Move failed, rolled back (v-fail)
When the provider refuses (4xx/412/409, offline, timeout 10 s): the block **slides back to its original slot** (250 ms settle; Reduce Motion: crossfade) and a **two-line toast** appears (`ToastView` multi-line variant, inset 12, **bottom 60**, no FAB overlap because it sits above it): line 1 Medium **"1:1 Maya went back to 15:00."** line 2 regular **"[work email] refused the change."** (account email, then provider reason in plain words when known: "It was changed on Google since you opened it." for 412), action **"Try again"** (reapplies the same move). Auto-dismiss 8 s (stays while VoiceOver is on it).
Offline: the drag does not start (see offline-status.md "Can't edit while offline").

## Accessibility
Drag is not required: the event menu offers custom actions **"Move to…"** (opens the form's Starts sheet) and **"Change length…"** (Ends sheet). VoiceOver announces "Lifted, Mon 12, 15:45" as the chip text changes (polite, throttled to 1/s).

## Components (CalendarUI)
★`DragChip`; extend `ToastView` with an optional second line (`detail:`). Timeline pieces from day-view.md (`TimelineEventBlock` supports `.lifted` and `.ghost`).

## Done means
Screenshots mid-drag (3 days), mid-resize (Day) and the failed-move toast in dark and light match v-drag, v-resize, v-fail: dashed ghost, ink-outlined lifted block with shadow, chip text format, 28×3 handle, two-line toast at bottom 60.
