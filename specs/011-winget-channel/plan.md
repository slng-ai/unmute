# Implementation Plan: Windows Release Channels (winget + Scoop)

**Branch**: `011-winget-channel` | **Date**: 2026-08-15 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/011-winget-channel/spec.md`

## Summary

Open two Windows install channels in the existing GoReleaser pipeline, both
on the free tier. Scoop: add a `scoops:` block that writes a manifest to a
new org-owned bucket repo, live the moment a release run finishes. winget:
flip the dormant block that feature 010 already staged (fork owner, own
classic token, `skip_upload: "auto"`). Update the install docs in the same
change: Scoop is the guided Windows path now, winget is "coming soon" until
Microsoft merges the first manifest PR. No Go code changes at all.

## Technical Context

**Language/Version**: none touched; the repo stays Go 1.24, this feature is
release configuration and docs only

**Primary Dependencies**: GoReleaser v2, open source tier only (FR-005);
GitHub Actions (existing `release.yml`); no new repo dependency

**Storage**: two external Git repos act as channel stores:
`slng-ai/scoop-bucket` (new, org-owned, manifest in root) and the
maintainer's fork of `microsoft/winget-pkgs` (new, synced by GoReleaser)

**Testing**: `goreleaser check` and `make release-dry` (already the
`release-config` CI job on every PR, no secrets needed, FR-007); dry-run
artifact inspection for the two `skip_upload` values; end-to-end proof is
the next tag, verified per [quickstart.md](quickstart.md)

**Target Platform**: GitHub Actions ubuntu runner (publish side); Windows
10/11 (install side)

**Project Type**: CLI release pipeline configuration

**Performance Goals**: none beyond SC-001: a Scoop install must be possible
the moment the release run finishes, zero third-party wait

**Constraints**: free GoReleaser tier only; prereleases skip both channels
(`skip_upload: "auto"`); a channel failure must not damage the release or
any other channel; rehearsal keeps needing zero secrets

**Scale/Scope**: 2 files changed in this repo's pipeline
(`.goreleaser.yaml`, `.github/workflows/release.yml`), 3 docs files
(`docs-site/start/installation.mdx`, `README.md`, `docs/REPO_MAP.md`),
3 contract-doc corrections in `specs/010-*`, 2 external repos, 2 credentials
(one extended, one new)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Compile ahead of time**: PASS. No runtime code; nothing generated
  changes.
- **II. Fail loud, never average**: PASS with one recorded, deliberate
  exception carried over from feature 010: the winget publisher logs errors
  and never turns the run red, so an upstream failure cannot dirty a
  release. The compensating control is FR-driven: the run log is read after
  every tag (quickstart step 5). Scoop in direct-push mode fails loudly like
  the cask does.
- **III. One source of truth**: PASS. Channel behavior lives only in
  `.goreleaser.yaml`; docs describe it, contracts in `specs/` pin it. No
  second capability table appears.
- **IV. The document wins**: PASS. The 010 contract docs currently state a
  falsehood (one token covers tap and winget); FR-010 corrects them in the
  same change. All external claims in research.md carry verification dates
  (2026-08-15).
- **V. Whatever compiles can be spoken to**: N/A, command surface untouched.
- **Complexity**: no new dependency, no paid tier, no abstraction. The one
  new external repo (`scoop-bucket`) mirrors an existing pattern
  (`homebrew-tap`).

Post-design re-check: PASS, unchanged.

## Project Structure

### Documentation (this feature)

```text
specs/011-winget-channel/
├── plan.md              # This file
├── research.md          # Phase 0: decisions D1..D8 with dates
├── data-model.md        # Phase 1: channels, credentials, doc states
├── quickstart.md        # Phase 1: pre-flight and post-tag verification
├── contracts/
│   └── channels.md      # Phase 1: config, workflow, and docs contract
└── tasks.md             # Phase 2 (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
.goreleaser.yaml                      # + scoops: block; winget: flip 3 lines
.github/workflows/release.yml         # + GH_PAT_WINGET in the goreleaser env
docs-site/start/installation.mdx      # + Scoop row/steps; winget -> coming soon
README.md                             # Install section: same two edits
docs/REPO_MAP.md                      # pipeline output list gains Scoop (one line)
specs/010-goreleaser-release-pipeline/
├── contracts/release-workflow.md     # correct Env row and runbook item 4
├── data-model.md                     # correct GH_PAT row, Phase 3 row; + GH_PAT_WINGET row
└── quickstart.md                     # correct Phase 3 preconditions
```

Outside this repo (preconditions, not commits):

```text
slng-ai/scoop-bucket                  # new public repo, README only; GoReleaser writes unmute.json
slng-ai/winget-pkgs                   # org fork of microsoft/winget-pkgs, default branch only
Actions secrets on slng-ai/unmute     # GH_PAT (repo list extended), GH_PAT_WINGET (new, classic)
```

**Structure Decision**: single existing repo; the feature is config and
docs. External state is created by hand once, then only ever written by
release runs.

## Complexity Tracking

No constitution violations to justify. The winget never-red behavior is an
upstream design carried from feature 010, recorded there and re-recorded in
research.md D7 with its compensating control.
