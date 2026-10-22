# Releasing Calendar

One product, one version. The hub image and the iOS app are released together under the same SemVer number. Nothing here has been run on GitHub yet: this folder is not a git repo, and the workflows and `scripts/release.sh` were checked with `actionlint`, `bash -n`/`shellcheck` and a dry run in a scratch repo only.

## Versioning policy

| Thing | Rule |
|---|---|
| Product version | SemVer `X.Y.Z`, single source `VERSION` (today `0.1.0`). `0.x` means the API and data may still change between minors; from `1.0.0` the rules below are strict. |
| Git tag | `vX.Y.Z`, annotated, created by `scripts/release.sh` on `main`. Pre-releases: `vX.Y.Z-rc.1`. The release workflow refuses a tag that disagrees with `VERSION` or the iOS marketing version. |
| Hub image | `ghcr.io/<owner>/calendar-hub`: `X.Y.Z` (exact, never moves), `X.Y` (latest patch), `latest` (latest **stable**; never set for `-rc`), `edge` (tip of `main`), `sha-<short>` (every build). Built `linux/amd64` + `linux/arm64`, with SBOM and signed provenance. The binary reports `X.Y.Z` in `GET /v1/hello` (`-X main.version`, Dockerfile build arg `VERSION`); `edge` builds report `edge`. |
| iOS marketing version | `MARKETING_VERSION` in `ios/project.yml` = product version without any pre-release suffix. `scripts/release.sh` rewrites that one line. |
| iOS build number | `CURRENT_PROJECT_VERSION`, must increase on every upload to App Store Connect, even for the same marketing version. Rule: **UTC timestamp `yymmddHHMM`**, e.g. `BUILD_NUMBER=$(date -u +%y%m%d%H%M)` (2610231530). Always increasing, needs no counter file or CI. The value in `project.yml` (`1`) is only for local builds; do not mix this with run numbers. |
| API version | `apiVersion` from `GET /v1/hello` (`api.APIVersion`, today `1`), documented in `api/openapi.yaml`. Independent of the product version. |

### API compatibility
- Within `apiVersion` 1 changes are **additive only**: new endpoints, new optional request fields, new response fields. Clients must ignore unknown fields. Nothing is removed, renamed, or changed in meaning.
- The hub accepts older apps: it keeps serving every `apiVersion` it ever shipped until a major release says otherwise.
- The app refuses a hub whose `apiVersion` is lower than it needs, with the message "update your hub" (a higher one: "install the latest app"), and saves nothing. **Current code** (`PairingViewModel.supportedAPIVersion`) checks equality with `1`. That is equivalent while only v1 exists; relax it to "hub >= app's minimum" when `apiVersion` 2 is introduced.
- Bumping `apiVersion` is a breaking change and needs a major product version.

### Database
- Migrations are embedded and **forward-only**; there are no down-migrations.
- Before applying pending migrations to an existing database the hub copies it with `VACUUM INTO` to `<db>.pre-migrate-<UTC timestamp>` and keeps the newest 3 (`server/internal/db/db.go`). A fresh database is not backed up (nothing to lose). If that copy fails, the hub does not migrate and does not start.
- So a hub upgrade can be rolled back only by restoring a copy: see Rollback.

### What counts as a breaking release (major, or minor while `0.x`)
- `apiVersion` bump, or any removal/rename/behaviour change of an endpoint or field.
- A migration that cannot be restored from the pre-migrate copy by an older hub (data transformed, not just added), or a change to the backup/encryption format or the master key requirements.
- A new required environment variable, secret file, or Compose service in `deploy/` that existing setups lack.
- Raising the iOS deployment target, or an app that no longer works with the previous hub.
- Everything else: new features are minor, fixes are patch.

## Pre-release checklist

Do these on `main`, in this order. Nothing here is automated by the tag, so do not skip it.

1. CI is green on the commit you will tag (Actions: `ci` for `main`, including the iOS job).
2. `make e2e-server` (hub against the fake Google) passes.
3. `make ios-e2e-real` (UI tests against the real hub + fake Google, needs a booted simulator) passes.
4. `deploy/verify.sh` if it exists in your checkout; otherwise `make deploy-check` and `deploy/first-run.sh`.
5. Walk the smoke checklist with a real Google account: [docs/ops/SMOKE.md](SMOKE.md) (`make smoke`). Until this has been done once, 0.1.0 is not proven against real Google.
6. `docs/process/CHANGELOG.md` `[Unreleased]` describes the changes (user-visible, grouped Added/Changed/Fixed/Removed/Security). Mention migrations and any breaking item.
7. `make release-dry VERSION=patch` (or `minor`, `major`, `X.Y.Z`) prints the plan and every check.

