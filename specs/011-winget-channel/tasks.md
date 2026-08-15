# Tasks: Windows Release Channels (winget + Scoop)

**Input**: Design documents from `/specs/011-winget-channel/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/channels.md, quickstart.md

**Tests**: No test tasks. The feature is release config and docs; the checks
are the existing `release-config` CI job (`goreleaser check` +
`make release-dry`), the dry-run artifact inspection, and the post-tag
verification in quickstart.md. No Go code changes.

**Organization**: Grouped by user story. All in-repo edits land in one PR,
but each story's edits are independently rehearsable in a dry run.

**External preconditions**: already done by the maintainer (bucket repo,
`GH_PAT` extended, org fork, `GH_PAT_WINGET` minted and stored). T001
re-verifies them cheaply before any edit.

## Format: `[ID] [P?] [Story] Description`

## Phase 1: Setup

**Purpose**: prove the outside world is ready and stage the branch.

- [X] T001 Re-verify the external preconditions from specs/011-winget-channel/contracts/channels.md section 5: `gh api repos/slng-ai/scoop-bucket --jq '.visibility, .permissions.push'` (public, true), `gh api repos/slng-ai/winget-pkgs --jq '.fork, .permissions.push'` (true, true), `gh secret list --repo slng-ai/unmute` (shows GH_PAT and GH_PAT_WINGET)
- [X] T002 Create the working branch from fresh main: `git checkout main && git pull && git checkout -b release/open-windows-channels`

---

## Phase 2: Foundational

**Purpose**: prove the local toolchain before blaming any edit for a failure.

**⚠️ CRITICAL**: complete before any story work.

- [X] T003 Baseline rehearsal on the unmodified config: `goreleaser check` exits 0 and `make release-dry` succeeds locally with no secrets set

**Checkpoint**: any failure after this point was caused by an edit.

---

## Phase 3: User Story 1 - winget install (Priority: P1) 🎯 MVP

**Goal**: the dormant winget block publishes on stable tags: manifest branch
to the org fork, PR to `microsoft/winget-pkgs`, no manual step.

**Independent Test**: `make release-dry` generates winget manifests in
`dist/` with `skip_upload` resolving to `auto`; end to end, the next tag's
run log shows the pushed branch and the upstream PR URL.

### Implementation for User Story 1

- [X] T004 [US1] Apply the three winget line edits in .goreleaser.yaml per contracts/channels.md section 1: `skip_upload: true` → `"auto"`, `owner: FORK_OWNER` → `slng-ai`, winget `token` → `"{{ .Env.GH_PAT_WINGET }}"`; update the "Dormant until Rollout Phase 3" comment to say the channel is open
- [X] T005 [P] [US1] In .github/workflows/release.yml: add `GH_PAT_WINGET: ${{ secrets.GH_PAT_WINGET }}` next to `GH_PAT` in the goreleaser step's `env`, and correct the two comments that become false: the header (line 4, "updates the Homebrew cask and opens the winget PR") gains the Scoop bucket, and the `GH_PAT` comment (line 53, "Writes the tap and the winget fork") becomes tap + Scoop bucket, with the fork belonging to `GH_PAT_WINGET`
- [X] T006 [P] [US1] Correct specs/010-goreleaser-release-pipeline/contracts/release-workflow.md: the Env row (line 16) lists `GH_PAT` (tap and Scoop bucket) and `GH_PAT_WINGET` (winget fork and upstream PR); runbook item 4 (lines 71-72) describes both tokens
- [X] T007 [P] [US1] Correct specs/010-goreleaser-release-pipeline/data-model.md: `GH_PAT` row (line 66) drops "tap + fork" and "the only added credential" and now covers tap + bucket; add a `GH_PAT_WINGET` row; Phase 3 row (line 55) names `owner: slng-ai` and the new secret
- [X] T008 [P] [US1] Correct specs/010-goreleaser-release-pipeline/quickstart.md: Phase 3 preconditions (line 96 area) add "GH_PAT_WINGET set and pre-flighted" and the org fork

**Checkpoint**: `make release-dry` writes winget manifests under `dist/`;
the artifacts inspection (T014) will show `auto`.

---

## Phase 4: User Story 2 - Scoop install on release day (Priority: P2)

**Goal**: a `scoops:` block writes `unmute.json` to `slng-ai/scoop-bucket`
on stable tags; the docs guide Windows users to it as the path that works
now, with winget marked "coming soon".

**Independent Test**: `make release-dry` generates `unmute.json` in `dist/`
with `skip_upload` resolving to `auto`; end to end, the bucket receives a
commit the moment the next tag's run finishes and a Windows machine can
install.

### Implementation for User Story 2

- [X] T009 [US2] Add the `scoops:` block to .goreleaser.yaml, exactly the YAML in contracts/channels.md section 1, placed after `homebrew_casks` and before `winget`, with a comment in the house style (why direct push, why root, why "auto")
- [X] T010 [P] [US2] Docs state A in docs-site/start/installation.mdx: add a Scoop row to the "Pick a way" table plus the two commands (`scoop bucket add slng-ai https://github.com/slng-ai/scoop-bucket`, `scoop install slng-ai/unmute`), change the winget row to "coming soon", rewrite the Note: Homebrew, Scoop, `go install` and the archive work today; winget manifests are submitted on each release and the command works once Microsoft merges
- [X] T011 [P] [US2] Docs state A in README.md Install section (lines ~94-115): same two changes as T010, keeping the section's existing voice
- [X] T012 [P] [US2] Update docs/REPO_MAP.md line 120: the pipeline output list "Homebrew cask, winget" becomes "Homebrew cask, Scoop manifest, winget"

