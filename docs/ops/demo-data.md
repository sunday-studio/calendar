# Demo data: what is seeded and what to test with it

`scripts/run-local.sh demo` runs the real hub against the fake Google with `--profile rich` (`server/internal/provider/google/gtest/rich.go`). It covers every state the app has, so you can test on a real iPhone without hunting for a case. The small dataset that `make e2e-server` and `make ios-e2e-real` assert on is the other profile (`--profile e2e`, the default of `fakegoogle`) and is not changed.

## Start, switch, reset
```sh
CALENDAR_ADDR=100.127.10.66:8790 scripts/run-local.sh demo     # rich profile; hub on that address, fake Google on 127.0.0.1:8788
scripts/run-local.sh demo-reset                                # delete .local/demo's database (only when no hub listens on the port)
scripts/run-local.sh scenario list                             # what the fake holds now + the scenario names
DEMO_PROFILE=e2e scripts/run-local.sh demo                     # the small e2e dataset instead
DEMO_TZ=America/New_York scripts/run-local.sh demo             # "today" starts at midnight in this zone (default Europe/London)
```
- **Switching from the old demo data:** a demo database made with the e2e dataset (every database before this change) is refused with a hint. Stop the demo hub and its fake Google, run `scripts/run-local.sh demo-reset`, start `demo` again, and pair the phone again (the device token is new). The script never kills anything it did not start; a fake Google that is already running with another profile is reported and left alone.
- **Dates are relative.** Everything is placed relative to the moment the fake starts (and again on `scenario reset`), at midnight of the fake's zone. A hub restarted tomorrow still has today and tomorrow. The fake does not move events while it runs: if you leave it up for days, run `scenario reset` to re-base ("in progress now" and "in 8 minutes" are exact only at start).
- **Accounts** (sync every 15 s): `work@example.com`, `personal@example.com`, `side@example.com`, `stale@example.com`. The hub gives each account one colour (cobalt, rose, ochre, mauve); it does not use Google's per-calendar colours, so calendar colours differ per account, not per calendar.
- **Day layout.** `D0` = today, `D+1` = tomorrow. One Saturday at `D+5` or later is **completely empty** (all accounts) and the Sunday after it has **only all-day events**. Fixed-offset events that would land on those two days move to the next day, so offsets below can be one or two days later than written. In the app they are the first empty Saturday that is at least five days away, and the day after it.
- **"Done for today" cannot be static.** Home shows it when nothing is left today. To see it, start the demo late in the evening, or run `scenario outside-delete` repeatedly until the day has no upcoming event; all-day events and past events do not count.
- **Not mappable (hub ignores it, so no UI state):** `visibility` (private/confidential events look like normal ones), `attachments`, Google "locked", Google colours per event. They are in the data so a future mapper change shows up here.

## Accounts and calendars
| Account | Calendar | Access | What it exercises |
|---|---|---|---|
| work | Work (primary) | owner | the main calendar, most events |
| work | Team Platform | reader | shared team calendar with reader access: no edit, no reply (`t-inv-ro`) |
| work | Release train (subscribed) | reader | read-only subscription (import calendar, UTC) |
| work | On-call rota | writer | second writable calendar (move event between calendars) |
| work | "Cross-functional product, design, research and operations planning calendar for the Q4 launch programme" | owner | very long calendar name (Settings > Calendars, event detail, editor picker) |
| work | Archive 2024 | owner, **hidden** | hidden by default: not on Home until switched on (Settings > Calendars) |
| work | Meeting rooms, floor 3 | reader, **selected:false** | second "off by default" calendar (Google "selected" flag) |
| personal | Personal (primary) | owner | |
| personal | Family | writer | shared writer calendar |
| personal | Birthdays | reader | yearly all-day `eventType birthday`, never editable |
| personal | Holidays in United Kingdom | reader | all-day holidays, read-only |
| side | Side project (primary), Pottery class | owner, writer | a small account |
| side | Studio partner (busy only) | freeBusyReader | the hub's `freebusy` access: events titled "Busy", read-only |
| stale | Stale account (primary), Old client (reader) | owner, reader | flip it to `needs_reauth` with `scenario reauth` |

