---

description: "Task list for Complexity Cleanup"
---

# Tasks: Complexity Cleanup

**Input**: Design documents from `/specs/011-complexity-cleanup/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/sweep-report.md

**Tests**: No TDD tasks. This feature changes no behavior, so there is nothing new to test-drive. The existing suite is the regression net (FR-003), and the L5 sweep built in Phase 2 is the acceptance evidence the spec explicitly asks for in FR-029 through FR-038.

**Organization**: Grouped by user story. Stories run in priority order and each is separately revertible (SC-006).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel — different files, no dependency on an incomplete task
- **[Story]**: US1–US5, on user-story tasks only
- Every task names the exact file it touches

## Path Conventions

Go module at repository root. Maintained code in `internal/`, one file per command in `internal/cli/`. No `src/` or `tests/` directory; Go tests sit beside the code they exercise.

---

## Phase 1: Setup (Baseline and Tooling)

**Purpose**: Capture what "unchanged" means before anything changes. Nothing here edits maintained code.

- [X] T001 Verify tooling and record versions in `specs/011-complexity-cleanup/baseline.md`: `go version` (expect 1.24), `docker info`, `npx playwright --version`, and `ruff --version`. The ruff version is load-bearing — formatting differs between versions and a mismatch makes every later byte comparison meaningless (research.md R3).
- [X] T002 Supply `FIRECRAWL_MCP_URL` in the repository root `.env`. Closes FR-038; `mcp-example` declares it and without it both its sessions fail under FR-035. **Done 2026-08-15 — all 25 required names now present.**
- [X] T003 Confirm the starting tree is green: `make fmt && make lint && make build && make test`. A red baseline must be fixed or recorded before any cleanup, so a pre-existing failure is never blamed on this feature.
- [X] T004 Record the baseline commit sha in `specs/011-complexity-cleanup/baseline.md` and compile all eleven examples into a baseline tree in the scratch directory, one build directory per example and target. Keep it outside the repository so it can never be committed.
- [X] T005 [P] Record baseline metrics in `specs/011-complexity-cleanup/baseline.md`: `go run golang.org/x/tools/cmd/deadcode@latest -test ./...` (expect two unreachable functions) and non-test Go line count (expect 20,901). These are the before-numbers for SC-002 and SC-003.

**Checkpoint**: Baseline recorded. Nothing has changed yet.

---

## Phase 2: Foundational (L5 Sweep Harness)

**Purpose**: Build the acceptance harness. **Blocking** — FR-033 requires a full sweep after User Story 1, so the harness must work before the first line of cleanup lands.

**⚠️ CRITICAL**: No user story work begins until T013 passes.

- [X] T006 [P] Create `internal/testdata/sweep/greeting-probe.js`: launch headless Chromium with `--use-fake-device-for-media-stream`, `--use-fake-ui-for-media-stream`, and `--autoplay-policy=no-user-gesture-required`; open the page URL from argv; poll `RTCPeerConnection.getStats()` until an inbound audio track reports `bytesReceived > 0` or the timeout expires; print one JSON line and exit 0 or 1 per `contracts/sweep-report.md`. Load the shipped page unmodified — a page changed to make itself testable proves nothing.
- [X] T007 Create `internal/cli/example_sweep_test.go` with `//go:build sweep` and a `TestExampleSweep` root, following the structure of the existing `internal/cli/dev_web_smoke_test.go`. Implement the preflight subtest: Docker reachable and Playwright present both **block**; a missing secret name or a wrong working directory **fails**. The distinction is FR-035 and the constitution's fail-loud rule — missing tooling says nothing about the code, a missing secret means the sweep cannot deliver its claimed coverage.
- [X] T008 In `internal/cli/example_sweep_test.go`, discover examples by reading each package under `examples/` and deriving `Runnable` from whether it declares a telephony channel. Derive it, never hand-maintain a list, so an example added later is classified correctly without editing the harness (data-model.md, Example).
- [X] T009 In `internal/cli/example_sweep_test.go`, add the per-example `--var` table. `salon-support` needs `customer_name` and `customer_id`; its greeting is `Hi {{customer_name}}` from `source: call_start` and would otherwise render malformed. Read `examples/multi-task/agent.yaml` and add the same treatment if its `customer_id`/`customer_name` are `call_start` (research.md R5, open item).
- [X] T010 In `internal/cli/example_sweep_test.go`, implement the session runner: `unmute dev <example> --target <t> --no-open` from the repository root, wait for the dev server, run the probe from T006, then tear the containers down on every exit path. Run serially on one port — no allocation, no name collisions, no interleaved logs (research.md R6). Do not copy `.env`: `devChildEnv` (`internal/cli/dev.go:447`) already reads `$CWD/.env`, which satisfies FR-036 with no harness code (research.md R4).
- [X] T011 [P] In `internal/cli/example_sweep_test.go`, implement the byte comparison against the T004 baseline, excluding `.env` and `livekit*.toml`. Those two are `preservedPatterns` (`internal/cli/compile.go:155`) and are restored on purpose, so comparing them compares the sweep against itself.
- [X] T012 [P] In `internal/cli/example_sweep_test.go`, write the JSON report to the scratch directory per `contracts/sweep-report.md`. `missingSecrets` holds **names only**; no field may carry a value (FR-036). Derive `verdict` rather than setting it — `green` requires all thirteen sessions passed and a clean byte diff, so a blocked sweep reports `incomplete` and can never read as a pass.
- [X] T013 Run the baseline sweep: `go test -tags sweep ./internal/cli/... -run TestExampleSweep -timeout 90m`. Must be Green across thirteen sessions. A failure here is pre-existing and must be understood before any cleanup starts.

