# Documentation index

## product/
- [PLAN.md](product/PLAN.md): stories and scope.
- [RESEARCH.md](product/RESEARCH.md): research behind the product (rev 3).
- [MILESTONES.md](product/MILESTONES.md): milestones M0 to M9.

## process/
- [COORDINATION.md](process/COORDINATION.md): how multiple agents work together.
- [STATUS.md](process/STATUS.md): task board per lane.
- [DECISIONS.md](process/DECISIONS.md): decisions, things only you can do, open questions, log.
- [CHANGELOG.md](process/CHANGELOG.md): user-visible changes per version (read and written by `scripts/release.sh` and the release workflow).

## engineering/
- [ENGINEERING.md](engineering/ENGINEERING.md): conventions and architecture for server and iOS.
- [e2e.md](engineering/e2e.md): end-to-end tests against the fake Google.
- [alfather.md](engineering/alfather.md): the alfather adapter.
- [scripts.md](engineering/scripts.md): script tokens, automations, MCP.

## ops/
- [deploy.md](ops/deploy.md): what is in `deploy/` and the hub's env vars.
- [first-run.md](ops/first-run.md): ordered checklist for the first real deployment.
- [RUNBOOK.md](ops/RUNBOOK.md): reconnect, backup, restore, rotation, upgrade, troubleshooting.
- [SMOKE.md](ops/SMOKE.md): manual live checklist (`make smoke`).
- [RELEASING.md](ops/RELEASING.md): versioning and how a release is cut.
- [testflight.md](ops/testflight.md): signing, archive and upload.
- [ci.md](ops/ci.md): GitHub Actions.

## design/
- [design/README.md](design/README.md): canvas link and what lives here.
- [audit-final.md](design/audit-final.md): last design conformance audit.
- [specs/](specs/README.md): per-screen implementation specs.
- `screens/`: reference and regression screenshots (`screens/e2e/` for end-to-end runs).
- ops/local-run.md: run the real hub locally, log your issues before going live