## Events (D0 = today)
IDs are the Google ids (also what `scenario` prints). "Work" = work primary unless a calendar is named. `*` = placed relative to now, not to a clock time.

### Time and layout
| Event | When | State it exercises | Look at |
|---|---|---|---|
| Incident review: checkout latency `w-live`* | started 20 min ago, 60 min, Meet | event **in progress**, Join | Home next block ("Now"), Day now line, notification |
| Customer call: Acme renewal `w-soon8`* | in 8 min, 30 min, Meet | **join window** (about 10 min before), next block countdown | Home next block, widgets, join button |
| Pricing sync `w-soon25`* | in 25 min, 25 min, Zoom URL as location | 25 min away, link parsed from location | Home next up, widget |
| Stand up and stretch / Hallway sync / Submit timesheet | 5 / 15 / 5 min | **5-min** and **15-min** events (minimum block height), `w-5min`, `w-15min`, `w-timesheet` | Day, 3 days |
| Tomorrow (D+1) busy day | 13 timed events 08:00 to 19:30 + standup + all-day | **busy day** (12+), many rows | Agenda, Day scroll, month dots |
| Interview A, Interview B, Interview debrief | D+1 10:00, 10:45, 11:30 | **back-to-back** chain | Day |
| Design review, Eng sync, 1:1 with Sam, Budget review | D+1 13:00 to 14:30 | **4-way overlap** (lanes of a quarter width) | Day, 3 days |
| Call with Mum (personal) | D+1 15:00 | cross-account overlap with Planning 14:30 | Day, free time |
| Customer onsite: Manchester `w-10h` | D+4 08:00 to 18:00 | **10-hour** event | Day, agenda |
| Overnight deploy window `w-overnight`, Ends at midnight `w-midnight-exact`, Flight LHR to JFK `p-flight` | 22:00 to 02:00, 22:00 to 00:00, 23:30 to 07:00 | events **crossing midnight** | Day (both days), agenda, month |
| Team offsite `w-offsite` | D+1 to D+3 | **multi-day all-day** | Home, month, Day all-day strip |
| Release freeze `w-freeze` | D-2 to D+1 | all-day event **ending tomorrow** | Day strip today and tomorrow |
| Office, Release freeze, Mum visiting, School INSET day, On-call: primary, Kwame's birthday | D0 | **several all-day** on one day (6) | Day strip, Home |
| Payroll cut-off, Hack week starts, Security training due `w-allday-*` | D+2 | three all-day on the same day | Day strip |
| First empty Saturday (D+5 or later) | whole day | **completely empty day** | Day ("Nothing planned"), 3 days, month |
| The Sunday after it: "Company day of rest", "Bank holiday (demo)" | whole day | day with **only all-day events** | Day, agenda, month |
| Conference week `w-conf-week` | the Monday to Friday after that | 5-day all-day | month |
| Next clock change in <zone> `w-dst-next` | 90 min around the zone's next real change | DST for whatever zone the fake runs in | Day |
| UK clocks go back: first 01:30 (BST) / second 01:30 (GMT) | 2026-10-25 | the **repeated hour** (two events both at 01:30; the first "ends" at 01:00 on the clock) | Day on that date |
| Night ops across the repeated hour | 2026-10-25 00:30 to 02:30 (3 h) | an event spanning the repeat | Day |
| UK clocks go forward: cutover across the gap | 2027-03-28 00:30 to 02:30 (1 h) | the skipped hour | Day |
| US clocks go back 01:30 EDT / 01:30 EST, US clocks go forward | 2026-11-01, 2027-03-14 (America/New_York) | across the **US DST** dates | Day, a second time zone |
| Customer workshop (New York) `w-ny` | D+2 09:00 America/New_York | other time zone (shown in your zone; the detail names New York) | Day, detail |
| India standup / Tokyo early call / Lord Howe sync / Date line sync (UTC+14) / Kathmandu check-in (+5:45) | D+3 to D+7 | zones with **half-hour, 45-min and +14 offsets**; Tokyo 07:30 and Kiritimati 09:00 fall on the previous evening in London | Day, detail, agenda day grouping |
| Quarterly planning | D+45 | far event | month, search |
| Lord Howe retreat (200 days out) | D+200 08:00 to 16:00 Australia/Lord_Howe (+10:30 then) | **far future**, 30-min zone | month far ahead, search |
| Old project kickoff (300 days ago) `w-p300` | D-300 | outside the usual window; found by search | search |
| Yesterday's retro, Missed call (declined), Sprint review, Customer trip (all-day), Workshop last week, Two weeks ago | D-1 to D-14 | **past days**, past all-day, declined in the past | agenda scroll back, month |

