# Implementation Plan: GoReleaser Release Pipeline

**Branch**: `010-goreleaser-release-pipeline` | **Date**: 2026-08-14 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/010-goreleaser-release-pipeline/spec.md`

## Summary

Pushing a `vX.Y.Z` tag becomes the one and only release action. One GitHub
Actions run builds 6 static binaries (darwin/linux/windows on amd64/arm64),
packages them (tar.gz, zip on Windows), writes checksums, signs the checksums
file keyless with cosign (one `.sigstore.json` bundle), attaches one syft
SBOM per archive, publishes a GitHub Release with a conventional-commits
changelog, and updates the Homebrew cask in `slng-ai/homebrew-tap`. Once
public and stable, the same run also opens a winget manifest PR to
`microsoft/winget-pkgs` from our fork. Everything is GoReleaser v2 free-tier
only, validated on every PR by `goreleaser check` plus a full snapshot build.
The feature's first task is the module path change to
`github.com/slng-ai/unmute` (the repo rename is already done). Field-level
config choices were re-verified against live docs on 2026-08-14; see
[research.md](research.md).

## Technical Context

**Language/Version**: Go 1.24 (pinned in `go.mod`); YAML for GoReleaser and
workflow config.

**Primary Dependencies**: GoReleaser v2 free distribution (installed in CI by
`goreleaser/goreleaser-action@v7`, `version: "~> v2"`; the quarantine hook
needs v2.13+, which the range satisfies), `sigstore/cosign-installer@v3`,
`anchore/sbom-action/download-syft@v0`. Zero new Go module dependencies.

**Storage**: N/A. Build output goes to `dist/` (gitignored, FR-022).

**Testing**: existing gate `make fmt lint build test` untouched. New:
`goreleaser check` (exit 1 invalid, exit 2 deprecated) and
`goreleaser release --snapshot --clean --skip=sign` in PR CI with a shell
assertion on the 6 archives and the built binary's `--version` output.
Locally the same via `make release-dry`.

**Target Platform**: binaries for macOS, Linux, Windows on amd64 and arm64;
the pipeline itself runs on ubuntu-latest.

**Project Type**: CLI release tooling (config + workflow + small Go/Makefile
touches + docs).

**Performance Goals**: none beyond SC-004 (a brew install works in under two
minutes on a normal connection); pipeline wall time is not a stated goal.

**Constraints**: GoReleaser free tier only (the Pro fence in research R13);
zero manual steps after the tag push; reproducible builds (trimpath, commit
timestamp, commit date); no secret values anywhere in config; plain-string
`before` hooks only (none are needed).

**Scale/Scope**: 1 config file, 1 new workflow + 1 new CI job, module rename
across `go.mod` + 68 Go files, ~6 docs files, LICENSE, Makefile, main.go.

## Constitution Check

*GATE: evaluated against constitution v2.0.0 before Phase 0; re-checked after
Phase 1 design. No violations requiring Complexity Tracking.*

- **I. Compile ahead of time**: PASS. No runtime layer, no Python, no change
  to the four-stage compile flow. The pipeline builds the same static
  `CGO_ENABLED=0` binary the Makefile builds.
- **II. Fail loud, never average**: PASS. Missing GH_PAT fails the cask
  publish step loudly; `goreleaser check` exit 2 means even deprecated (but
  valid) config turns CI red. One documented exception, accepted in the
  spec's edge cases: GoReleaser logs a failed winget PR without failing the
  release, which is the behavior the spec wants (a Microsoft rejection must
  not block or dirty the GitHub Release).
- **III. One source of truth**: WATCHED, mitigated. The link-time stamping
  recipe now exists in the Makefile and `.goreleaser.yaml`. Both use the
  identical `-X main.version/main.commit/main.date` names (GoReleaser's own
  defaults); the agreement is stated in
  [contracts/version-output.md](contracts/version-output.md) and exercised
  from both sides by `make build` (existing) and the PR snapshot assertion
  (new). Install facts land in README, the docs-site install page, and the
  runbook in the same commit per the repo's three-places rule (FR-018,
  FR-019).
- **IV. The document wins**: PASS. No authoring-surface change, so no
  SCHEMA.md amendment. All provider/tool claims re-verified against official
  docs with dates (research.md). One spec correction made and dated during
  planning: casks have no license field (FR-017).
- **V. Command surface**: PASS. No new command. `--version` already exists
  via cobra's `root.Version`; only the string passed in gets richer (D9).
- **Dependencies rule**: PASS. No new Go dependency; GoReleaser, cosign, and
  syft are CI-side tools, not module deps.
- **Command rules / layout / secrets**: PASS. No cobra changes beyond the
  string in `main.go`; no secret values in any file, only the env names
  `GITHUB_TOKEN` and `GH_PAT`.

**Post-design re-check (after Phase 1)**: unchanged, all PASS; the III
duplication stays mitigated as above.

## Project Structure

### Documentation (this feature)

```text
specs/010-goreleaser-release-pipeline/
├── plan.md              # This file
├── research.md          # Phase 0 output (dated doc verification)
├── data-model.md        # Phase 1 output (artifacts, channels, phases)
├── quickstart.md        # Phase 1 output (validation guide)
├── contracts/
│   ├── goreleaser-config.md   # .goreleaser.yaml contract, annotated
│   ├── release-workflow.md    # release.yml + CI job contract
│   └── version-output.md      # --version output + stamping agreement
└── tasks.md             # Phase 2 output (/speckit-tasks, not this command)
```

### Source Code (repository root)

```text
go.mod                            # module github.com/slng-ai/unmute (task 1)
**/*.go                           # 68 files: import prefix rewrite (task 1)
main.go                           # + commit/date vars, composed version string
Makefile                          # LDFLAGS stamps all three vars; + release-dry
.goreleaser.yaml                  # NEW: the whole pipeline config
.github/workflows/release.yml     # NEW: tag-triggered release
.github/workflows/ci.yml          # + release-config validation job
.gitignore                        # + dist/
LICENSE                           # NEW: MIT
RELEASING.md                      # NEW: the runbook (FR-019)
README.md                         # install section (FR-018)
docs-site/start/installation.mdx  # install paths; module-path note removed
docs-site/README.md               # old clone URL updated
docs/REPO_MAP.md                  # old repo name in its opening line
```

**Structure Decision**: everything lands at the paths above; no new
directories except `dist/` at run time (gitignored, disposable). Historical
spec artifacts under `specs/008-*/` mention the old URL and stay untouched:
they are records of their time.

## Phase summary

- **Phase 0 (done)**: five parallel doc-verification passes; all findings
  and corrections consolidated in [research.md](research.md). All spec-level
  NEEDS CLARIFICATION were already resolved during `/speckit-clarify`.
- **Phase 1 (done)**: [data-model.md](data-model.md) (artifact set,
  channels, phase gates, naming), three contracts under
  [contracts/](contracts/), and [quickstart.md](quickstart.md) (how to
  validate locally, in PR CI, and per phase).
- **Phase 2 (/speckit-tasks)**: expected task shape, in order: (1) module
  path rename + gate, (2) LICENSE + .gitignore, (3) main.go + Makefile
  version stamping, (4) .goreleaser.yaml, (5) release.yml + ci.yml job,
  (6) docs (README, installation.mdx, URL sweeps, RELEASING.md), (7) local
  dry-run verification. External human tasks (tag ruleset, GH_PAT, winget
  fork, going public) stay outside tasks.md per the spec.

## Complexity Tracking

No constitution violations to justify. The one watched item (stamping recipe
in two places) is recorded under Constitution Check III with its mitigation.
