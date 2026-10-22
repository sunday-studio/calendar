# Personal Calendar Hub — Plan

_Based on [RESEARCH.md](RESEARCH.md) (rev 3) · 2026-10-10_

## Vision

Replace Notion Calendar with **my own calendar**:
- my **home server** connects to all my accounts, keeps everything in sync, and exposes **my own API**;
- a **custom-UI mobile app** shows what's coming up and lets me act on events;
- my own **scripts, automations and agents** can use the calendar too.

## Assumptions (change if wrong)

| Decision | Assumed | Alternative |
|---|---|---|
| App platform | **Expo (React Native)**, iPhone first, Android possible later | SwiftUI (iPhone only) or a PWA |
| Server stack | **TypeScript** (Hono) + SQLite in Docker | Go |
| Remote access | **Tailscale** on the server + phone (`tailscale serve` for HTTPS) | Cloudflare Tunnel + own domain |
| Accounts | Personal Google (1+), Outlook.com, iCloud | Work Google Workspace / M365 may need admin approval |
| Sync method | Polling with sync tokens (~60 s) | Webhooks later |

## Persona

**Me:** one user, several calendar accounts, a home server, and a preference for owning my data and tooling.

---

## Milestones at a glance

| # | Milestone | Outcome | Epics |
|---|---|---|---|
| **M0** | Foundations | Server reachable from the phone over HTTPS; provider apps registered | E0 |
| **M1** | Connected | Google, Microsoft and iCloud accounts connected through the app | E1 |
| **M2** | Synced | All calendars and events mirrored on the server and kept fresh | E2 |
| **M3** | Read MVP | The app shows my merged agenda and event details ← **can start using daily** | E3, E4 (read parts) |
| **M4** | Act | Create / edit / move / delete / RSVP / Meet link from the app | E5 |
| **M5** | Glanceable | Widgets, reminders, week view with drag & drop | E6 |
| **M6** | Automate | API tokens, live stream, quick-add, automations/agent | E7 |
| **M7** | Cut over | Notion Calendar removed | E8 |

---

## E0 — Foundations

**US-0.1 · Reach my server securely**
As the owner, I want my phone to reach my home server over HTTPS without exposing it to the internet, so that my calendar data stays private.
- [ ] Tailscale on the server + phone; `tailscale serve` gives `https://<server>.<tailnet>.ts.net` a valid cert
- [ ] The hub runs in Docker Compose and restarts automatically
- [ ] `GET /health` returns 200 from the phone over Tailscale, and fails off-tailnet

**US-0.2 · Register my own Google app**
As the owner, I want my own Google OAuth app, so that I can sign in to Google from my app instead of Notion's.
- [ ] Google Cloud project with the Calendar API enabled
- [ ] Consent screen is External and **In production** (not Testing → avoids 7-day token expiry)
- [ ] Web OAuth client with the redirect `https://<server>.ts.net/auth/google/callback` accepted by Google
- [ ] Client ID/secret stored in the server's env/secrets, not in git

**US-0.3 · Register my own Microsoft app**
As the owner, I want my own Entra ID app registration, so that I can connect Outlook/Microsoft accounts.
- [ ] Supports personal + org accounts; Web redirect `…/auth/microsoft/callback`
- [ ] Delegated scopes `Calendars.ReadWrite offline_access User.Read`
- [ ] Client secret expiry date recorded (a reminder event!)

**US-0.4 · Skeleton projects**
As the developer, I want a server and app skeleton with config, lint and tests, so that features land on a clean base.
- [ ] `server/` (Hono + SQLite + migrations) and `app/` (Expo) folders
- [ ] Token encryption key loaded from env; the server refuses to start without it

---

## E1 — Connect accounts

**US-1.1 · Connect a Google account**
As a user, I want to tap "Connect Google" and sign in, so that my Google calendars appear in my app.
- [ ] The app opens `…/auth/google/start` in a system browser sheet
- [ ] I see Google's consent screen (the one-time "unverified app" warning is expected)
- [ ] The server exchanges the code with `access_type=offline` and stores the **encrypted refresh token**
- [ ] The browser redirects to `calendarapp://connected` and the app shows the account email
- [ ] Connecting the same account twice updates it rather than duplicating it

**US-1.2 · Connect multiple Google accounts**
As a user with personal and side-project Google accounts, I want to connect more than one, so that I see them all together.
- [ ] Each account is listed separately with its email
- [ ] The Google account chooser shows (`prompt=select_account consent`)

**US-1.3 · Connect a Microsoft account**
As a user, I want to connect Outlook/Microsoft 365, so that those events appear too.
- [ ] Same flow as US-1.1 against Microsoft identity
- [ ] If a work tenant blocks consent, the app shows a clear "needs admin approval" message

**US-1.4 · Connect iCloud**
As a user, I want to connect iCloud with an app-specific password, so that my Apple calendars are included.
- [ ] The form asks for the Apple ID email + app-specific password, with a link/explanation of where to create one
- [ ] The server validates by discovering calendars over CalDAV before saving
- [ ] Wrong credentials → an inline error, nothing saved