### Content
| Event | State it exercises | Look at |
|---|---|---|
| "Extraordinarily long event title ..." `w-longtitle` (exactly 120 chars) | very long title wraps/truncates | every list, Day block, detail, widget, notification |
| (no title) `w-notitle`, "Out of office" without summary `w-ooo-untitled` | empty title placeholder; typed events get a name | Day, agenda, detail |
| "Kōya brunch 🍜 ...", "会議 (Kaigi) ... 🇯🇵", "مرحبا — درس اللغة العربية", "Café Renée: naïve résumé review (Zoë, Søren, Łukasz)" | emoji, CJK, RTL and accents; search with and without accents | all text, search ("Koya", "kaigi") |
| Programme planning (long notes) `w-longdesc` | description of 1,500+ characters with newlines, 5 links, a very long URL; location with a full address | detail scroll, link taps |
| HTML-ish description `w-html` | tags, entities and a `<script>` shown as text, never executed | detail |
| Walkthrough (location is a link) `w-loc-url` | location that is just a URL (not a conference link) | detail location row |
| Meet (structured `conferenceData`): most meetings | Join Google Meet | Home, detail |
| Vendor review (Zoom in notes) / Partner call (Teams in location) / Training (Webex) | Zoom link only in description / Teams in location / Webex | Join button names the service |
| Dial-in only call `w-phone` | phone entry point only: **no join button** | detail |
| Whiteboard session (no link) `w-nolink`, Write quarterly summary | no conference, no guests; "Add Meet" offered | detail, editor |
| Performance review (private), Salary calibration (confidential) | `visibility` set (see Not mappable above) | detail |
| Gym (free time), Team drinks, Lunch with Ama | transparent: **free** | Day, free time (not busy) |
| Board prep (maybe) `w-board-prep`, Maybe: pottery pop-up `p-maybe` | event **status tentative** (hatched) | Day |
| Strategy offsite (cancelled by organizer) `w-cancelled`, Padel (cancelled) `p-padel-cx` | cancelled with times: hidden unless "Show declined"; an outside delete shows as removed | agenda, Settings > Appearance |
| Standup (one cancelled occurrence), Design crit (cancelled), Climbing (cancelled), Pottery class (cancelled) | cancelled **single occurrence** of a series | agenda, Day |
| Q4 kickoff `w-kickoff` | **12 guests**: accepted, declined, tentative, needsAction, 2 optional, 1 room (resource), a group address | detail People |
| Quick chat (1 guest), Write quarterly summary (none) | 1 guest / no guests | detail |
| Architecture board (organizer is CTO), most "mine" events (organizer is me) | organizer other / me | detail |
| Shared planning doc review `w-inv-edit` (`guestsCanModify` true) vs Org-wide Q&A `w-inv-noedit` (false) | invitee can edit or not | detail Edit button |
| Review with attachments `w-attach` | attachments in the data (not mapped) | detail |
| Doctor (out of office) `w-ooo-timed`, Out of office: conference travel (all-day) | `outOfOffice` timed and all-day | Day, detail |
| Deep work: Q4 roadmap `w-focus` | `focusTime` | Day |
| Office / Home / Working from a cafe | `workingLocation` (office label, home, custom) | Day strip |
| Birthdays calendar (6) | yearly all-day `birthday`, read-only | Home, month |
| Flight LHR to JFK, Hotel check-in | `fromGmail`: read-only | detail |

