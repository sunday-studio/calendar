# Issue log

Problems found while using the app. One entry each, newest first. Say what you saw, what you expected and what you
have not yet checked. Move the entry to **Fixed** with the version and commit when it is done; a fix also goes in
the [changelog](CHANGELOG.md).

Status: `open` (not looked at), `investigating`, `fixed`, `wontfix`.

## Open

### 1. Replying "Yes" to an invitation does not update

- **Status:** open
- **Found:** 2026-10-30, hub 0.1.3, Release app on iPhone
- **Seen:** tapping Yes on an invitation does not update the event as it should. Tapping No and then Yes afterwards
  works.
- **Expected:** Yes updates the event the same way every time.
- **Not yet known:** what "does not update" means on screen (response chip, event colour, or the guest list), whether
  Google shows the reply after a refresh, and whether it only happens for the first reply on a new invitation.
- **Places to look:** the RSVP write in `server/internal/api/write.go` and
  `server/internal/provider/google/write.go`, and how the app shows the response afterwards.

## Fixed

Nothing yet.
