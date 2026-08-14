# Contract: release workflow and PR validation

**Date**: 2026-08-14 | Verified against goreleaser-action v7 README and the
GoReleaser CI docs ([research.md](../research.md), R12).

## `.github/workflows/release.yml` (new)

| Aspect | Contract |
|---|---|
| Trigger | `on: push: tags: ["v*"]` and nothing else |
| Permissions | `contents: write` (create the release), `id-token: write` (cosign keyless OIDC). Nothing more. |
| Checkout | `actions/checkout` with `fetch-depth: 0` (required for the changelog and git-describe history) |
| Go | `actions/setup-go` with `go-version-file: go.mod` (single source of truth, same as ci.yml) |
| Tool installs | `sigstore/cosign-installer@v3` and `anchore/sbom-action/download-syft@v0`; the GoReleaser action installs neither by design |
| Release step | `goreleaser/goreleaser-action@v7`, `distribution: goreleaser`, `version: "~> v2"`, `args: release --clean` |
| Env | `GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}` (release), `GH_PAT: ${{ secrets.GH_PAT }}` (tap and winget fork; empty until the secret exists, harmless while `skip_upload: true`) |

Failure semantics: a failed run for a tag is safe to re-run; `--clean` wipes
`dist/` first and release mode `keep-existing` (default) does not duplicate.
A missing or expired `GH_PAT` fails the cask publish step loudly once
Phase 2 flips (spec edge case). A failed winget PR logs but does not fail
the run (documented GoReleaser behavior, accepted by the spec).

## `ci.yml` addition: `release-config` job (runs with test/lint/format on every PR)

Steps, in order:

1. Checkout with `fetch-depth: 0`, setup-go from `go.mod` (as above).
2. `anchore/sbom-action/download-syft@v0` (snapshot produces SBOMs too).
3. `goreleaser/goreleaser-action@v7` with `install-only: true`.
4. `goreleaser check` — exit 1 (invalid) and exit 2 (deprecated) both fail
   the job.
5. `goreleaser release --snapshot --clean --skip=sign` — full pipeline
   rehearsal, publishes nothing. Sign is skipped because PRs should not
   mint OIDC tokens and the release path is exercised on real tags.
6. Assertion step (shell): exactly 6 archives exist in `dist/` (4 tar.gz +
   2 zip), the checksums file and 6 `.sbom.json` files exist, and
   `dist/` contains a linux/amd64 binary whose `--version` output contains
   the snapshot version string. This is SC-002 running on every PR.

The job keeps the existing workflow conventions: `permissions: contents:
read` at workflow level stays; this job needs no write permission because
snapshot mode never publishes.

## `Makefile` addition

```make
release-dry: ; goreleaser release --snapshot --clean --skip=sign
```

Same command as CI step 5, so a maintainer reproduces PR validation locally
(FR-014, SC-002). `goreleaser healthcheck` is the documented way to check
local tooling; the runbook mentions it. Existing targets stay untouched
except `LDFLAGS` (see [version-output.md](version-output.md)).

## `RELEASING.md` runbook (FR-019) must cover

1. Who can release: the `v*` tag ruleset (external prerequisite 5).
2. The one command: `git tag vX.Y.Z && git push origin vX.Y.Z`, and exactly
   what the run produces.
3. Phase table and the one-line flips (from
   [goreleaser-config.md](goreleaser-config.md) assertion 3), with each
   phase's external preconditions.
4. Secrets: `GH_PAT` fine-grained, maintainer-owned, contents-write on
   `slng-ai/homebrew-tap` and the winget fork; where it is stored; that the
   default token cannot cross repos.
5. Known failure modes and responses: re-running a tag, missing PAT,
   winget PR rejected or flagged by Microsoft validation (non-fatal,
   manual follow-up), antivirus false positives, module proxy lag after
   going public, first winget PR needs the Microsoft CLA (one-time).