### Invitations and replies
| Event | My reply | Look at |
|---|---|---|
| Quarterly sync `w-inv-qsync` (D+2 16:00) | **needs reply**, Meet, organizer note in the description | Home "Reply needed", RSVP bar |
| Weekly product review `w-inv-weekly` (every Thursday) | **needs reply, repeating** (this event or the series?) | RSVP sheet |
| Intro with new vendor `w-inv-vendor` (D+3) | **needs reply**, 1 guest | Home banner count |
| Parents evening `f-inv`, Padel `p-padel-future` | needs reply (personal) | banner count |
| Platform roadmap review `t-inv-ro` (Team Platform, reader) | needs reply on a **read-only calendar**: no reply bar | detail |
| Demo day (optional) `w-inv-optional` | self is an **optional** guest, needsAction | detail People |
| Roadmap workshop, Standup, Design review... | accepted | |
| Hack session `w-inv-tentative`, Budget review | **maybe** | Day hatched, agenda "Maybe" |
| All-hands prep `w-inv-declined`, Vendor demo `w-vendor-demo` (today 14:00) | **declined**: faded and struck through | Day, agenda |

### Recurrence
| Series | State | Look at |
|---|---|---|
| Standup `w-standup`: weekdays 09:30 | every weekday; one occurrence cancelled, one moved and renamed | Day, edit "this / all" |
| Design crit `w-crit`: Tue and Thu 14:00 | one **moved** (+2 h), one **cancelled**, one **renamed** ("Q4 mocks"), one **EXDATE** | agenda, detail "Every Tuesday and Thursday" |
| Onboarding bootcamp: weekdays, COUNT=7 | **COUNT** series that ends in the middle of the window | agenda |
| Monthly demo day: last Friday | `BYDAY=-1FR` | month |
| Sprint planning: every 2 weeks, Monday | `INTERVAL=2` | agenda |
| Q3 launch war room | series **ended** last month (UNTIL) | past days only |
| Hackweek (5 days) and Mentoring | series **starting next week** | agenda next week |
| Company anniversary (yearly all-day), Invoice day (monthly all-day), Birthdays | yearly and monthly all-day | month |
| Climbing (Tue and Thu, personal), Pottery class (Wed), Platform weekly (Wed), On-call handover (Wed) | more series, with one moved/cancelled each | |

### Burst, hidden and read-only extras
Archive 2024 and Meeting rooms hold events that do not show until the calendar is switched on. Release train, Team Platform, On-call, Studio partner and Old client hold a few events each.

