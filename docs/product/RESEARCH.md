# Personal Calendar Hub — Research

_Researched 2026-10-10 · revision 3 (replacing Notion Calendar; signing in to accounts is OK)_

## The goal

- **Replace Notion Calendar** with my own app and a **custom UI**.
- **Sign in to my accounts inside the app** (like Notion Calendar does).
- **My home server is the brain.** It holds the connections, syncs everything, and exposes **my own API**, so my scripts, automations and agents can read and write my calendars.
- I can see what's coming up easily, and I can act on events: create, edit, move, delete, RSVP, join calls, add a Meet/Teams link.

---

## TL;DR — architecture

This is how Notion Calendar works too: it has its own server holding your account connections. Here, **your home server plays that role.**

```
                    ┌──────────────── Home server (Docker) ────────────────┐
 Google Calendar ◄──┤  connectors: Google API · Microsoft Graph · iCloud    │
 Outlook / M365  ◄──┤  sync engine (incremental, every ~1 min)             │
 iCloud (CalDAV) ◄──┤  SQLite mirror of all events                        │
                    │  token vault (encrypted refresh tokens)             │
                    │  YOUR API  (REST + live updates)                    │
                    └───────▲──────────────────────────▲──────────────────┘
                            │ Tailscale (private)      │
                   Mobile app (custom UI)        your scripts / automations /
                   "Connect Google" button        Home Assistant / AI agent
```

- **Sign-in:** tap *Connect Google* in the app → Google's normal sign-in page opens → you approve → Google hands an authorization code **to your home server** → the server stores a refresh token. The phone never holds the provider tokens.
- **Sync:** the server keeps a mirror of every calendar and event, pulling only changes, so it's cheap.
- **Actions:** the app (or a script) calls *your* API → the server writes to Google/Outlook/iCloud immediately → the mirror updates.
- **No phone bridge needed.** The server works 24/7 even when your phone is off, on iPhone or Android.

---

## 1. The one-time "developer" setup (why Notion didn't need it, and why you do)

When you sign in with Google inside Notion Calendar, you're approving **Notion's** registered app. For your own app, you register your own app once. It's free, takes about 15 minutes, and **no review is needed for personal use.**

### Google (Gmail / Google Workspace calendars)
1. Create a project in Google Cloud Console → enable **Google Calendar API**.
2. Set up the OAuth consent screen: User type **External**, then set the **publishing status to "In production"**.
   - ⚠️ If you leave it in **"Testing"**, refresh tokens **expire every 7 days** and you'd have to sign in again every week.
   - "In production" without verification means you'll see a **"Google hasn't verified this app"** screen once per sign-in (click *Advanced → Continue*). Unverified apps are capped at 100 users, which is irrelevant for you.
3. Create an OAuth client of type **Web application**. Redirect URI: `https://<server>.<tailnet>.ts.net/auth/google/callback`
   - Google requires HTTPS, no raw IPs, and a domain on the Public Suffix List. **`ts.net` is on the list** (checked), so Tailscale's built-in HTTPS (`tailscale serve`) satisfies this. Confirm when you register it.
4. Scopes: `https://www.googleapis.com/auth/calendar` (full read/write incl. calendar list) + `openid email`. Request `access_type=offline` + `prompt=consent` to get a refresh token.
5. Workspace (work) accounts: the admin may block unverified third-party apps. Personal Gmail is fine.

### Microsoft (Outlook.com / Hotmail / Microsoft 365)
1. Go to the Azure portal → **Entra ID → App registrations → New**. Supported accounts: *"Any org directory + personal Microsoft accounts"*.
2. Platform **Web**, redirect `https://<server>.<tailnet>.ts.net/auth/microsoft/callback`, and create a client secret (it expires, so put the renewal in your calendar 🙂).
3. Delegated scopes: `Calendars.ReadWrite`, `offline_access`, `User.Read`.
4. Personal Outlook.com accounts work straight away. **Work M365 tenants** often block user consent for unverified apps. You may need your IT admin, or accept read-only via a published ICS link.
5. Don't use **EWS**. Microsoft blocks it by default from 1 Oct 2026 and removes it on 1 Apr 2027.

