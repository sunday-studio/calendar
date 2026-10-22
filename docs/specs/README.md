# Implementation specs (design lane)

Source of truth: the **Gnomon canvas** (artifact `9NRPFcFQbsNXewTJMLRkfZ`; board ids below are its file names, e.g. `v-day` = `project/v-day.dc.html`). Style sheet = `project/gnomon.css`. Numbers are pt on a 390×844 frame. Dark is the default theme; every board also has a light twin (`-l`).
`design/src` is the superseded earlier iteration: do not use it.

Components referenced here live in `ios/Packages/CalendarUI` (see `docs/design-audit-1.md` §4 for the D2 list). **New components added for these specs** are marked ★ and listed in each file's "Components" section.

| Spec | Boards | Milestone |
|---|---|---|
| [day-view.md](day-view.md) | v-day, v-day2, v-dayempty, v-three, v-week (drag/resize/fail = D5) | S1 / S4 |
| [event-detail.md](event-detail.md) | d-detail, d-nolink, d-people, d-recur, d-actions, d-move, d-del, d-deleted, d-delrec, d-ro, d-join | S1 / S3 |
| [settings.md](settings.md) | g-home, g-cals, g-colour, g-default, g-tz, g-alerts, g-work, g-look, g-week | S2 |
| [offline-status.md](offline-status.md) | y-offline, y-offedit, y-down, y-diag, y-status, y-log, y-tokens, y-newtoken | S2 |

Every spec ends with **Done means**: the simulator screenshot (light and dark) next to the board, per STATUS rule 1.