## Scenarios (sync states)
`scripts/run-local.sh scenario NAME [args]` calls the fake's admin API on `127.0.0.1:${FAKE_PORT:-8788}` and prints what it did and what to look for.
| Scenario | Does | Expected in the app |
|---|---|---|
| `reauth [email]` | revokes the account (default `stale@example.com`) | within 15 s the account is `needs_reauth`: Reconnect banner on Home and in Settings > Accounts; its events stay, marked stale; other accounts keep syncing |
| `reconnect [email]` | undoes the revocation and re-seeds the account in the hub | account `active` again (the app's Reconnect button also works: the fake approves at once) |
| `google-down [seconds]` | every call (API and token) answers 503 | Settings > Server and sync > Sync log shows failing runs; the hub backs off about a minute between retries; the account stays active (see below); creating or editing fails with a message and keeps the form |
| `rate-limit [seconds] [retryAfter]` | every call answers 429 + Retry-After | the hub waits and retries on its own; no error on the account; writes fail |
| `slow [ms]` | every answer is delayed | pull-to-refresh and the syncing indicator take longer; very large values time out |
| `recover` | clears outages, rate limit, slowness, failure rules, revocations | next sync succeeds. An account already `needs_reauth` stays so until `reconnect` or Reconnect in the app |
| `outside-move [account]` | moves the next upcoming timed event of the account's primary calendar +1 h | within 15 s the block moves and a "moved" notification/row word appears (notification: title and new time) |
| `outside-rename [account]` | appends " (renamed in Google)" to it | new title in every view and search |
| `outside-delete [account]` | deletes it | disappears; open detail says it was deleted; "cancelled" notification |
| `outside-add [account]` | new event "Added in Google web" in 10 min with a Meet link | appears live; Home next block and widget update |
| `expire-sync` | every sync token answers 410 | a full re-list on the next sync; nothing changes on screen, no error |
| `burst [n] [account]` | adds n (200) events over 30 days on the primary calendar | paging in the sync, month dots, agenda scroll, search "Burst" |
| `calendar-add [account]` | new calendar "Pop-up: Hack night" with 2 events | appears in Settings > Calendars after the next sync |
| `calendar-remove [account]` | removes the pop-up (else the first read-only calendar) | calendar and its events disappear, no error |
| `reset` | re-seeds the fake: dates re-based, everything above undone | next sync re-lists: burst/outside-add events vanish, deleted ones return, removed calendars return |
| `list` | what the fake holds, outages, delay, scenario names | |

Known hub behaviour to expect (observed): during `google-down` the account's status stays `active` with no `lastError` (only the sync log and `failCount` show it); `rate-limit` leaves no error record. These are the "Google being down is not visible in the app" gap noted in `docs/engineering/e2e.md`.

## Manual checklist
Tick each state with the item named. Do it on the phone with the demo hub.
- **Home, next block:** in progress (Incident review), join window (Acme call), 25 min (Pricing sync), "Done for today" (late evening, see above), an all-day-only day, the empty Saturday.
- **Home, Reply needed:** banner count (seven rows need a reply: Quarterly sync, Weekly product review, Intro with new vendor, Demo day (optional), Parents evening, Padel, and Platform roadmap review on a read-only calendar; check what the banner counts for the last two kinds), open Quarterly sync and the weekly product review (series sheet).
- **Month:** dots on the busy day (D+1), multi-day offsite, the empty Saturday and the all-day Sunday, 200 days out, 300 days back (search only), DST dates.
- **Agenda:** day grouping across time zones (Tokyo 07:30, Kiritimati), cancelled hidden, declined faded, series with exceptions, scrolling a burst.
- **Day:** busy day D+1 (back-to-back, 4-way overlap), 5-min and 15-min, 10-hour, crossing midnight, all-day strip with 6 items, empty Saturday ("Nothing planned"), tentative hatch, free events, now line.
- **3 days:** overlaps narrow, D0 to D+2.
- **Event detail:** Meet / Zoom / Teams / Webex / dial-in only / no link; long title, no title, long description with links, HTML text, location URL, 12 guests, organizer other/me, read-only Team and Birthdays events, `fromGmail`, working location, out of office, birthday.
- **Editor:** edit an event you own and one with `guestsCanModify`; the 120-char title; move between calendars (Work to On-call rota); the very long calendar name in the picker; add Meet to "Whiteboard session".
- **RSVP:** Yes, Maybe (note), No (note) on Quarterly sync; the repeating one (this / series); the read-only invitation shows no reply bar; the optional-guest invitation.
- **Move:** drag in 3 days, then `outside-move` while moving (conflict).
- **Delete and undo:** delete a single event and Undo; delete one occurrence of Design crit and the series.
- **Settings > Calendars:** hidden Archive, unselected Meeting rooms, the long name, `calendar-add`/`calendar-remove`.
- **Settings > Server and sync / Accounts:** `reauth` and Reconnect, `google-down`, `rate-limit`, `slow`, sync log, `expire-sync`.
- **Quick add:** "lunch with Sam friday 1pm" next to the busy day; **Search:** "Koya", "kaigi", "مرحبا", "Zoe", "renamed", "Burst", "300 days".
- **Widgets and notifications:** next-event widget with Acme call and the long title; notification for an event in 8 min; `outside-move` and `outside-delete` notification texts; `outside-add` live update.
- **Offline:** stop the hub (Ctrl-C), see the banner and saved rows, restart (it keeps the data in `.local/demo/`).