### iCloud
- **No OAuth exists.** Apple has no calendar API. In your app, the "Connect iCloud" screen asks for your Apple ID email + an **app-specific password** (generated at appleid.apple.com → Sign-In & Security). The server stores it and talks **CalDAV** to `caldav.icloud.com`.
- Read and write both work. There's no push, so the server polls (every 1–5 min is fine).

---

## 2. What the server can do per provider

| Capability | Google Calendar API | Microsoft Graph | iCloud CalDAV |
|---|---|---|---|
| List calendars, colours | ✅ `calendarList` | ✅ `/me/calendars` | ✅ PROPFIND |
| Incremental sync | ✅ `syncToken` per calendar | ✅ `calendarView/delta` (**one calendar + one date range per delta**) | ⚠️ `ctag`/`etag` comparison (sync-collection if supported) |
| Create / edit / delete / move | ✅ | ✅ | ✅ (you write iCalendar text) |
| Recurring events & exceptions | ✅ (`singleEvents=true` to expand) | ✅ | ✅ RRULE (you expand it, e.g. `rrule` lib) |
| **RSVP** | ✅ patch your attendee `responseStatus` | ✅ `POST /events/{id}/accept\|decline\|tentativelyAccept` | ⚠️ limited |
| **Add video link** | ✅ Meet via `conferenceData.createRequest` | ✅ Teams via `isOnlineMeeting` (work accounts) | ❌ |
| Free/busy, "find a time" | ✅ `freebusy` | ✅ `getSchedule` | Compute from your mirror |
| Push changes to server | ✅ webhooks (need a **public** HTTPS URL) | ✅ subscriptions (public URL) | ❌ poll |
| Cost | Free (generous quota) | Free | Free |

**Push vs poll:** webhooks need a public HTTPS endpoint (Tailscale **Funnel** or a Cloudflare Tunnel), and channels expire and must be renewed. **Start with polling every ~60 s using sync tokens.** It's simpler and nearly real-time, and you write changes yourself, so they show up instantly anyway. Add webhooks later if you want.

---

## 3. Home server — design

**Suggested stack:** TypeScript (Hono or Fastify) **or** Go · SQLite · Docker Compose · Tailscale sidecar (`tailscale serve` for HTTPS).
Useful libraries: `googleapis`, `@microsoft/microsoft-graph-client`, `tsdav` (CalDAV), `ical.js`, `rrule`.

**Tables**
- `accounts`: provider, email, encrypted refresh token / app password, status (`ok`, `needs_reauth`)
- `calendars`: account, provider id, name, colour, writable, **visible** (your toggle), sync token / delta link / ctag
- `events`: provider id + iCal UID, calendar, title, start/end, tz, all-day, location, description, conference/join URL, attendees + my RSVP status, recurrence, etag, updated_at, deleted
- `sync_log`: for debugging

**Your API (examples)**
- `GET /agenda?days=7`: merged upcoming events across all visible calendars ← what the app's home screen uses
- `GET /events?from&to&calendar`, `GET /events/:id`
- `POST /events`, `PATCH /events/:id`, `DELETE /events/:id`
- `POST /events/:id/rsvp {status}`, `POST /events/:id/meet`
- `GET /freebusy?from&to`, `POST /quick-add {text}` (natural language → event, optionally via an LLM)
- `GET /stream` (SSE): live updates to the app
- `GET /auth/{google|microsoft}/start`, `/callback`, and `POST /auth/icloud`

**Security:** reach the server only over Tailscale. Encrypt tokens at rest (a key from env or a file). Use a per-device API token for your app and scripts.

**Gotchas to handle**
- Google `syncToken` can return **410 Gone**. Do a full resync of that calendar when it does.
- Microsoft delta covers one calendar + date window at a time, so roll the window forward (e.g. today − 30 d → today + 365 d).
- Recurring events: store the series + exceptions, and expand to instances for the agenda.
- Timezones: store UTC + the original TZID. All-day events are dates, not datetimes.
- Revoked access → mark the account `needs_reauth` → show a "Reconnect" banner in the app.

---

## 4. Mobile app — custom UI

