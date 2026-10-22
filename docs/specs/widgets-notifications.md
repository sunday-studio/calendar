# Widgets, notifications, Live Activity (boards w-home, w-states, w-remind, w-live, w-moved, w-reauth, w-summary)

Notes: *Widgets show the next event and the rest of today. Reminders are scheduled on the phone from the saved agenda. A Join button appears on the alert when there is a link. A live activity starts 30 minutes before an event. Moves, cancellations and sign-in problems get their own alerts.* Stories 6.1, 6.2, 6.4, 7.5. Targets: `ios/CalendarWidgets`, `ios/CalendarNotificationService` (Swift lane); the shared views below live in CalendarUI so the targets import them.

## Widgets (w-home, w-states)
Data from the App Group saved agenda (never the network). Timeline refresh: at every event start/end boundary plus every 15 min (iOS budget); `Text(timerInterval:)` is not used (countdown words are computed per entry: "in 8 min").
- **Small, 166×166 (★`WidgetNextSmall`)**: surface `#0A0A0B` (see decision below), 1pt border `#232326`, padding 14. Top: `FlipCells(.small)` **20pt** start time (cell h30); a 3×30 account-colour bar at the leading edge (top 14); title **19 Medium**, lh 1.15 (max 2 lines), margin-top 12; line 14 amber **"in 8 min"** (margin-top 6). Tap opens the event.
- **Small date, 176×166**: surface `p1`, 1pt `border`, padding 14: mono 13 dim caps weekday "FRIDAY" and **36pt** day number "16" (sans). Tap opens Home on today.
- **Medium, 358×166 (★`WidgetTodayMedium`)**: padding 14. Header row: mono 11 +.14em caps dim "TODAY" left, "3 LEFT" right (count of remaining events). Up to **3 rows** (h44): account bar x=0 (top/bottom 9), time mono 13 dim in a 42pt column, title 16 (1 line), status 13 amber right ("8 min" for the next one, "Moved", "Reply needed"). No dividers needed beyond the 1pt `line` already in rows (last row none).
- **States (120pt-high medium previews)**: **In progress** (small layout with flip cells, amber **"Now · 22 min left"**); **Nothing left today** (title 19 Medium "Done for today", 14 dim "Next: Standup, tomorrow 09:00"); **Needs sign in** (title 17 Medium "Showing saved events", 14 amber "[work email] needs sign in. Open Calendar."); **No accounts** (title 19 Medium "Connect an account", 14 dim "Tap to open Calendar").
- **Appearance decision (DECISIONS Q8):** the canvas draws widgets always dark. Default: widgets **adapt** (dark = canvas values, light = the light tokens with the Next block's bright account colour) because a permanently dark widget looks wrong on a light home screen; flip to always-dark is a one-line `containerBackground` change.
- Accessibility: one combined element per widget ("Roadmap review, 10:30, in 8 minutes").

## Reminder notification (w-remind)
Scheduled locally (UNCalendarNotificationTrigger) from the saved agenda; lead time from Alerts (default 10 min). Content: title **"Roadmap review in 8 min"** · body **"10:30 · Google Meet · [work email]"** · category with actions **"Join"** (primary, foreground, only when a link exists) and **"Snooze 5 min"** (background). Without a link: no actions (second card: title **"Lunch with Sam at 12:30"**, body **"Kōya · 14 min walk"** — the walking time is added only when location services/permission exist, otherwise just the place). Thread identifier per event; replaced when the event moves. App name label "Calendar". Time-sensitive interruption level only for events with a link starting within the lead time.

## Live Activity (w-live)
Starts **30 minutes before** an event that has a link or location; ends when the event ends (or dismissed). Lock-screen layout (★`LiveActivityBlock`): card with a 1pt `#232326` border on `#0A0A0B`; top section padding 16, fill = the account **bright** colour, text `#0A0A0B`: `FlipCells(.small)` at **22pt** (cell h36) start time; centre column: title **20 Medium** (lh 1.2, ≤2 lines), line 14 **"in 8 min · Google Meet"** (live countdown words via the activity's relative text); right: **"Join"** button `#0A0A0B` fill, `#F4F1EA` 15pt, padding 8/18 (a `Link` / App Intent opening the URL). Below: **3pt progress line** (track `#222225`, fill amber) = elapsed fraction of the 30 minutes to start, then of the event once running (copy changes to "Now · 22 min left"). Dynamic Island (not drawn in the canvas; proposal): compact leading = flip-cell-style "10:30" mono 12, trailing = minutes remaining mono; expanded = title + Join; minimal = amber square. Caption on the board ("starts when the event is 30 minutes away…") is an annotation, not UI.

## Event moved / cancelled (w-moved)
Push (silent, then local notification built by the NSE from the diff): title **"Dentist moved to 11:45"**, body **"Was 11:00 · Personal"**, actions **"View"** (opens detail) and **"OK"**. Cancelled: title **"Gym on Sunday cancelled"**, body **"17:30 · Home"**, no actions. Grouped by thread per day; at most one notification per event per change batch.

## Sign in needed + secret expiry (w-reauth)
Title **"[work email] needs you to sign in"**, body **"Its events are paused until you do."**, actions **"Sign in"** (opens Accounts › reconnect) and **"Later"**. Server secret: title **"Server secret expires in 12 days"**, body **"Renew the Microsoft client secret on your server."**, no actions; sent at 14 days, 3 days, 1 day. Controlled by Alerts › "When an account needs sign in".

## Morning summary (w-summary) — optional, alfather (M6+)
Label "Calendar · alfather". Title **"Today: 5 events, first at 09:00"**, body (≤ 3 lines): **"Standup, Design sync, Lunch, 1:1, Dinner. One reply needed. Free from 14:00 to 17:00."**, actions **"Open today"** and **"Mute"**. Off by default. The canvas says the time is chosen in Alerts, but g-alerts has no time row: add **"Morning summary"** toggle + a `FlipCells(.small)` time row (default 07:30) under "Also tell me" (DECISIONS Q9).

## Components (CalendarUI, shared with widget targets)
★`WidgetNextSmall`, ★`WidgetTodayMedium`, ★`WidgetStateCard`, ★`LiveActivityBlock`. They use only `Theme`, `AccountColor`, `FlipCells` and `CalFont`; no SwiftUI features unavailable to WidgetKit.

## Done means
Widget gallery (small, date, medium, four states) and notification mocks match w-* in layout and copy in dark and light; a real device/simulator reminder shows the actions; the Live Activity renders with flip cells, bright block and progress line.
