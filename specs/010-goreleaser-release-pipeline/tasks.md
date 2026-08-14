# Tasks: GoReleaser Release Pipeline

**Input**: Design documents from `/specs/010-goreleaser-release-pipeline/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: no test-framework tasks. This feature's checks are executable
gates, not unit tests: `goreleaser check`, snapshot dry runs with artifact
assertions, and the existing `make fmt lint build test` gate. Validation
tasks below run them at each checkpoint.

**Organization**: grouped by user story from spec.md. US1 is the MVP. All
config blocks live in one file (`.goreleaser.yaml`), so tasks touching it are
sequential across stories by design, never [P] against each other.

## Format: `[ID] [P?] [Story] Description`

## Outside this repo (humans, not tasks)

Per spec "External prerequisites": make repo public (gates Rollout Phase 2),
winget fork (Rollout Phase 3), mint `GH_PAT` (Rollout Phases 2/3), `v*` tag
ruleset. None block the tasks below; the pipeline lands with publishing off.
"Rollout Phase" always means the spec's rollout stages, never the task
phases below.

---

## Phase 1: Setup

**Purpose**: prepare the ground; no behavior changes.

- [X] T001 Add `dist/` to `.gitignore` (FR-022), with a one-line comment that GoReleaser writes there
- [X] T002 [P] Install local tooling and prove it: `brew install goreleaser syft`, then `goreleaser healthcheck` (document result in the PR description; needed for T015/T025, see quickstart.md §Prerequisites)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the module identity and version plumbing every story builds on.

**⚠️ CRITICAL**: T003+T004 are the feature's agreed first task (spec
Clarifications 2026-08-14); nothing else lands before they are green.

- [X] T003 Change module path to `github.com/slng-ai/unmute`: edit `go.mod` line 1 and rewrite the import prefix `github.com/slng/unmute` → `github.com/slng-ai/unmute` in all 68 Go files (mechanical sweep, e.g. `find . -name '*.go' -exec sed -i '' 's|github.com/slng/unmute|github.com/slng-ai/unmute|g' {} +`), one commit, nothing else in it
- [X] T004 Prove the rename: run `make fmt lint build test` and confirm all green; `git grep -l 'github.com/slng/unmute' -- '*.go' go.mod` returns nothing
- [X] T005 [P] Create `LICENSE` at repo root: MIT text, copyright line `Copyright (c) 2026 slng.ai` (maintainer confirms exact legal entity wording in review) (FR-017)
- [X] T006 [P] Add `commit` and `date` link-time vars to `main.go` and compose the version string passed to `cli.Execute` exactly per `contracts/version-output.md` (bare version when commit is empty; no change to `internal/cli`)
- [X] T007 Extend `Makefile` `LDFLAGS` to stamp all three vars (`main.version` unchanged from `git describe`; `main.commit` from `git rev-parse --short HEAD`; `main.date` from `git log -1 --format=%cI`) per `contracts/version-output.md`, then verify `make build && bin/unmute --version` prints `<describe> (<sha> <date>)`

- [X] T024 Make the CLI cross-compile for Windows: `syscall.SysProcAttr{Setpgid: true}` (`internal/cli/dev_tunnel.go`, `internal/cli/dev_cloud_websocket.go`) and `syscall.Kill` (`internal/cli/dev.go`) are POSIX-only, so `GOOS=windows go build .` failed and the 6-pair matrix could never build. Two build-tagged helpers (`internal/cli/procgroup_unix.go`, `internal/cli/procgroup_windows.go`) hold the difference; the Windows graceful stop is a documented no-op and the forceful one kills only the child. Prove it with all 6 pairs of `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -o /dev/null .`

  *Added during implementation, 2026-08-14.* It was not foreseen: nothing had
  ever asked this repo for a Windows binary, so nothing had ever compiled for
  one. Found by T014's first `make release-dry`, which died on
  `target=windows_amd64_v1`. It belongs in Phase 2 because every story's build
  depends on it.

**Checkpoint**: module identity final, both `--version` fields stamped, gate green.

---

## Phase 3: User Story 1 - One tag push produces the whole release (P1) 🎯 MVP

**Goal**: a `v*` tag yields a complete GitHub Release: 6 binaries in 6
archives, checksums, keyless signature bundle, 6 SBOMs, grouped notes, zero
manual steps.

**Independent Test**: quickstart.md §4 — push a test tag on the private
repo, inspect the release, download a binary, check `--version`, delete the
test tag and release.

### Implementation for User Story 1

- [X] T008 [US1] Create `.goreleaser.yaml` with the core blocks exactly per `contracts/goreleaser-config.md`: `version: 2`, `project_name`, commented `gomod` block (Rollout Phase 2 flip), `builds` (env/goos/goarch pinned to 6 pairs/main/binary/flags/mod_timestamp/ldflags), `archives` (`formats: [tar.gz]`, windows zip override), `checksum`, `changelog` (use git, sort asc, exclude docs/style/test/chore, groups Features/Bug fixes/Others), `release` (explicit owner/name, `prerelease: auto`)
- [X] T009 [US1] Add the `sboms` block (`artifacts: archive`) and the `signs` block (cosign keyless bundle, `artifacts: checksum`) to `.goreleaser.yaml`, verbatim shapes from research.md R8/R9
- [X] T010 [US1] Create `.github/workflows/release.yml` exactly per `contracts/release-workflow.md`: trigger `push: tags: [v*]`; permissions `contents: write` + `id-token: write`; checkout `fetch-depth: 0`; setup-go from `go.mod`; `sigstore/cosign-installer@v3`; `anchore/sbom-action/download-syft@v0`; `goreleaser/goreleaser-action@v7` (`distribution: goreleaser`, `version: "~> v2"`, `args: release --clean`); env `GITHUB_TOKEN` + `GH_PAT`
- [X] T011 [US1] Run `goreleaser check` locally; exit code must be 0 (2 = deprecated key = fix before continuing), per quickstart.md §1

**Checkpoint / validation (maintainer)**: quickstart.md §4 — cut `v0.1.0`
(or next free number) on the private repo, verify the release contents, the
grouped notes, the `--version` output, and the cosign bundle
(`cosign verify-blob --bundle ...`); confirm the tap was NOT touched; clean
up the test tag. US1 is done when this passes.

---

## Phase 4: User Story 2 - A broken release setup cannot reach main (P2)

**Goal**: every PR validates the config and rehearses the full pipeline
without publishing; maintainers can do the same locally with one command.

**Independent Test**: quickstart.md §3 — a PR that breaks the config goes
red on the `release-config` job; reverted, the job passes and asserts the
artifacts.

### Implementation for User Story 2

- [X] T012 [US2] Add `release-config` job to `.github/workflows/ci.yml` per `contracts/release-workflow.md`: checkout `fetch-depth: 0`, setup-go from `go.mod`, `anchore/sbom-action/download-syft@v0`, goreleaser-action `install-only: true`, then `goreleaser check`, then `goreleaser release --snapshot --clean --skip=sign`, then a shell assertion step: exactly 4 `*.tar.gz` + 2 `*.zip` in `dist/`, 1 `*_checksums.txt`, 6 `*.sbom.json`, and the linux/amd64 binary under `dist/` prints a `--version` matching `SNAPSHOT.*\([0-9a-f]{7,} .+\)` — the full shape from `contracts/version-output.md`, so a dropped or misnamed `main.commit`/`main.date` stamp goes red instead of silently falling back to the bare version
- [X] T013 [P] [US2] Add `release-dry: ; goreleaser release --snapshot --clean --skip=sign` target and `.PHONY` entry to `Makefile` (FR-014, D12)
- [X] T014 [US2] Run `make release-dry` locally and verify quickstart.md §2 end to end: the 6 archives, checksums, 6 SBOMs, unpublished cask + winget manifests absent for now, and the extracted binary's snapshot `--version`

**Checkpoint / validation**: open the drill PR from quickstart.md §3 (break
config → red, revert → green). US1 and US2 both hold.

---

## Phase 5: User Story 3 - A macOS user installs with one brew command (P3)

**Goal**: the release run generates and (from Rollout Phase 2) commits the
cask to `slng-ai/homebrew-tap`; installed binaries run with no Gatekeeper
error.

**Independent Test**: dry run shows the generated cask with the quarantine
hook; the live `brew install` test is gated on Rollout Phase 2
(quickstart.md §5).

### Implementation for User Story 3

- [X] T015 [US3] Add the `homebrew_casks` block to `.goreleaser.yaml` exactly per `contracts/goreleaser-config.md`: name, `skip_upload: true` with the `Rollout Phase 2: "auto"` comment, repository slng-ai/homebrew-tap with `token: "{{ .Env.GH_PAT }}"`, homepage, description, and the verbatim `xattr -dr com.apple.quarantine` post-install hook targeting `#{staged_path}/unmute`; no license key (casks have none, FR-017)
- [X] T016 [US3] Verify: `goreleaser check` still exits 0 and `make release-dry` now also writes the cask under `dist/` — read the generated `unmute.rb` and confirm both arch stanzas and the hook are present. Run the dry run once with `GH_PAT` deliberately unset: if the `{{ .Env.GH_PAT }}` token template makes it fail, give the make target an empty default (e.g. `GH_PAT ?=` in the Makefile) and record the behavior in RELEASING.md