**Checkpoint**: Harness works, baseline sweep green. Cleanup can begin.

---

## Phase 3: User Story 1 — Nothing in the tree is unreachable (Priority: P1) 🎯 MVP

**Goal**: Remove the three things nothing can reach, so "no callers" reliably means "does not exist".

**Independent Test**: `deadcode -test ./...` reports zero unreachable functions; no reference to the removed script survives outside historical spec records.

- [X] T014 [P] [US1] Delete `Warned` and `Ok` from `internal/style/style.go:72,75`. **Keep the `Warn` and `Success` constants** and `style_test.go` unchanged — the token table is the single source of colour, and an unused token is not dead code (spec Edge Cases).
- [X] T015 [P] [US1] Delete `Run` from `internal/tui/tui.go:49`. Only `RunConsole` and `RunCreate` are called, both from `internal/cli/init.go:44,47`.
- [X] T016 [P] [US1] Delete `preflight.sh` from the repository root. No build target, no workflow, and no current document references it, and its default argument `examples/human-transfer/build/livekit` names a directory renamed to `livekit-human-transfer`.
- [X] T017 [US1] Before deleting anything above, confirm each name is absent from `internal/generate/templates/**` and `internal/scaffold/templates/**` as well as from Go code. Template `FuncMap`s bind helpers by bare name, so a parenthesised search reports live helpers as dead — this false-positived during the audit itself (spec Edge Cases).
- [X] T018 [US1] Run the gate (`make fmt && make lint && make build && make test`), confirm `deadcode -test ./...` now reports zero, then run the full sweep. Record the report as story P1.

**Checkpoint**: User Story 1 complete and independently revertible.

---

## Phase 4: User Story 2 — Each repeated fact has one home (Priority: P2)

**Goal**: Collapse seven behaviors that exist as two or more near-identical copies, each a place where a fix can land in one copy and miss the other.

**Independent Test**: For each behavior, one definition remains and every former call site reaches it; goldens match without regeneration.

