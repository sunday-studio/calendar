# Design

The source of truth is the Gnomon canvas: https://claude.ai/artifact/9NRPFcFQbsNXewTJMLRkfZ
The first design iteration (hi-fi HTML files, `design/src`) was removed; it is recoverable from git commit 9c13ffa.
In this folder and beside it: [specs/](../../specs/README.md) (implementation specs per screen), [screens/](../../screens/)
(reference and regression screenshots, partly from earlier builds), [audit-final.md](audit-final.md) (last conformance audit), and
`audit-shots/` (the screenshots that audit cites).
Widgets and notifications: the current design is the "Widgets and notifications v2" row on the canvas (boards `w2-*`); the older `w-*` boards are superseded. Spec: [widgets-notifications.md](../../specs/widgets-notifications.md).

Photo assets for the GitHub README (fresh screenshots of the current app, the app icon, hero images and widget/notification mocks) live in [docs/assets](../../assets/); `scripts/screenshots.sh` regenerates the screenshots.