**Checkpoint**: cask generation proven; publishing waits on the Rollout Phase 2 flip.

---

## Phase 6: User Story 4 - Anyone can verify what they downloaded (P4)

**Goal**: the verification story is documented where releasers and users
look; the artifacts themselves already exist from US1.

**Independent Test**: following only the written steps against the US1 test
release: checksum matches, signature verifies, SBOM opens; Phase 2 adds
`go version -m` provenance.

### Implementation for User Story 4

- [X] T017 [US4] Create `RELEASING.md` at repo root per `contracts/release-workflow.md` §runbook (FR-019): who can release (tag ruleset), the one command and what it triggers, the phase table with the exact one-line flips (cask skip_upload, gomod uncomment, winget skip_upload + FORK_OWNER), secrets (`GH_PAT` scope and owner), verification commands (shasum, `cosign verify-blob --bundle`, `go version -m`), and the known failure modes (re-run safety, missing PAT, winget rejection non-fatal, CLA one-time, antivirus false positives, proxy lag). Naming: call the rollout stages "Rollout Phase 1/2/3" everywhere (runbook prose and config comments) so they never collide with task or plan phase numbers

**Checkpoint**: a reader can verify a release without asking anyone.

---

## Phase 7: User Story 5 - A Windows user installs with winget (P5)