- [X] T019 [US2] Extract the shared signing sequence into `internal/cli/livekit_token.go` as `signJWT(secret string, claims any) (string, error)` — marshal, fixed HS256 header, HMAC, base64url concat. Rewrite `mintLiveKitToken` (`livekit_token.go:49`), `mintLiveKitSIPAdminToken` (`dev_livekit_sip.go:108`), and `mintLiveKitDispatchToken` (`dev_livekit_sip.go:324`) to keep only their own claim struct. The three existing tests already recompute the signature against the secret with a fixed clock and must pass untouched.
- [X] T020 [P] [US2] Merge `isTTY` (`internal/cli/dev.go:396`) and `isCharDevice` (`internal/cli/init.go:32`) into one helper in `internal/cli/dev.go`. Same `Stat()` and `ModeCharDevice` check, two functions in one package. Update the four call sites in `dev.go:365`, `init.go:21`, `ui.go:44`, and `root.go:31`.
- [X] T021 [US2] Merge `call` (`internal/cli/dev_livekit_sip.go:213`) and `callDispatch` (`:346`) into one method taking the service path. They are byte-identical apart from `/twirp/livekit.SIP/` versus `/twirp/livekit.AgentDispatchService/`. Sequential after T019 — same file.
- [X] T022 [P] [US2] Merge `checkLiveKitVersion` (`internal/generate/livekit_v1.go:574`) and `checkPipecatVersion` (`internal/generate/pipecat_v1.go:554`) into one `checkVersion(name, version string, pattern *regexp.Regexp, major, minMinor int)`. Identical apart from a name and three constants. Error text must stay byte-identical — it reaches users.
- [X] T023 [US2] Delete `pipecatEnvRef` (`internal/generate/pipecat_v1_build.go:1231`) and keep one shared env-lookup helper, since both it and `livekitEnvRef` (`livekit_v1_build.go:1031`) return the identical string.
- [X] T024 [US2] Replace the five scattered type mappings with one table in `internal/generate` keyed by `ir.PrimitiveType`, carrying the JSON name, the Python name, and the isinstance check. Covers `jsonType` (`pipecat_v1_build.go:933`), `pyType` (`:1310`), `livekitTypeCheck` (`livekit_v1_build.go:588`), `resultPyType` (`:1213`), and `jsonPyType` (`:1223`). **Note**: these are three different outputs over one key set, not one repeated mapping — the single source is the table, and the three accessors stay. Sequential after T023 — same files.
- [X] T025 [US2] Replace the body of `sortedKeys` (`internal/generate/pipecat_v1_build.go:762`) with `slices.Sorted(maps.Keys(set))`, matching the already-correct generic version at `internal/ir/build.go:1343`. Sequential after T024 — same file.
- [X] T026 [US2] Run the gate, confirm zero goldens regenerated and the byte diff is clean, then run the full sweep. Record as story P2. This sweep matters most of the five: T019 changes the token minting that `/api/session` calls at `internal/cli/dev_web.go:275`, and a live LiveKit session is the only check that the merged signing still produces a token a real server accepts.

**Checkpoint**: User Stories 1 and 2 both complete and independently revertible.

---

## Phase 5: User Story 3 — The standard library does the standard-library work (Priority: P3)

**Goal**: Replace five hand-written re-creations of things the language already ships.

**Independent Test**: Each named helper is gone, call sites use the standard equivalent, and `go vet` reports no API newer than the module's declared language version.

- [X] T027 [US3] Replace `containsName` (`internal/tui/tui.go:2892`) with `slices.Contains` at its seven call sites, then delete it.
- [X] T028 [US3] Replace `removeName` (`internal/tui/tui.go:2901`) with `slices.DeleteFunc` at its nine call sites, then delete it. `deleteResource` already calls `slices.DeleteFunc` for the same job a line away, so the file currently disagrees with itself. Both compact in place and every call site reassigns the result, so behavior is unchanged. Sequential after T027 — same file.
- [X] T029 [US3] Replace `firstNonempty` (`internal/tui/tui.go:474`) with `cmp.Or` at its roughly sixty call sites, then delete it. Semantics are identical: first non-zero value, zero value if none. Sequential after T028 — same file.
- [X] T030 [P] [US3] Replace `firstNonEmpty` (`internal/generate/pipecat_v1_build.go:1365`) with `cmp.Or` at its call sites, then delete it. Same helper, written twice in two packages.
- [X] T031 [P] [US3] Replace `lessLiveKitVersion` (`internal/generate/livekit_v1.go:564`) with `slices.Compare(a[:], b[:]) < 0` and delete it.
- [X] T032 [US3] Run the gate — `go vet` is the guard that every introduced call (`cmp.Or` 1.22, `slices.Sorted`/`maps.Keys` 1.23) is available at the pinned language version — then run the full sweep. Record as story P3.

**Checkpoint**: User Stories 1 through 3 complete.

---

## Phase 6: User Story 4 — No knob that only ever has one setting (Priority: P4)