The app is a **thin client of your home server.** It holds no provider logic.

| Option | Pick if… |
|---|---|
| **Expo (React Native)** | You want iPhone + Android with one codebase and fast iteration. Widgets need a small native target. |
| **SwiftUI** | iPhone only, and you want the best widgets, Live Activities and Siri/Shortcuts (App Intents). |
| **PWA (web app)** | You want the fastest start: served from the home server, added to the home screen. Weaker widgets and notifications. |

**Sign-in inside the app:** "Connect Google" opens `https://<server>/auth/google/start` in a system browser sheet (`ASWebAuthenticationSession` / Custom Tabs, via `expo-web-browser`). After approval the server redirects to `yourapp://connected`, and the sheet closes.
→ The phone must be on Tailscale while connecting, because the redirect goes to your server.

**Screens to replace Notion Calendar**
- **Up next / agenda:** merged list, with a big "Join" button when a meeting is within 10 min
- **Day / 3-day / week timeline**, with drag to move and drag to resize
- **Quick add** (natural language)
- **Event detail:** edit, RSVP, add a Meet link, copy the join link, delete
- **Calendars:** show/hide per calendar, colours, accounts + reconnect
- **Widgets:** next event, today's list. **Notifications:** reminders before events (from the server via ntfy, or local notifications scheduled by the app)

Notion Calendar features worth copying later: multiple timezones, sharing availability / booking links (your server can host a booking page), and a keyboard-driven command palette (a desktop/web version of your UI).

---

## 5. Build plan

| Phase | Deliverable |
|---|---|
| **0. Setup** | Tailscale on the server + phone, `tailscale serve` HTTPS; Google Cloud project + OAuth client (**In production**); Entra app registration |
| **1. Connect** | Server: OAuth flows for Google + Microsoft, iCloud app-password form, encrypted token vault |
| **2. Sync** | Calendar lists → initial full event sync → incremental polling loop → SQLite mirror |
| **3. API** | `/agenda`, events CRUD, RSVP, Meet link, SSE stream, device tokens |
| **4. App v1** | Agenda + day view + event detail + quick add + connect-accounts screen |
| **5. Polish** | Week view with drag & drop, widgets, reminders/notifications, calendar toggles |
| **6. Automations** | Scripts / Home Assistant / an LLM agent on top of your API; optional webhooks via Funnel |
| **7. Migrate off Notion Calendar** | Revoke Notion Calendar's access in each Google/Microsoft account's security settings |

---

## 6. Still to decide

1. **App platform:** iPhone only (SwiftUI), iPhone + Android (Expo), or start with a PWA?
2. **Which accounts:** how many Google accounts? Any work Google Workspace / Microsoft 365 (admin approval risk)? iCloud?
3. **Server stack:** TypeScript or Go? What's already running on the home server (Docker? Home Assistant?)
4. **Remote access:** OK to put Tailscale on the phone (recommended), or do you want a public domain via Cloudflare Tunnel?

---

## Sources
- [Google: Manage OAuth clients (redirect URI rules: HTTPS, no IPs, Public Suffix List)](https://support.google.com/cloud/answer/15549257)
- [Public Suffix List](https://publicsuffix.org/list/public_suffix_list.dat) (`ts.net` present, checked 2026-10-10)
- [Google: sensitive scope verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification)
- [Google: Testing-mode refresh tokens expire after 7 days](https://developers.google.com/health/setup)
- [Google Calendar API: push notifications (HTTPS, valid cert, channel expiry)](https://developers.google.com/workspace/calendar/api/guides/push)
- [Microsoft Graph: event delta](https://learn.microsoft.com/en-us/graph/api/event-delta) · [delta query for events](https://learn.microsoft.com/en-us/graph/delta-query-events)
- [EWS retirement timeline (Petri)](https://petri.com/microsoft-exchange-web-services-2026/)
- [iCloud via CalDAV + app-specific passwords (Nylas)](https://developer.nylas.com/docs/cookbook/calendar/apple-calendar-api/)
- [Expo Calendar SDK](https://docs.expo.dev/versions/latest/sdk/calendar/)
