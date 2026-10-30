# CI and image publishing

The `ci` workflow runs vet, tests (with the race detector), `govulncheck` and the end-to-end suite against fixtures on every push and pull request; the iOS build and tests run on `main` and when `ios/`, `api/` or the Makefile change. `golangci-lint` is advisory (it reports unchecked `Close` and `Remove` errors, mostly in `server/internal/backup`).

If you only want to build and publish from your own machine, with no Actions at all, see "Publishing by hand" below.

Workflows live in `.github/`. Action versions are pinned by major tag, and Dependabot keeps them current.

## What runs

| Workflow | When | What |
|---|---|---|
| `ci.yml` `server` | every push / PR | `go vet`, `go test -race -count=1`, `govulncheck` (Go modules cached) |
| `ci.yml` `lint` | every push / PR | golangci-lint with default config, advisory (`continue-on-error`) until the first run is clean |
| `ci.yml` `e2e` | every push / PR | `make e2e-server`, skipped with a notice while the Makefile has no such target |
| `ci.yml` `ios` | on `main`, or when `ios/**`, `api/**`, `Makefile` changed | brew XcodeGen, `make ios-gen / ios-build / ios-test` on macOS with an iPhone simulator picked via `simctl` (newest runtime, Pro preferred); `.xcresult` uploaded on failure |
| `release.yml` | push to `main`, tags `v*` | on tags first checks tag = `VERSION` = iOS marketing version; reuses `ci.yml` (no iOS), builds the amd64 image and smoke-tests it (`serve --fixtures`, `docker exec calendard health`, `/v1/hello` reports the built version; no secrets), pushes `linux/amd64,linux/arm64` to GHCR with SBOM, max provenance and a signed attestation; on tags also creates the GitHub Release with assets |

Image: `ghcr.io/sunday-studio/calendar-hub`. Tags: `edge` (latest main), `sha-<short>`, and on tag `v1.2.3`: `1.2.3`, `1.2` and `latest` (pre-releases like `v1.2.3-rc.1` get only `1.2.3-rc.1`). Action versions are pinned by major tag; Dependabot (weekly: Go modules, Actions, Docker base image, the CalendarCore Swift package) keeps them current. For stricter supply-chain hygiene, pin each `uses:` to a full commit SHA later (Dependabot can then bump the SHAs).

## One-time setup

No repository secrets are needed: GHCR publishing uses the built-in `GITHUB_TOKEN`. In *Settings, Actions, General*, workflow permissions can stay on "read repository contents"; the jobs request `packages: write` (image) and `contents: write` (GitHub Release only) themselves.

After the first successful `release` run the package is **private** by default and linked to the repo. In *Packages > calendar-hub > Package settings* either leave it private (the home server then needs a token) or set it public (no login required to pull; the image contains no secrets).

## Running the published image

Follow [compose-example.md](compose-example.md), or in the secret-file composes set `CALENDAR_IMAGE` to the published tag.

For a private package, once per machine, with a classic PAT that has only `read:packages`:

```sh
echo "$GHCR_PAT" | docker login ghcr.io -u <github-username> --password-stdin
```

Then:

```sh
export CALENDAR_IMAGE=ghcr.io/sunday-studio/calendar-hub:0.1.0     # or put it in deploy/.env; default is :edge
docker compose -f deploy/compose.host-tailscale.yaml pull
docker compose -f deploy/compose.host-tailscale.yaml up -d
```

The published image is multi-arch (`linux/amd64,linux/arm64`), so it runs natively on Apple Silicon.

## Publishing by hand

Without Actions, build both architectures on your machine and push (log in first with a token that has `write:packages`):

```sh
docker buildx create --name multi --driver docker-container --use
docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=0.1.0 \
  -t ghcr.io/<owner>/calendar-hub:0.1.0 -t ghcr.io/<owner>/calendar-hub:latest --push server
```

Then attach the deploy bundle to the GitHub Release with `gh release create` (the workflow's "Build release assets" step shows what goes in it).

## Releasing and rolling back

Full policy, checklist and steps are in [RELEASING.md](RELEASING.md). Short form:

```sh
make release-dry VERSION=minor     # checks + plan (git repo, main, clean tree, CHANGELOG Unreleased has content)
make release VERSION=minor         # bumps VERSION, docs/process/CHANGELOG.md, ios MARKETING_VERSION; commits; tags; never pushes
git push origin main && git push origin vX.Y.Z
```

The tag runs `release.yml`: guard (tag = `VERSION` = iOS marketing version) -> tests -> amd64 smoke test (`serve --fixtures`, `calendard health`, `/v1/hello` version) -> multi-arch push (`X.Y.Z`, `X.Y`, `latest` for stable only) with SBOM and attestation -> GitHub Release (notes from `docs/process/CHANGELOG.md`, else generated with `.github/release.yml` categories) with the deploy bundle tarball, `openapi.yaml` and `SHA256SUMS`. Only the `github-release` job has `contents: write`.

Rollback: take a backup (`docs/ops/RUNBOOK.md`), go back to the previous version tag (or a `sha-` tag, or `@sha256:` digest from the Actions summary), `docker compose pull && docker compose up -d`. If the newer version already migrated the database, restore the `.pre-migrate-` copy as described in the runbook. Verify provenance with `gh attestation verify oci://ghcr.io/sunday-studio/calendar-hub:0.1.0 --owner <owner>`.

## Not in CI on purpose

iOS code signing, archiving and TestFlight upload need your Apple credentials (distribution certificate, provisioning profiles or an App Store Connect API key), so no job does this. No `ios-release` workflow exists; the supported path is `make ios-archive` on your Mac (RELEASING.md). Optional future job (manual `workflow_dispatch` only, never on tags): macOS runner, importing the certificate and an App Store Connect API key from repository secrets (`ASC_KEY_ID`, `ASC_ISSUER_ID`, `ASC_KEY_P8`, `CERT_P12`, `CERT_PASSWORD`), `xcodebuild archive` + `-exportArchive`, then `xcrun altool`/`fastlane pilot` upload. Add it only after the signing setup (DECISIONS section B) is done.