**Checkpoint**: dry run writes the Scoop manifest; docs never show a command
that cannot work yet (spec edge case).

---

## Phase 5: User Story 3 - the release loop is intact (Priority: P3)

**Goal**: prove the maintainer's one-action release survived the edits:
free tier, no secrets needed to rehearse, untouched blocks byte-identical.

**Independent Test**: full rehearsal passes with zero secrets; the diff
touches nothing outside the contract's file list.

### Implementation for User Story 3

- [X] T013 [US3] Full rehearsal on the branch: `goreleaser check` exits 0 (still the OSS binary, proving FR-005) and `make release-dry` succeeds with no secrets set (proving FR-007)
- [X] T014 [US3] Inspect dist/artifacts.json with the tolerant snippet from quickstart.md step 2: every Scoop and winget artifact resolves `skip_upload` to `auto`; open the generated `unmute.json` and winget manifests under dist/ and read them
- [X] T015 [US3] Confirm the blast radius: `git diff main --stat` lists only .goreleaser.yaml, .github/workflows/release.yml, docs-site/start/installation.mdx, README.md, docs/REPO_MAP.md, and the three specs/010 files; within .goreleaser.yaml, `builds`, `archives`, `checksum`, `sboms`, `signs`, `release`, and `homebrew_casks` are unchanged

**Checkpoint**: US3 acceptance scenarios 3.3 and 3.4 hold before the PR
even opens.

---

## Phase 6: Ship & verify (cross-cutting)

**Purpose**: land the PR, tag, and verify each channel per quickstart.md.
Merge and tag are maintainer actions.

- [X] T016 Commit and open the PR: `git add -A && git commit -m "feat(release): open the Scoop and winget channels"`, push, `gh pr create --fill`, then `gh pr checks --watch` (the release-config job green re-proves FR-007 in CI)
- [ ] T017 Maintainer merges: `gh pr merge --squash --delete-branch`, then `git checkout main && git pull`
- [ ] T018 Prove the flips are on origin/main with the three greps in quickstart.md step 3 (scoops `"auto"`, winget `"auto"`/`slng-ai`/`GH_PAT_WINGET`, workflow env line)
- [ ] T019 Maintainer tags and pushes v0.1.3 per quickstart.md step 4 (annotated, `--cleanup=verbatim`, read back before pushing), then watches the run
- [ ] T020 Read the run log per quickstart.md step 5: Scoop shows a commit to slng-ai/scoop-bucket with no error; winget shows a manifest branch and a PR URL on microsoft/winget-pkgs (winget never turns the run red, so this read is mandatory)
- [ ] T021 Verify Scoop like a stranger per quickstart.md step 6: `gh api repos/slng-ai/scoop-bucket/contents/unmute.json --jq '.name, .size'`, and on a Windows machine the two install commands plus `unmute --version` printing 0.1.3 (SC-001)
- [ ] T022 Maintainer answers the Microsoft CLA bot on the first winget PR (one-time, from the token's account), then tracks the PR to merge; fix validation flags by hand on the PR if any
- [ ] T023 After Microsoft merges and the index refreshes (quickstart.md step 7): docs state B in a separate small PR touching docs-site/start/installation.mdx and README.md only: winget becomes the primary Windows path, Scoop stays documented as the fastest, every "coming soon" deleted (FR-009)
- [ ] T024 Update the gitignored RELEASING.md runbook to match reality: Scoop section added, phase table updated, winget marked open, the stale "merge PR #77 before the next tag" item removed (it merged)
- [ ] T025 Prerelease guard, deferred until the first `-rc` tag after the flip: verify per quickstart.md step 9 that the tag produces a marked GitHub prerelease and that the bucket, the fork, and the tap all received nothing (FR-003, SC-005)

---

## Dependencies & Execution Order

- **Phase 1 → 2 → (3, 4) → 5 → 6**, strictly. Phases 3 and 4 both edit
  .goreleaser.yaml, so T004 and T009 are sequential; every other story task
  marked [P] can run alongside its phase-mates.
- T005-T008 [P] after T004 (different files). T010-T012 [P] after T009.
- Phase 5 needs both stories' config edits in place (it validates the
  combined diff).
- Phase 6 is linear: T016 → T017 → T018 → T019 → T020 → (T021, T022 in
  parallel) → T023; T024 any time after T020; T025 waits for the first rc
  tag, whenever that happens.
- Days-scale wait sits between T022 and T023 (Microsoft review). Nothing
  else waits on it; Scoop is live from T020 onward.

## Parallel Example: User Story 1

```bash
# After T004, in parallel:
Task: "Env line + comment fixes in .github/workflows/release.yml"
Task: "Correct Env row in specs/010-.../contracts/release-workflow.md"
Task: "Correct token rows in specs/010-.../data-model.md"
Task: "Correct Phase 3 preconditions in specs/010-.../quickstart.md"
```

## Implementation Strategy

One PR, one tag. US1 alone is a valid MVP (winget only, the P1 goal), but
US2 costs four small edits and is what makes SC-001 (install on release
day) true, so both ship together as the spec decided. Stop points: after
T015 (everything rehearsed, nothing shipped), after T018 (merged, not
tagged), after T021 (Scoop proven, winget pending Microsoft).