**US-1.5 · Manage connected accounts**
As a user, I want to see, reconnect and remove accounts, so that I stay in control.
- [ ] The accounts screen lists the provider, email, status (`ok` / `needs_reauth`) and last sync time
- [ ] **Reconnect** reruns sign-in; **Remove** deletes the tokens + mirrored data for that account
- [ ] If a token is revoked or expires, the account is marked `needs_reauth` and the app shows a banner

---

## E2 — Sync engine (server)

**US-2.1 · Mirror my calendar list**
As a user, I want all calendars from every account listed with their colours, so that I can choose what to see.
- [ ] Google `calendarList`, Graph `/me/calendars`, iCloud PROPFIND → `calendars` table
- [ ] Read-only vs writable is recorded per calendar

**US-2.2 · Initial full sync**
As a user, I want my events loaded right after connecting, so that the app is useful immediately.
- [ ] Window: today − 30 days → today + 365 days
- [ ] Recurring series + exceptions are stored; instances are expandable for any range
- [ ] Times stored as UTC + original TZID; all-day events stored as dates

**US-2.3 · Stay fresh**
As a user, I want changes I make elsewhere (e.g. Google on my laptop) to show up within about a minute.
- [ ] A polling loop of ~60 s per account using Google `syncToken`, Graph `deltaLink`, CalDAV `ctag`/`etag`
- [ ] Google **410 Gone** → full resync of that calendar
- [ ] The Graph delta window rolls forward daily
- [ ] Deletions/cancellations are reflected

**US-2.4 · Resilient sync**
As the owner, I want sync to survive outages and rate limits, so that it doesn't need babysitting.
- [ ] Exponential backoff on 429/5xx; one failing account doesn't block the others
- [ ] `sync_log` records each run (account, duration, changes, error)
- [ ] `GET /status` shows the last successful sync per account

---

## E3 — See what's coming up (app)

**US-3.1 · Up next**
As a user, when I open the app I want to see my next event immediately, so that I know what's coming.
- [ ] A top card with the next event: title, time, countdown, location
- [ ] A **Join** button appears when a meeting link exists and it starts within 10 min (or is in progress)

**US-3.2 · Merged agenda**
As a user, I want one list of upcoming events from all accounts, grouped by day, so that I see my week at a glance.
- [ ] `GET /agenda?days=14` from the server; Today / Tomorrow / weekday headers
- [ ] Each event shows its calendar colour, time, title and an RSVP state indicator
- [ ] All-day events are pinned at the top of each day
- [ ] Pull to refresh; loads in under 1 s from the server cache

**US-3.3 · Day view**
As a user, I want a timeline of a day, so that I can see gaps and overlaps.
- [ ] Hour grid, overlapping events side by side, a current-time line
- [ ] Swipe between days; tap a date in the header to jump

**US-3.4 · Event details**
As a user, I want to open an event and see everything about it.
- [ ] Title, time + timezone, calendar/account, location (tap → Maps), description, attendees + their status, join link, recurrence summary

**US-3.5 · Offline glance**
As a user, I want to see my agenda even without connectivity.
- [ ] The last agenda is cached on the device and shown with a "last updated" note

---

## E4 — Calendar settings (app)

**US-4.1 · Show/hide calendars**
As a user, I want to toggle which calendars appear, so that noisy calendars (holidays, birthdays) don't clutter my view.
- [ ] Calendars are listed per account with toggles; stored on the server (`visible`)
- [ ] The agenda/day views respect the toggles

**US-4.2 · Custom colours**
As a user, I want to set my own colour per calendar, so that the UI matches how I think.
- [ ] Colour override stored on the server; defaults to the provider's colour

**US-4.3 · Default calendar**
As a user, I want to choose which calendar new events go into by default.

---

## E5 — Act on events

**US-5.1 · Create an event**
As a user, I want to create an event in any writable calendar.
- [ ] Fields: title, start/end or all-day, calendar, location, notes, reminder
- [ ] The server writes to the provider first, then updates the mirror; the app shows it immediately
- [ ] Provider error → a clear message, nothing half-saved

**US-5.2 · Edit an event**
As a user, I want to change any field of an event I own.
- [ ] For a recurring event, choose **this event / this and following / all events**
- [ ] Conflict (the event changed elsewhere, etag mismatch) → refetch and show the latest version

**US-5.3 · Delete an event**
- [ ] Confirmation sheet; recurring options as in US-5.2
- [ ] Deleting a meeting I organise notifies attendees (provider default)

**US-5.4 · RSVP**
As a user, I want to accept / decline / maybe an invitation from my app.
- [ ] Google: patch my attendee `responseStatus`; Graph: `accept/decline/tentativelyAccept`
- [ ] An optional note to the organiser
- [ ] iCloud invitations: show the status read-only (no reliable RSVP)

**US-5.5 · Add a video link**
As a user, I want to add a Google Meet (or Teams) link to an event with one tap.
- [ ] Google: `conferenceData.createRequest` (requires `conferenceDataVersion=1`)
- [ ] Microsoft work accounts: `isOnlineMeeting: true`; the option is hidden where unsupported

