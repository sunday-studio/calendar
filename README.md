<p align="center">
  <img src="docs/assets/icon.png" width="96" alt="Calendar app icon">
</p>

<h1 align="center">Calendar</h1>

<p align="center">
  <b>A calendar of your own: a small hub on your server and a native iPhone app.</b><br>
  Your accounts stay on a machine you own. The phone talks only to it.
</p>

<p align="center">
  <img src="docs/assets/hero.png" alt="Home, Day and event detail screens of the iPhone app" width="760">
</p>

Run the hub on a machine you own. It signs in to your Google Calendar, keeps one synced copy, and writes your
changes back. The iPhone app reads and edits that copy, so tokens never leave your server and there is no hosted
version and no account to create.

## What you get

- **Google Calendar, read and write:** create, edit, delete with Undo, move between calendars, RSVP, guests, Meet
  links, all-day and recurring events (including single-occurrence edits). ICS links, read only.
- **Agenda, Day, 3-day and month views,** free-time search, full-text search and quick add from plain text.
- **Works offline** from the saved copy, with clear banners when the hub or the phone is offline.
- **Push, widgets and a Live Activity:** a silent push keeps the app current, and alerts for moved, cancelled and
  new-invitation events are worded on the phone, so event titles never travel through Apple.
- **An API for your own scripts:** scoped tokens, an MCP endpoint, encrypted backups and optional OpenTelemetry
  export.
- **One small footprint:** a single Go service and SQLite. Back it up as ordinary files.

<table>
  <tr>
    <td align="center"><img src="docs/assets/screenshots/home-light.png" width="200"><br><sub>Home</sub></td>
    <td align="center"><img src="docs/assets/screenshots/day-dark.png" width="200"><br><sub>Day</sub></td>
    <td align="center"><img src="docs/assets/screenshots/event-detail-light.png" width="200"><br><sub>Event</sub></td>
    <td align="center"><img src="docs/assets/screenshots/quick-add-light.png" width="200"><br><sub>Quick add</sub></td>
  </tr>
</table>

## Get started

You need a server with Docker, a Google Cloud OAuth client, and an Apple developer account if you want the app on
your own iPhone. Each release publishes a multi-arch image and a deploy bundle.

```sh
docker login ghcr.io                      # the image is private until you make the package public
docker compose -f deploy/compose.image.yaml up -d
```

The compose file and every setting, in one env file: [docs/ops/compose-example.md](docs/ops/compose-example.md).
The longer walkthrough (Google Cloud project, HTTPS over Tailscale, pairing the phone, backups):
[docs/ops/first-run.md](docs/ops/first-run.md).

The hub is generic. The app is built with the author's bundle id, App Group and push topic, so to install it on
your own phone you change those to your team's: [CONTRIBUTING.md](CONTRIBUTING.md#making-it-yours).

Want to try it first? A demo hub with fake data needs no Google account:

```sh
scripts/run-local.sh demo
```

## Documentation

Everything is indexed in [docs/README.md](docs/README.md). The useful starting points:
[deploying](docs/ops/deploy.md), [day-two tasks and restore](docs/ops/RUNBOOK.md),
[telemetry](docs/engineering/observability.md), [the API](api/openapi.yaml) and
[the changelog](docs/process/CHANGELOG.md).

## Limitations

- Not yet walked on a real account: offline save, the "changed elsewhere" conflict, drag and resize, revoking access
  and moving events between calendars ([docs/ops/SMOKE.md](docs/ops/SMOKE.md)).
- Google answers `fullSyncRequired` to the stored sync token for some calendars, so those resync fully on every poll.
- With the app closed, widgets update only when iOS lets a silent push or a background refresh through.
- Only Google and ICS providers exist.

## Contributing

Issues and pull requests are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md). Report security problems privately,
as described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
