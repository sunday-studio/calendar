# Contributing

Thanks for looking. This is a personal project that others can run and change; issues and pull requests are welcome,
and small, focused ones are easiest to review.

## Set up

- Hub: Go (see `server/go.mod`). `make test` runs vet and the race tests; `make e2e-server` runs the hub against the
  fake Google (`server/cmd/fakegoogle`), so you never need a real account to develop.
- App: Xcode 26 and [XcodeGen](https://github.com/yonaskolb/XcodeGen). `make ios-gen` generates the project from
  `ios/project.yml`; `make ios-test` runs the unit tests on a simulator. Set `CALENDAR_TEAM_ID` to your Apple team to
  run on a device.
- A hub with demo data and no Google: `scripts/run-local.sh demo` ([docs/ops/local-run.md](docs/ops/local-run.md)).

## Making it yours

The bundle id (`dev.casprine.calendar`), the App Group and the APNs topic belong to the original author's Apple
account. To run your own build, change them together: `ios/project.yml` (bundle ids, App Group, entitlements),
`CALENDAR_APNS_TOPIC` on the hub, and the App Group string in the Swift sources. Your own Google Cloud project and
APNs key are needed for real accounts and push; [docs/ops/first-run.md](docs/ops/first-run.md) walks through them.

## Pull requests

- Run `make test` and, for app changes, `make ios-test` before opening one. Add a test with each fix.
- Keep `api/openapi.yaml` in step with any API change; changes within `apiVersion` 1 are additive only.
- Commit messages say what changed and why, in the imperative. Run `git config core.hooksPath scripts/hooks` once per
  clone; the hook strips AI-attribution trailers (see [AGENTS.md](AGENTS.md)).
- Do not commit secrets, `.p8` keys, `.env` files or databases. `.gitignore` covers the usual ones.

## Reporting security problems

See [SECURITY.md](SECURITY.md); do not file them as public issues.