**Goal**: the winget channel is fully configured and dormant; Rollout
Phase 3 is a one-line flip plus filling the fork owner.

**Independent Test**: dry run generates manifests under `dist/`; the live PR
flow is gated on Rollout Phase 3 (quickstart.md §6).

### Implementation for User Story 5

- [X] T018 [US5] Add the `winget` block to `.goreleaser.yaml` exactly per `contracts/goreleaser-config.md`: publisher `slng`, identifier `slng.unmute`, required `short_description` + `license: MIT`, publisher URLs, `skip_upload: true` with the `Rollout Phase 3` comment, repository `FORK_OWNER/winget-pkgs` + `pull_request.enabled: true` + base microsoft/winget-pkgs master
- [X] T019 [US5] Verify: `goreleaser check` exits 0 and `make release-dry` writes winget manifests under `dist/` at the `manifests/s/slng/unmute/<version>` shape (installer, locale, version files)

**Checkpoint**: all five stories implemented; channels dormant by flag only.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: the docs rule (three places, one commit) and final proof.

- [X] T020 [P] Rewrite the README.md install/build section (FR-018): all four install paths (brew macOS-only, winget Windows, `go install`, direct download) with per-phase honesty (what works today vs at repo-public), new `--version` output shape, keep `make build` for contributors
- [X] T021 [P] Update `docs-site/start/installation.mdx` (FR-018): new clone URL `slng-ai/unmute`, the four install paths with honest platform coverage (FR-011: no brew on Linux), new `--version` sample output, and replace the "module path differs" Note with the phase-true statement (`go install` works once the repo is public; the exact wording goes live now, the note deletion is listed as part of the Rollout Phase 2 flip in RELEASING.md)
- [X] T022 [P] Sweep remaining live docs for the old URLs `slng/unmute` and `slng-ai/unmute_cli`: `docs-site/README.md` (two Mintlify repository-access lines) and `docs/REPO_MAP.md` (opening line names the old repo); leave `specs/008-*` historical artifacts untouched. `git grep -l 'unmute_cli' -- ':!specs/'` must return nothing when this and T020 are done
- [X] T023 Run the full local validation: quickstart.md §1 (`goreleaser check`), §2 (`make release-dry` + artifact list + snapshot `--version`), and `make fmt lint build test`; confirm `grep -E 'metadata:|includes:|template_files|after:' .goreleaser.yaml` finds no Pro key (research.md R13, SC-008)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: none; start immediately. T001 and T002 are independent.
- **Foundational (Phase 2)**: T003 → T004 strictly ordered; T005/T006 parallel to them; T007 after T006. Blocks all stories.
- **US1 (Phase 3)**: after Foundational. T008 → T009 (same file) → T011; T010 parallel to T009 after T008 exists.
- **US2 (Phase 4)**: after US1's T008/T009 (the snapshot asserts SBOMs, which T009 adds). T013 parallel to T012.
- **US3 (Phase 5) / US5 (Phase 7)**: each edits `.goreleaser.yaml`, so sequential with any other task touching it; otherwise only need US1's T008.
- **US4 (Phase 6)**: needs US1's test release to have happened for its independent test; the writing task itself only needs the contracts.
- **Polish (Phase 8)**: T020/T021/T022 parallel; T023 last, after everything.