**Goal**: Remove two parameters, one wrapper, and one alias that tell a reader nothing.

**Independent Test**: Each is gone, callers behave identically, generated output unchanged.

- [X] T033 [US4] Remove the `cat targetcap.Catalog` and `envRef func(string) string` parameters from `resolveService` (`internal/generate/service_call.go:30`). `cat` is `defaultCatalog` at every call site and `envRef`'s two arguments produce identical output. Delete the comment at `service_call.go:10` reserving `cat` for a `providers.yaml` loader that does not exist — that comment is the stated reason Governance asks for, and it does not hold. Update `pipecat_v1_build.go:1237`, `livekit_v1_build.go:1034`, and `catalog_golden_test.go:56,115`.
- [X] T034 [P] [US4] Delete `confirmAction` (`internal/tui/tui.go:2915`) and point its three callers at `internal/tui/tui.go:549,1287,2912` directly at `confirmChoice`. It forwards with an unchanged signature.
- [X] T035 [P] [US4] Replace `shellRequest interface{}` (`internal/tui/shell.go:23`) with `any`.
- [X] T036 [US4] Run the gate and the full sweep. Record as story P4.

**Checkpoint**: User Stories 1 through 4 complete. SC-003's 300-line target should already be met.

---

## Phase 7: User Story 5 — The console's repeated list screens share one flow (Priority: P5)

**Goal**: Collapse five hand-written list screens into one shared flow.

**Independent Test**: Drive each screen through the accessible renderer with no terminal attached; options, order, and labels are unchanged.

**⚠️ Droppable**: highest risk, and SC-003 is met without it. Drop rather than rush.

**DROPPED 2026-08-15, after being attempted and measured.** The five screens
share about ten lines each, fifty in total. A `listScreen[T]` covering them needs
nine parameters plus an extra-rows hook for `editAgents`: roughly forty lines of
helper and eight lines of struct literal per call site, about **eighty lines to
replace fifty**. Building an abstraction that costs more than the duplication it
removes would fail this feature's own test, so the screens stay as they are. Full
reasoning in [results.md](./results.md).

- [ ] T037 [US5] Add a generic `editList` helper to `internal/tui/tui.go` parameterised by title, description, the items, an entry label function, a create path, and a details handler. It must reproduce the existing sequence exactly: one option per saved item, then "Add …", then "← Back", dispatching on the `view:` prefix.
- [ ] T038 [US5] Migrate `editVariables` (`internal/tui/tui.go:731`) to `editList` and confirm `internal/tui/testdata` goldens match without regeneration.
- [ ] T039 [US5] Migrate `editTools` (`internal/tui/tui.go:850`) to `editList`. Sequential — same file.
- [ ] T040 [US5] Migrate `editAgents` (`internal/tui/tui.go:1149`) to `editList`. Sequential — same file.
- [ ] T041 [US5] Migrate `editTasks` (`internal/tui/tui.go:1640`) to `editList`. Sequential — same file.
- [ ] T042 [US5] Migrate `editFallbacks` (`internal/tui/tui.go:2503`) to `editList`. Sequential — same file.
- [ ] T043 [US5] Confirm the interactive path still imports no `huh` (FR-026), run the gate, then run the full sweep. Record as story P5.

**Checkpoint**: All five stories complete.

---

## Phase 8: Polish & Verification

**Purpose**: Prove the claims the spec makes.

