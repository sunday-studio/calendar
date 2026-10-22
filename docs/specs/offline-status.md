# Offline and status (boards y-offline, y-offedit, y-down, y-diag, y-status, y-log, y-tokens, y-newtoken)

Notes: *Offline: the saved agenda opens and a banner says how old it is. Reading works. Editing is paused. If the server is unreachable a checklist finds the cause. Server and sync shows each account and secret expiry. API tokens let scripts, alfather and this phone in.* Stories 3.5, 2.4, 7.1.

## Connection states (what the app is in)
| State | Trigger | Header | Banner (`Banner`) | Content |
|---|---|---|---|---|
| **Online** | hub reachable, SSE open | normal | none | normal |
| **Offline** (y-offline) | no network path on the phone | normal | amber: **"Offline."** (Medium) " Showing your saved agenda from 10:19." action **"Retry"** | saved agenda unchanged; Next block normal; **FAB dimmed to 35%** and disabled |
| **Server unreachable** (y-down) | network ok, hub request fails/times out 3× | normal | **red** (`kind: .error`): **"Can't reach your server."** " This agenda is from 07:12 and may be out of date." action **"Check"** → diagnostic screen | **Next block in its quiet variant but with flip cells**: `nx q` card (surface, hairlines) with `FlipCells("10:30")`, title, footer dim "in 8 min · saved copy" (no Join); every row gets the status word **"Stale"** (dim); sub 16 dim under the list: "Alerts already scheduled on this phone will still arrive." |
| **Needs sign in** | an account is `needs_reauth` | normal | amber (home spec h-banner) | that account's rows "Stale" |

`HomeLogic.hero` needs a `stale` flag so the quiet-with-cells block exists; the quiet card therefore accepts an optional time: `QuietCard(time:, title:, footer:)` ★ (added in CalendarUI).
Banner sits **directly under the header** (margin-top 16/18); the Next block follows with margin-top 0 and no top border when a banner is shown (h-banner). Only one banner at a time; priority: unreachable > offline > needs sign in.
Age text: "from 10:19" same day, "from yesterday 21:40", "from Sat 10 Oct" otherwise.

## Offline editing (y-offedit)
Event detail while offline/unreachable: back, **"More" dimmed (ink2)**, `Banner` (amber): **"You're offline."** " You can read this event. Editing, replying and deleting need a connection to your server." (no action). Join/Map still work (Map needs no hub). Any tap on a disabled write control (More, Reply bar, FAB, drag) shows `ToastView("Can't edit while offline", "OK")` at bottom 96, auto-dismiss 4 s. **Writes are not queued** (the canvas says "Editing waits for the connection"; this also settles open question 2 of the old design README).

## Diagnostic checklist (y-diag)
Push from the banner "Check", or from Settings › Server and sync. `ScreenNav("Server and sync")`; red `Banner`: **"hub.tail3a9f.ts.net didn't answer."** " Last good contact 07:12."; label "Checking"; three `ChecklistRow`s (h58): "This phone has internet" **Yes** (ok) · "Tailscale is connected" **Off** (bad) · "Server answers" **Not tried** (none); sub 16 dim with the next step ("Turn Tailscale on for this phone, then try again."); `FooterBar`: primary **"Open Tailscale"** (only when Tailscale is the failing step; otherwise omitted) + outline **"Try again"**. Steps run in order and stop at the first failure; Try again re-runs all three. Row states while running: Working (amber).

## Server and sync (y-status)
`ScreenNav("Server and sync")`. Label "Server": `KeyValueRow` **Address** (value + `StatusLabel(ok, "Reachable")` right), **Version** "0.3.1 · up 12 days". Label "Accounts": ★two-line `ListRow` per account (bar x=0, email 19, sub 14 dim "Last sync 1 min ago · 212 events" / iCloud "… · polls every 5 min", trailing `StatusLabel` OK / Sign in(bad) / Syncing(working)); tapping a bad row opens the account (reconnect). Label "Secrets": `KeyValueRow` "Microsoft client secret" value "Expires 23 Oct" with right-aligned amber "12 days" (amber when < 14 days, red when expired). `FooterBar`: outline **"Sync now"** (spinner text "Syncing…" 35% while running; result toast "Synced 3 accounts"). Entry to log: tap an account row's sub line → Sync log.

## Sync log (y-log)
`ScreenNav("Sync log")`, sub 16 dim account email; label by day ("Today"); rows `.r.r2` (h72): mono 15 dim **time** (48 col), title 19 ("Checked, 0 changes", "3 changes", "Sign in expired"), second line 14 dim ("214 ms", "1 moved, 1 added, 1 removed · 402 ms"; **err red** for errors: "Microsoft returned invalid_grant"). **Quiet checks (0 changes) group**: consecutive "Checked, 0 changes" rows collapse into "Checked 14 times, 0 changes" with the span as the sub line. Hint 13 dim: "Checks run about once a minute. Quiet checks are grouped." Data: `/v1/sync/runs` (SyncRun exists).

## API tokens (y-tokens, y-newtoken)
List: `ScreenNav("API tokens", trailing: "New")`; sub 16 dim "Tokens let this phone, your scripts and alfather use your calendar through your server."; label "Active"; `r2` rows: name 19 + sub 14 dim "Read and write · used just now" / "Read only · not used in 9 days"; trailing `CalTag("Yours")` on this phone's own token (not revocable here) or chevron → token detail (revoke = `CalButton(.danger)` "Revoke token", confirm sheet "Revoking signs that device or script out straight away."); hint 13 dim "Revoking a token signs that device or script out straight away."
New token (modal, `ScreenNav("New token", leading: .close)`): `LabeledField("Name")` (focused); label "Access" ★`SegmentedControl` Read only · Read and write (default); label "Expires" segments 30 days · 1 year · Never; on **Create** (primary in the footer, replaced by) the reveal state: `KeyValueRow("Token, shown once", token)` with the token in **mono 18**, hint 13 dim "Copy it now. Calendar cannot show it again."; `FooterBar`: primary **"Copy token"** (toast "Copied") + outline **"Done"**. **Copy deviation:** the canvas sample shows `gn_live_…`; the real prefix is `cal_` (DECISIONS T01).

## Components (CalendarUI)
Existing: `Banner`(`AmberBanner`), `ChecklistRow`, `StatusLabel`, `KeyValueRow`, `ListRow`, `ToastView`, `FooterBar`, `LabeledField`, `CalTag`, `ScreenNav`, `Fab` (add `isEnabled` dimming: 35%). New ★: `QuietCard(time:…)` option, `SegmentedControl` (shared), `DimmedTextButton` behaviour for the "More" action via `.disabled`.

## Done means
Offline, unreachable, offline-detail, diagnostic, status, log, tokens list and new-token (reveal) in dark and light, on top of the fixture hub with the hub stopped/started, match the y-* boards: banner copy and colours, "Stale" words, dimmed FAB and More, flip-cell quiet card.