### Story order note

US2's artifact assertion counts SBOMs, so it lands after T009 rather than
being fully independent of US1. This is deliberate: the assertion is US2's
whole value, and a weaker "archives only" assertion would go stale the same
week it was written.

### Parallel Opportunities

- Phase 1: T001 ∥ T002
- Phase 2: (T003→T004) ∥ T005 ∥ (T006→T007)
- Phase 3: T010 ∥ T009 (different files, both after T008)
- Phase 4: T012 ∥ T013
- Phase 8: T020 ∥ T021 ∥ T022

## Parallel Example: Phase 2

```bash
# After T003+T004 (module rename) is committed:
Task: "Create LICENSE (MIT) at repo root"                       # T005
Task: "Add commit/date vars to main.go per version-output.md"   # T006
```

## Implementation Strategy

**MVP = Phase 1 + 2 + 3 (US1)**: after T011 and the test-tag checkpoint,
the product exists: one tag, one complete private release. Stop and validate
there.

**Incremental**: each later story is one small addition that
`goreleaser check` + `make release-dry` re-validate in minutes. US2 makes
the pipeline safe to evolve; US3/US5 are dormant channel blocks; US4 is the
runbook. Ship the whole feature as one PR or story-by-story; each checkpoint
leaves the repo releasable.

**One-PR note**: FR-018/FR-019 require the docs (T017, T020–T022) to land in
the same change as the pipeline, so if shipping story-by-story, the docs
phase merges together with the last config-bearing PR.

## Notes

- Tasks touching `.goreleaser.yaml` (T008, T009, T015, T018) are never [P]
  with each other: one file, deliberate order, `goreleaser check` after each.
- Commit after each task or logical group; T003 stays a lone mechanical
  commit per the clarification.
- The three checkpoints with human actions (test tag, drill PR, entity name
  on the LICENSE) are the only places a task waits on the maintainer.

## Implementation log, 2026-08-14

Two things changed while these tasks ran. Both are recorded where they are
load-bearing, not only here.

- **T024 added** (above): the CLI did not compile for Windows at all. Nothing
  in Phase 0 or 1 caught it because the question had never been asked.
- **`{{.Commit}}` → `{{.ShortCommit}}`** in `.goreleaser.yaml`.
  `contracts/version-output.md` said `{{.Commit}}` in its stamping table, which
  is the full 40-character SHA, while the output table on the same page showed a
  short one. The two build paths would have printed different shapes for the
  same commit. Both contracts are corrected and carry the dated note.
- T016's fallback turned out to be unnecessary: a snapshot run succeeds with
  `GH_PAT` completely unset, because snapshot mode never publishes and so never
  evaluates the token template. No `GH_PAT ?=` default in the Makefile.
- **T017's `RELEASING.md` was deleted by the maintainer before merge.** The task
  ran and the file existed; the maintainer removed it, so FR-019 ships unmet
  (noted in spec.md). Every reference to it was cleaned up in the same change:
  `README.md`, `docs-site/start/installation.mdx`, `docs/REPO_MAP.md`,
  `.goreleaser.yaml`, `.github/workflows/release.yml`. The verified facts it
  carried, the rollout flips, the `GH_PAT` scope, the release-notes procedure
  and the failure modes, survive only in `contracts/` and this folder.
- Release notes needed one config addition that was not in any task:
  `release.header: {{ .TagContents }}`, plus a `^.*?Merge ` changelog filter.
  See assertion 6 in `contracts/goreleaser-config.md` for the four gotchas
  behind it.
