# Security policy

The hub holds OAuth refresh tokens for your calendar accounts, so please report security problems privately.

Use GitHub's private vulnerability reporting: **Security > Report a vulnerability** on this repository. Do not open a
public issue for a vulnerability. Include what you found, how to reproduce it, and the version (`calendard version`,
or `GET /v1/hello`).

You can expect an acknowledgement within a week. Fixes land in a patch release and are listed in the
[changelog](docs/process/CHANGELOG.md).

Supported: the latest release. Before 1.0.0 only the newest minor receives fixes.

How the hub protects secrets (master key, token storage, backups, rate limits) is described in
[docs/ops/deploy.md](docs/ops/deploy.md) and [docs/ops/RUNBOOK.md](docs/ops/RUNBOOK.md). Never put your master key,
Google client secret or APNs key in an issue, a pull request or a log you share.