- [X] T044 [P] Add `// ponytail:` comments wherever a simplification's reasoning is not obvious from the resulting code (FR-027), so simple reads as intent rather than oversight.
- [X] T045 [P] Confirm SC-003: non-test Go is at least 300 lines below the 20,901 recorded in T005 if User Story 5 shipped, or at least 150 below if it was dropped.
- [X] T045a Confirm SC-006 by doing it, not asserting it: revert the most entangled story alone — US3 or US4, both of which touch `internal/tui/tui.go` alongside other stories — with `git revert` of that story's commits only. Confirm `make build && make test` stays green, then restore. This is the only check that the five stories are genuinely independent rather than merely committed separately.
- [X] T046 [P] Confirm SC-007: `git diff --stat <baseline-sha> -- docs/ docs-site/ examples/` produces no output, and `go mod tidy && git diff --exit-code go.mod go.sum` shows no change.
- [X] T047 [P] Confirm SC-002: `deadcode -test ./...` reports zero unreachable functions.
- [X] T048 Run `make smoke` where `uv` is available, proving emitted Python is unchanged and still valid (FR-004).
- [ ] T049 Run one example by hand and speak to it: `go run . dev examples/simple-prompt --target livekit`. The greeting probe deliberately does not cover a first user turn (FR-030b); this is the two minutes that does. **Outstanding**: it needs a person at a microphone and cannot be automated, so this remains the one unverified claim.
- [X] T050 Walk `specs/011-complexity-cleanup/quickstart.md` end to end and confirm every Definition of Done box.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: no dependencies. T004 must complete before any code changes or there is nothing to compare against.
- **Phase 2 (Foundational)**: depends on Phase 1. **Blocks every story** — FR-033 needs a working sweep after story one.
- **Phases 3–7 (Stories)**: each depends on Phase 2 and on the previous story's sweep being green.
- **Phase 8 (Polish)**: depends on every desired story.

### User Story Dependencies

Stories are **sequential here, not parallel**, which departs from the usual template on purpose:

- Four of the five stories touch `internal/tui/tui.go`, so parallel work would conflict in one file.
- FR-033 requires a full sweep after each story so a regression names its cause. Overlapping stories would destroy that attribution.
- SC-006 requires each story be revertible alone, which sequential commits give and interleaved ones do not.

Dependencies between stories are ordering only. No story needs another's code, which is what makes each revertible.

### Parallel Opportunities

- T005 with T003/T004 (read-only metrics against separate outputs)
- T006 with T007 (different files; the probe is standalone)
- T011 and T012 (separate concerns inside the harness, once T007's skeleton exists)
- T014, T015, T016 (three different files, no shared symbol)
- T020 with T022 (`internal/cli` versus `internal/generate`)
- T030 with T031 (different files in `internal/generate`)
- T034 with T035 (`tui.go` versus `shell.go`)
- T044 through T047 (independent verification commands)

**Blocked from parallel**: anything touching `internal/tui/tui.go` (T027→T028→T029, T037→T042), anything touching `internal/cli/dev_livekit_sip.go` (T019→T021), anything touching the `*_v1_build.go` pair (T023→T024→T025).

---

## Parallel Example: User Story 1

```bash
# Three unrelated files, no shared symbol — safe together:
Task: "Delete Warned and Ok from internal/style/style.go:72,75"
Task: "Delete Run from internal/tui/tui.go:49"
Task: "Delete preflight.sh from the repository root"

# Then, sequentially:
Task: "Confirm no removed name appears in any template FuncMap"
Task: "Run gate, confirm deadcode reports zero, run full sweep"
```

---

## Implementation Strategy

### MVP (Setup + Foundational + User Story 1)

1. Phase 1 — baseline captured
2. Phase 2 — harness built, baseline sweep green
3. Phase 3 — User Story 1
4. **STOP and VALIDATE**: `deadcode` reports zero, sweep green, byte diff clean
5. That is a shippable increment: three dead things gone, nothing else moved

Phase 2 is much larger than the story it unblocks. That is deliberate. The harness is what makes every later claim checkable, and building it after the first cleanup would mean the first cleanup was never verified.

### Incremental Delivery

Each story is one commit, one green gate, one green sweep. Stop after any of them: the tree is smaller, correct, and verified. US5 is the natural stopping point if the console work looks risky on the day.

### If a sweep goes red

The story that just landed caused it — that is what the per-story cadence buys. Revert that story alone (SC-006), confirm the sweep returns green, then retry. Never update the baseline to match new output; the baseline is the claim.

---

## Notes

- **Zero behavior change is the spine.** Every task inherits FR-001 and FR-002: if generated bytes move, the task is wrong. Fix the code, never regenerate the golden.
- `[P]` means different files with no shared symbol — the file-level conflicts above are listed explicitly rather than left to judgement.
- Commit per task or per logical group; every commit leaves the gate green (FR-003).
- No task adds a dependency. T046 checks that with `go mod tidy`.
- Line references are from the audit at commit `1085102`. Re-confirm before editing; they shift as tasks land.
