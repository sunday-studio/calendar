# Smoke checklist (`make smoke`)

Run before a release, against a real hub and **real Google accounts** (one personal Gmail, one Workspace if you have it). No fake can cover these.
Tick each line; note anything surprising in docs/process/DECISIONS.md section B.

## Install
- [ ] `hub doctor` shows no FAIL.
- [ ] Phone (on the tailnet) pairs; pairing screen shows reachable, TLS ok, token ok, version ok.
- [ ] Off the tailnet the app says Unreachable (not a crash) and recovers when you reconnect.

## Accounts and sync
- [ ] Add each Google account: sign-in sheet, return to the app, first sync finishes, events appear.
- [ ] Edit an event in Google web: it appears on the phone within about 2 minutes (about 1 minute with the app open).
- [ ] Delete an event in Google web: it disappears.
- [ ] A repeating event with a changed single occurrence and an excluded date looks the same as in Google web.
- [ ] An all-day event shows on the right day (also around midnight and after changing the Time zone setting).
- [ ] A birthday / holiday calendar (read-only) shows and cannot be edited.
- [ ] Revoke the app in the Google account security page: the account turns to "Sign in needed", a push arrives, Reconnect fixes it.

## Writes (verify each in Google web)
- [ ] Create a timed event with a guest and a Meet link; the guest gets the invitation.
- [ ] Create an all-day event and a repeating event.
- [ ] Edit title, time, location. Edit one occurrence, "this and following", "all".
- [ ] Long-press an event on Day or 3 days and drag it to a new time (15-minute snap; on 3 days also to another day); Google updates within seconds. Cut the network mid-drag: it snaps back with an error. All-day events are not draggable. Resize with the bottom handle.
- [ ] RSVP to an invitation with a note; the organizer sees it. Reply to one occurrence of a repeating invite.
- [ ] Add Meet to an existing event. Move an event to another calendar, and (with a confirm) to another account.
- [ ] Delete one occurrence, following, all. Double-tap Save: exactly one event is created.
- [ ] Edit the same event in Google web first, then save in the app: the "Changed elsewhere" screen shows the Google version.
- [ ] Turn off Wi-Fi, save: a clean failure, nothing half-saved anywhere.

## Notifications and glance
- [ ] Reminder fires 10 minutes before an event with a working Join action; airplane mode does not stop it.
- [ ] Move an event in Google web: the phone alerts "moved to ..." without the title being in the push payload (check the hub log: payload has ids only).
- [ ] Widgets show the right next event after a sync with the app closed.
- [ ] Morning summary arrives at the chosen time (Settings, Alerts) and again not twice.

## Assist and automation
- [ ] Quick add "lunch with Sam fri 1pm at Kōya": draft is right, ambiguities are asked, nothing is created until Create.
- [ ] Find free time for 2 hours this week: slots respect working hours and hidden calendars.
- [ ] Search finds an event by title, place and guest.
- [ ] A read-only script token reads the agenda and is refused (403) on a write.
- [ ] A cron automation posts the 07:00 summary to ntfy or a webhook (use `POST /v1/automations/<id>/test` first).

## Operations
- [ ] `hub backup` works; `make backup-drill` passes; the nightly backup file exists next morning; `/v1/status` has no backup warning.
- [ ] Restore the newest backup onto a scratch copy of the volume and start a second hub on it; the phone can be pointed at it and shows the same data.
- [ ] Restart the container mid-sync: no duplicate events, no lost account.