**US-5.6 · Join a meeting**
As a user, I want one tap to join the meeting from the agenda or event detail.
- [ ] Join URL from structured conference data, falling back to links parsed from the location/description (Meet, Zoom, Teams, Webex)

**US-5.7 · Duplicate / move to another calendar**
- [ ] Duplicate an event; move it between calendars of the same account (Google `move`), or across accounts (create + delete, with a warning)

---

## E6 — Glanceable & comfortable

**US-6.1 · Home-screen widget**
As a user, I want a widget with my next events, so that I don't have to open the app.
- [ ] Small: the next event + countdown; Medium: the next 3–4 events today
- [ ] Refreshes from the server cache; tap opens that event

**US-6.2 · Reminders**
As a user, I want reliable notifications before events.
- [ ] The app schedules local notifications from the synced agenda (default 10 min, per-event override)
- [ ] The notification has a **Join** action when a link exists

**US-6.3 · Week / 3-day view with drag & drop**
As a user, I want to drag events to reschedule and resize them to change their duration.
- [ ] Long-press to drag; snap to 15 min; resize from the bottom edge
- [ ] Optimistic update, rolled back if the server write fails

**US-6.4 · Next meeting on the lock screen** _(iOS Live Activity / Android ongoing notification, nice-to-have)_

**US-6.5 · Multiple timezones** _(nice-to-have, a Notion Calendar feature)_
- [ ] A secondary timezone column in the day/week views

---

## E7 — Automation & API (home server)

**US-7.1 · API access for my scripts**
As the owner, I want personal API tokens, so that my scripts and automations can use the calendar.
- [ ] Create/revoke tokens per device/script, optionally read-only
- [ ] Documented endpoints: `/agenda`, `/events` CRUD, `/events/:id/rsvp`, `/events/:id/meet`, `/freebusy`

**US-7.2 · Live updates**
As a client (app or dashboard), I want to be pushed changes instead of polling.
- [ ] `GET /stream` (SSE) emits `event.created|updated|deleted` and `account.status`

**US-7.3 · Quick add (natural language)**
As a user, I want to type "lunch with Sam fri 1pm at Kōya" and get a proper event.
- [ ] `POST /quick-add {text}` → parsed preview → confirm → create
- [ ] Parsing is handled by **alfather** (a local AI system, reached over MCP), with a deterministic fallback for simple phrases

**US-7.4 · Find a free slot**
As a user, I want to ask "when am I free for 2h this week?"
- [ ] `/freebusy` over all visible calendars, respecting working hours

**US-7.5 · Automations**
As the owner, I want server-side hooks, e.g. "on event start → run Home Assistant scene" or "daily 7am summary to my phone (ntfy)".
- [ ] Simple rules or a webhook-out on event lifecycle triggers

**US-7.6 · Agent access via alfather**
- [ ] All AI features go through **alfather**, a local AI system, via MCP; the hub exposes its API as MCP tools so alfather can read and plan my calendar

---

## E8 — Cut over from Notion Calendar

**US-8.1 · Parity checklist**
As a user, I want to confirm my app covers what I use daily in Notion Calendar before switching.
- [ ] List the features I actually use; each is either covered or consciously dropped

**US-8.2 · Remove Notion's access**
- [ ] Revoke Notion Calendar in Google account → Security → Third-party access, and in the Microsoft account → Apps
- [ ] Uninstall Notion Calendar

---

## Non-functional requirements

| Area | Requirement |
|---|---|
| **Security** | The server is reachable only over Tailscale; refresh tokens / app passwords encrypted at rest; API tokens hashed; no secrets in git |
| **Privacy** | No third-party services hold calendar data; AI runs locally through alfather (MCP) |
| **Freshness** | Remote changes visible in ≤ 2 min; my own writes visible immediately |
| **Performance** | Agenda API < 200 ms on the server; the app shows cached data instantly |
| **Reliability** | Sync survives restarts (tokens/sync state in SQLite); nightly SQLite backup |
| **Observability** | `/status` page + `sync_log`; alert (ntfy) when an account needs reauth |

## Risks

| Risk | Mitigation |
|---|---|
| A work M365 / Workspace admin blocks consent for unverified apps | Ask the admin, or fall back to a read-only published ICS feed for that account |
| Google consent left in "Testing" → tokens die every 7 days | US-0.2 acceptance criteria; the reauth banner catches it |
| Microsoft client secret expires | Calendar reminder; the `/status` warning 30 days before |
| iCloud app password revoked silently | Mark `needs_reauth` on 401 and alert |
| Recurring-event edge cases (exceptions, TZ/DST) | Use `rrule` + `ical.js`; fixture-based tests from real exported events |
| iOS app install expires every 7 days on a free Apple ID | Apple Developer Program ($99/yr) or EAS + TestFlight; or Android/PWA |

## Suggested order of work

1. **M0 → M3** (foundations, connect, sync, read-only app) is the first usable version. Use it alongside Notion Calendar.
2. **M4** (actions) is when you can stop opening Notion Calendar for most tasks.
3. **M5–M6** in any order, depending on whether glanceability or automation matters more.
4. **M7** once the parity checklist is green.