## Cut a release (after the repo exists)

```sh
make release-dry VERSION=minor        # checks + plan, writes nothing
make release     VERSION=minor        # edits VERSION, docs/process/CHANGELOG.md, ios/project.yml; commits "Release vX.Y.Z"; tags; does NOT push
git push origin main
git push origin vX.Y.Z                # this starts the release workflow
```

`scripts/release.sh` refuses unless it is in a git repo, on `main`, with a clean tree, a non-empty `[Unreleased]`, a version greater than the current one, and a tag that does not already exist (locally or on origin). Undo before pushing: `git tag -d vX.Y.Z && git reset --hard HEAD~1`.

What the tag does (`.github/workflows/release.yml`): guard (tag = `VERSION` = iOS marketing version) -> `ci.yml` tests -> amd64 build + smoke test (container runs `serve --fixtures` with no secrets, `calendard health` passes, `/v1/hello` reports the tag's version and `apiVersion` 1, non-root user) -> multi-arch push to GHCR with SBOM and provenance attestation -> GitHub Release. Release notes come from the matching `docs/process/CHANGELOG.md` section; if it is missing, GitHub's generated notes are used with the categories in `.github/release.yml`. Attached: `calendar-deploy-X.Y.Z.tar.gz` (compose files, `first-run.sh`, README, RUNBOOK, SMOKE, `calendard.env.example`, a `compose.ghcr.yaml` override pointing at this version; secrets are never included, and the job fails if it sees something that looks like one), `openapi.yaml` and `SHA256SUMS`. `-rc` tags become GitHub pre-releases and do not move `latest` or `X.Y`.

## Cutting v0.1.0 (the repo does not exist yet)

`VERSION`, `docs/process/CHANGELOG.md` (dated `0.1.0` section) and `ios/project.yml` already say `0.1.0`, so the first release only needs the tag.

1. Review `.gitignore` (created at the repo root). It must ignore: `deploy/secrets/*` (except `.gitkeep`), `deploy/calendard.env`, `*.p8`, `*.p12`, `*.mobileprovision`, `master_key`, `google_client.json`, `*.db*`, `.e2e*/`, `server/bin/`, `ios/build/`, `.build/`, `*.xcarchive`, `*.ipa`. Check before the first commit:
   ```sh
   git init -b main
   git add -A
   git status --short | grep -Ei 'secret|\.env$|\.p8|\.p12|master_key|google_client|\.db|\.ipa|xcarchive' || echo "no secret-looking files staged"
   git check-ignore -v deploy/calendard.env deploy/secrets/master_key ios/build server/bin
   ```
   `ios/Calendar.xcodeproj` is generated by XcodeGen but is currently kept; delete it from the index if you prefer to generate only (`make ios-gen`).
2. `git commit -m "Initial commit"`.
3. Create the GitHub repository and push `main`:
   ```sh
   gh repo create calendar --private --source . --remote origin --push
   ```
   Replace `OWNER` in `docs/process/CHANGELOG.md` link references and in the commented compose hint if you want working compare links. Wait for CI on `main` to go green (this also publishes the `edge` image).
4. Run the pre-release checklist above.
5. `make release-dry VERSION=0.1.0`, then `make release VERSION=0.1.0` (tag-only mode, no new commit), then `git push origin v0.1.0`.
6. Watch the `release` workflow. When it finishes: package `calendar-hub` appears under the repo's Packages (private by default; make it public or log in on the server, see docs/ops/ci.md). Then ship the app (next section).

## iOS release (TestFlight)

**Supported path today: `make ios-archive` on your Mac.** It needs your paid team signed into Xcode and works without any CI secrets.

```sh
BUILD_NUMBER=$(date -u +%y%m%d%H%M) TEAM_ID=<paid team id> make ios-archive
# NO_UPLOAD=1 exports the .ipa without uploading it (then drag it into Transporter)
```

Details, one-time App Store Connect setup and TestFlight steps: [docs/ops/testflight.md](testflight.md). Check out the release tag first (`git checkout vX.Y.Z`) so the app is built from exactly what was tagged, and use the same marketing version as the hub release. A hub-only fix does not need an app upload; a new app build for the same version only needs a higher `BUILD_NUMBER`.

**No `ios-release` workflow is shipped.** Archiving and uploading from GitHub Actions needs a distribution certificate and provisioning (via `fastlane match` or a base64 `.p12` + profile), plus an App Store Connect API key (`ASC_KEY_ID`, `ASC_ISSUER_ID`, `ASC_KEY_P8`), all added by you as repository secrets. That flow cannot be tested here and signing failures on a hosted runner are slow to debug, so it was not written. If you want it later, add a `workflow_dispatch`-only workflow on `macos-latest` that imports the certificate into a temporary keychain, runs `TEAM_ID=... BUILD_NUMBER=... scripts/ios-archive.sh`, and removes the keychain; keep it manual so a tag never uploads to TestFlight by accident.

## Updating the Mac mini

Local build (default, as in docs/ops/first-run.md):

```sh
git fetch --tags && git checkout vX.Y.Z
hub backup                                  # see docs/ops/RUNBOOK.md; the hub also makes a .pre-migrate- copy
CALENDAR_VERSION=X.Y.Z docker compose -f deploy/compose.host-tailscale.yaml up -d --build
```

Published image (no source needed on the server): unpack `calendar-deploy-X.Y.Z.tar.gz` (or keep your existing `deploy/` folder and add the bundle's `deploy/compose.ghcr.yaml`), keep your `calendard.env` and `secrets/`, then:

```sh
export CALENDAR_VERSION=X.Y.Z
docker compose -f deploy/compose.host-tailscale.yaml -f deploy/compose.ghcr.yaml pull
docker compose -f deploy/compose.host-tailscale.yaml -f deploy/compose.ghcr.yaml up -d
```

`compose.ghcr.yaml` is just `services: calendard: {image: ghcr.io/<owner>/calendar-hub:X.Y.Z, pull_policy: always}`. Do not pass `--build` with it. Private package: `docker login ghcr.io` once with a PAT that has only `read:packages` (docs/ops/ci.md). Check afterwards: `docker compose ... exec calendard calendard health`, `calendard doctor`, and the app's Settings should show the new hub version. Use an exact `X.Y.Z` tag on the server, not `latest` or `edge`, so updates are deliberate.

## Rollback

1. `hub backup` of the current state (so nothing is lost), then stop the hub.
2. Switch `CALENDAR_VERSION` back to the previous `X.Y.Z` (local build tag or GHCR tag; a `@sha256:` digest from the Actions summary also works).
3. If the newer version applied migrations, an older hub may not understand the database: restore the newest `<db>.pre-migrate-*` copy or a verified backup (`calendard restore --from FILE`, see docs/ops/RUNBOOK.md), then start. Events made after the upgrade are lost from that copy; they re-sync from Google, local-only data (tokens, devices, rules) is as of the copy.
4. `docker compose up -d` (no `--build`) and run the checks above.
The iOS app: TestFlight keeps previous builds; testers can install an older build. Because the hub accepts older apps, rolling the hub back is safe for a newer app only if `apiVersion` is unchanged.

## Hotfix flow

1. Branch from the tag that is in production only if `main` has unreleased work you cannot ship: `git switch -c hotfix/X.Y.Z+1 vX.Y.Z`, fix, test, and open a PR into a `release/X.Y` branch. Otherwise fix on `main`.
2. Add the `[Unreleased]` entry (Fixed), then `make release VERSION=patch` on `main` (the script requires `main`; for a release branch merge the fix to `main` first or cut the patch from `main` once ready).
3. Push `main` and the tag. Roll out as in "Updating the Mac mini". Bump `BUILD_NUMBER` and upload to TestFlight only if the app changed.

## Yanking a bad release

Never delete or re-point a published tag/version: servers may already run it.
1. Mark the GitHub Release as pre-release or edit its notes to say "do not use, see vX.Y.Z+1" (Releases page). Keep the tag and the image `X.Y.Z`.
2. Move `X.Y` and `latest` back to the last good image so new installs do not get it: retag the old digest, e.g. `docker buildx imagetools create -t ghcr.io/<owner>/calendar-hub:latest ghcr.io/<owner>/calendar-hub:<good version>` (and the same for `X.Y`), after `docker login ghcr.io` with a PAT that has `write:packages`.
3. Ship the fix as a new patch version (hotfix flow). Add a `### Yanked` note under the bad version in `docs/process/CHANGELOG.md` (Keep a Changelog: `## [X.Y.Z] - date [YANKED]`).
4. iOS: in App Store Connect expire the TestFlight build (Build > Expire build) and upload a fixed one with a higher build number.
5. Users already on the bad hub follow Rollback.
