---

description: "Task list for 015-cwd-livekit-defaults"
---

# Tasks: Work Inside The Agent Folder, LiveKit By Default

**Input**: Design documents from `specs/015-cwd-livekit-defaults/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/cli-surface.md](contracts/cli-surface.md), [quickstart.md](quickstart.md)

**Revised 2026-08-16** after two rounds of cross-artifact analysis. Three
corrections changed real work, so read them before starting:

1. **The telephony "regression" was never one, and there is no guidance work
   either.** Two drafts of this plan predicted what an author would see on the
   LiveKit phone path and both were wrong; the third pass ran it. The path
   stays gated on both targets and LiveKit's message is already specific
   (`connections/phone.yaml: connection "phone" declares no transport`). No
   code change. The only consequence is that
   `TestRunTelephonyCreateGatedOnConnection` fails on its target-coupled
   message assertion while its blocking assertion still passes, so the message
   half is updated and the blocking half is not (research.md D11, measured).
2. **The maintain menu must not follow the default.** It preselects the
   package's own target (FR-011). Note the case that is broken *today* is
   opening a **LiveKit** package, which highlights Pipecat; a Pipecat package
   looks correct only because Pipecat happens to sit at `options[0]`.
3. **Reordering a menu breaks positional tests.** The accessible renderer takes
   numeric input, so `TestRunSelectTarget` selects the target by ordinal.

**Tests**: Included, and not optional. The constitution requires that
non-trivial logic leaves one runnable check behind, and CLAUDE.md makes "a rule
with no gate is a wish" a hard rule. Contract IDs (C1–C10) refer to
[contracts/cli-surface.md](contracts/cli-surface.md); decision IDs to
[research.md](research.md).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1, US2, US3 per spec.md
- Every task names its exact file path

---

## Phase 1: Setup

- [x] T001 Run `make fmt && make lint && make build && make test` and confirm green before editing. Verified green on 2026-08-16, so any later red is attributable to this change

---

## Phase 2: Foundational (Blocking Prerequisites)

**None required.** The cwd default (US1) touches `internal/cli/` and two
scaffold comment lines; the LiveKit default (US2) touches
`internal/scaffold/` and `internal/tui/`. Either ships alone. The only shared
file is `internal/scaffold/testdata/golden/init.txt`, which both regenerate —
a sequencing note, not a prerequisite. Proceed to User Story 1.

---

## Phase 3: User Story 1 - Commands work inside the agent folder (Priority: P1) 🎯 MVP

**Goal**: `validate`, `compile`, and `dev` run against the current directory
with no positional argument.

**Independent Test**: scaffold, `cd` in, run all three with no argument.

### Tests for User Story 1

> C5 and C9 pass today — they are regression guards, not red-first tests. C1,
> C4, C6, C7, C8a, and C10 fail at the cobra arity layer before any Unmute code
> runs, which is the reported bug. Drive everything through the real command
> tree via the existing `runValidateCommand`-style helpers so the package still
> compiles before T004 lands.

- [x] T002 [US1] Create `internal/cli/package_dir_test.go` covering C1 (zero-arg `validate`), C4 (non-package: message names `agent.yaml`, names the absolute directory, shows both usage forms, and is **not** `accepts 1 arg(s), received 0`), C5 (two args rejected), C6 (zero-arg `compile` writes under the cwd), C8a (`--target` on zero-arg `validate` and `compile`), C9 (explicit missing directory keeps today's wording), and **C10** (zero-arg `compile` from inside `build/<target>/` fails with the C4 message and does **not** recompile the parent). Add an L1 table test for the display helper, which is a pure function and otherwise ships untested because `printHeader` is TTY-gated. Use `t.Chdir`; resolve fixture paths to absolute before chdir or copy into `t.TempDir()`; no `t.Parallel` alongside `t.Chdir`
- [x] T003 [P] [US1] Add zero-arg `dev` tests for C7 and C8 to `internal/cli/dev_test.go`, covering `--target`, `--console`, and `--var`

### Implementation for User Story 1

- [x] T004 [US1] Create `internal/cli/package_dir.go` with `packageDir(cmd *cobra.Command, args []string) (string, error)`. **The `cmd` parameter is required**: the C4 message names the command twice, and a helper holding only `args` cannot know which of the three called it (research.md D2, corrected). One argument returns it verbatim with no extra checks, preserving C9. Zero arguments resolves `.`, guards it with an `os.Stat` of `agent.yaml`, and reports the absolute path via `filepath.Abs` — wrap an `Abs` failure with `%w` rather than falling back to a relative path in a message that promises an absolute one. No parent search (D3). Add the D5 display helper in the same file
- [x] T005 [US1] Wire `internal/cli/validate.go`: `Args: cobra.MaximumNArgs(1)`, `Use: "validate [package-dir]"`, resolve through `packageDir`, display helper in `printHeader`
- [x] T006 [P] [US1] Wire `internal/cli/compile.go`: `MaximumNArgs(1)`, `Use: "compile [package-dir]"`. Confirm `build/<target>/` still lands under the resolved directory (`compile.go:55`)
- [x] T007 [P] [US1] Wire `internal/cli/dev.go`: `MaximumNArgs(1)`, `Use: "dev [agent-dir]"`, resolve at `dev.go:39` before the `--var` preload at `dev.go:55-67`
- [x] T008 [US1] Add a `Long:` field to all three commands stating that omitting the argument uses the current directory. Brackets alone say the argument is optional but never what the default is, which spec.md's help-text edge case requires. None of the three has a `Long:` today. Same files as T005–T007, so sequential
- [x] T009 [US1] Drop the `.` from `internal/scaffold/templates/agent.yaml.tmpl:1-2` and add "from this directory", matching the qualifier `env.example.tmpl` already carries — without it the comment loses the context the dot supplied
- [x] T010 [P] [US1] Drop the `.` from `internal/scaffold/templates/env.example.tmpl:2-3`, keeping its existing "from this directory"
- [x] T011 [US1] Regenerate the scaffold golden after T009/T010: `go test ./internal/scaffold -update`; confirm only comment lines moved
- [x] T012 [US1] Regenerate the help golden after T005–T008: `go test ./internal/cli -run TestHelpCaptureMatchesBinary -update`; confirm `specs/008-mintlify-user-docs/help.txt` shows the bracketed forms and the new `Long:` text
- [x] T013 [US1] Verify: `go test -race ./internal/cli ./internal/scaffold` green, and walk C1–C10 by hand per [quickstart.md](quickstart.md)

**Checkpoint**: the reported bug is fixed and every explicit-argument invocation is unchanged. Shippable alone.

---

## Phase 4: User Story 2 - New agents are LiveKit-based by default (Priority: P2)

**Goal**: `unmute init my-test` scaffolds a LiveKit package that validates,
compiles, and runs untouched.

**Independent Test**: scaffold with defaults, validate, expect `✓ livekit (livekit)`.

### Tests for User Story 2

> The two halves of T015 go red at different moments, which is fine but worth
> knowing. **T015(a)** (create menu) passes today — `options[0]` is Pipecat and
> `DefaultTarget` is `"pipecat"` — then goes red the instant T018 flips the
> constant, and green again at T019. That drift is exactly what it exists to
> catch. **T015(b)** (maintain menu, opened on a LiveKit package) is red from
> the moment it is written, because that case is broken today, and goes green
> at T020.

- [x] T014 [P] [US2] Make `TestScaffoldAgreesWithItsOwnBuild` (`internal/scaffold/scaffold_test.go:121-134`) table-driven over both targets. It uses `name := DefaultTarget`, so after the flip the `.env.example`-versus-build agreement check would silently cover LiveKit only, and Pipecat would lose it. `TestPreflightShippedTargets:620` already models the two-target loop
- [x] T015 [P] [US2] Create `internal/tui/default_target_test.go` covering **both halves of FR-011**, which are different rules and must not be conflated: (a) the **create** flow's target menu (`tui.go:231-233`) has `scaffold.DefaultTarget` first — Principle III makes this mandatory, because ordering the menu states the default a second time; (b) the **maintain** flow preselects the package's own target. **Open a LiveKit package for (b), not a Pipecat one.** Pipecat sits at `options[0]` in `maintain.go:556` today, so a Pipecat package appears correct by coincidence and such a test passes both before and after T020 — gating nothing. The case that is genuinely broken today is opening a LiveKit package, which highlights Pipecat. New file, so no collision with T017
- [x] T016 [P] [US2] Update `internal/cli/init_test.go:61` from `"✓ pipecat (pipecat)"` to the LiveKit form; fix the stale comments at `:56` and `:88`. No `-update` path exists for this string
- [x] T017 [P] [US2] Fix `TestRunSelectTarget` at `internal/tui/tui_test.go:528-542`. Its input `"1\nagent\n1\n1\n2\n7\n\n"` selects the target menu **by ordinal** — the `2` picks the second option — and it asserts the result is `livekit`. T019 reorders that menu, so `2` becomes Pipecat and this goes red. Update the ordinal and the comment at `:531`. Before finishing, grep every accessible-renderer test input that traverses the target menu, because any `huh.Option` reorder invalidates all of them

### Implementation for User Story 2

- [x] T018 [US2] Flip `DefaultTarget` from `"pipecat"` to `"livekit"` at `internal/scaffold/scaffold.go:80`
- [x] T019 [US2] Order the **create** flow's target options by `scaffold.DefaultTarget` at `internal/tui/tui.go:231-233`. `selectOne` takes `options[0].Value` positionally on both the interactive (`tui.go:3223`) and accessible (`tui.go:3232`) paths and never reads `data.Target`, so reordering is what moves the preselect
- [x] T020 [US2] Fix the **maintain** flow at `internal/tui/maintain.go:554-563` to preselect `data.Target`, the package's own current target — **not** `DefaultTarget`. `editTarget` already holds `data` and compares against `data.Target` on line 560, so the value is in hand. Ordering this menu by the default would make editing a Pipecat package highlight LiveKit, which no requirement authorizes and FR-011 forbids. This is a pre-existing wart the flip would otherwise make visible. Gated by T015(b)
- [x] T021 [US2] **No code change. Confirm only.** Add no `Transport = "sip"` arm and write no new message: measured on 2026-08-16, LiveKit's wizard phone path already produces a specific, actionable error naming the file and the missing field (`connections/phone.yaml: connection "phone" declares no transport…`, from `internal/ir/build.go:101`). It never reaches `unsupported telephony route`, which two earlier drafts of this plan both predicted wrongly. FR-010 is satisfied by existing behaviour; confirm that by hand and move on
- [x] T022 [US2] Update the message half of `TestRunTelephonyCreateGatedOnConnection` (`internal/tui/tui_test.go:1019-1043`) to be target-aware. Measured: with the LiveKit default it **fails** at `:1041`, because its second assertion pins Pipecat's wording (`"cannot receive them"`) while a different, earlier guard now fires. **Leave the blocking assertion at `:1038` exactly as it is** — `got.Confirmed` must still be false, and it still is on both targets, so the gate itself is intact. Assert the block plus a message that names the connection file and the missing field, which holds for both targets. Updating a target-coupled string while the behavioural assertion stands is tightening, not the gate-loosening CLAUDE.md forbids. Fix the now-wrong comment at `:1022-1026`, which says the wizard "cannot supply the carrier" — on LiveKit it cannot supply the transport
- [x] T023 [US2] Regenerate the scaffold golden: `go test ./internal/scaffold -update`. Confirm exactly three regions moved: the `agent.yaml` turn block, `targets.yaml` (provider, `version: "1.5.2"`, new `sdk_language: python`), and `.env.example` growing to five keys
- [x] T024 [US2] Confirm no other golden moved. `internal/tui/testdata/golden/console_models_80x24.txt`, the `internal/generate` goldens, and `internal/ir/testdata/golden/compiler.txt` are sourced from fixtures and the catalogue, not the scaffold (D13). Do not run `-update-pipecat` or `-update-catalog`. Also confirm `internal/skill/agreement_test.go:446` stays green — it calls `scaffold.Write` with no `Target`, so it is an implicit `DefaultTarget` consumer that D13's enumeration missed
- [x] T025 [US2] Verify: `go test -race ./internal/scaffold ./internal/tui ./internal/cli` green; scaffold by hand and confirm `✓ livekit (livekit)`, a `build/livekit/` project, that `unmute init` with no name highlights LiveKit, and that editing an existing Pipecat package still highlights Pipecat

**Checkpoint**: fresh packages are LiveKit, Pipecat is still fully selectable, and the maintain flow respects existing choices.

---

## Phase 5: User Story 3 - Naming a folder still works from outside (Priority: P3)

- [x] T026 [US3] Add the C2 and C3 multi-agent tests to `internal/cli/package_dir_test.go`: two packages side by side, `validate a` and `validate b` from the parent each hit their own, and `validate ../b` from inside `a` resolves to `b`. Same file as T002, so sequential
- [x] T027 [US3] Confirm nothing was weakened: `go test -race ./...` green with no test skipped or assertion loosened, and `git diff` over `internal/**/*_test.go` shows only additions plus the intentional changes from T014, T016, and T017

---

## Phase 6: Close the gate gap

- [x] T028 Extend `TestDocsSiteCLIPagesQuoteHelp` at `internal/cli/help_capture_test.go:76` to assert the `Usage:` line. Its loop at `:103-105` skips every line not starting with `-`, so it checks flags and never the usage string
- [x] T029 Fix the pages the stricter test flags: `docs-site/reference/cli/validate.mdx:11`, `compile.mdx:11`, `dev.mdx:11`
- [x] T030 Prove the new assertion bites: revert one page by hand, watch `go test ./internal/cli -run TestDocsSiteCLIPagesQuoteHelp` fail, restore it. A gate nobody has seen fail is not yet a gate
- [x] T031 Add both new gates to the table in `CLAUDE.md`: the `Usage:`-line assertion (`internal/cli/help_capture_test.go`) and the console-versus-`DefaultTarget` agreement test (`internal/tui/default_target_test.go`). Every comparable gate has a row today, and CLAUDE.md's own rule is that a new rule wires its check in the same change

---

## Phase 7: The five-places documentation sweep

**Read before editing.** An explicit directory argument is still **correct**
wherever the reader is not standing in the package: commands run from the
repository root against `examples/`, and the emitted `build/<target>/README.md`
whose reader is inside the build directory. Only two things change: grammar
restatements, and surfaces that teach the first-run workflow. Do not
mechanically strip arguments.

### Argument grammar

- [x] T032 Update the grammar table in `internal/skill/assets/references/workflow.md:10-12` and the step text at `internal/skill/assets/SKILL.md:46`. This surface is ungated for arity — `TestSkillBundleNamesRealCommands` checks that commands and flags exist, never their arity or a flag's value — so read it rather than trusting a green run. **Not `[P]`**: T035 and T039 also write `references/workflow.md` and `SKILL.md`. All three skill-bundle tasks are sequential with each other
- [x] T033 [P] Update `docs-site/start/how-unmute-works.mdx:39`, which restates `unmute compile <package>`

### `docs/TESTING.md` (one task: two separate concerns, one file)

- [x] T034 Update `docs/TESTING.md` for both changes, in one pass to avoid a write collision: the command-surface grammar at `:463-466`, **and** the `unmute init` walkthrough at `:201-242`, whose line 210 says `# expect: pipecat-dev  pipecat  pass`, line 217 reads `$agent/build/pipecat-dev/bot.py`, and line 241 pins `--target pipecat-dev`. Both are falsified by the LiveKit default, and line 210 is already stale from SCHEMA N15, which dropped the `-dev` suffix

### The LiveKit default

- [x] T035 Update `internal/skill/assets/` for the new default: the transcript at `references/workflow.md:47-52` showing `✓ pipecat (pipecat)`, and every scaffolded-package invocation. Nothing in the bundle currently knows the default moved, and CLAUDE.md's own words apply — a feature the skill does not know about is a feature no coding agent will use. **Not `[P]`**: shares `references/workflow.md` with T032 and T039
- [x] T036 [P] Update `docs-site/start/quickstart.mdx`: teach `cd my-test` then argument-free commands as the primary path, and fix the LiveKit drift — the scaffolded file list, `✓ pipecat (pipecat)` at `:60`, `build/pipecat` at `:73, :77, :101, :121`, `?agent=pipecat` at `:76`, and the two-key sentence at `:46-48` that is false now `.env.example` carries five
- [x] T037 [P] Update `docs-site/start/coding-agents.mdx`, a full first-run transcript showing `✓ pipecat (pipecat)` at `:174`, `compiled salon/build/pipecat` at `:192`, `?agent=pipecat` at `:195`, and `salon/build/pipecat/dev.log` at `:196`. It is the page that teaches the skill and was missing from the sweep entirely
- [x] T038 [P] Update `docs-site/reference/cli/init.mdx` around `:64-68`, not `:20-30`: the scaffolded-file list at `:20-30` is target-agnostic and needs nothing, while `:64` (`.env.example` | starter keys for the scaffolded target) and the sentence at `:67-68` claiming one `SLNG_API_KEY` covers the keys both become incomplete once a LiveKit scaffold also writes three `LIVEKIT_*` names. Also update `README.md:153-155`, which teaches `unmute validate my-agent` / `unmute dev my-agent` right after an `init` and repeats the two-key claim

### FR-009: no surface pins a target a scaffolded package lacks

- [x] T039 Remove `--target pipecat` from every invocation aimed at a **scaffolded** package. A scaffolded package declares one instance named after its provider, so after the flip these fail with `target instance "pipecat" is not declared`. Delete the flag rather than swapping the value: `internal/cli/dev.go:422` returns the sole target when only one is declared, so the flag is unnecessary and an omitted flag cannot go stale again. Sites: `internal/skill/assets/SKILL.md:51, 64, 76`, `references/workflow.md:118, 157`, `references/prompting.md:358`, `docs-site/dev/webhooks-and-tunnels.mdx:43, 84`, `docs/TELEPHONY.md:915`, and a tenth that is not even a valid instance name — `docs/TESTING.md:241` pins `--target pipecat-dev` against the scaffolded `"$agent"`, stale since SCHEMA N15 dropped the `-dev` suffix. It falls inside T034's range, so coordinate: whichever task reaches it first fixes it. Leave `--target pipecat` alone wherever it targets `examples/`, which declare their own pipecat instance

### Teach what the flip makes newly relevant

- [x] T040 [P] Document the two-file target switch in `docs-site/targets/overview.mdx`, the page that owns `targets.yaml` and the "one package, two runtimes" story and is therefore where an author looks before switching by hand. Say that `targets.yaml` and the turn block in `agent.yaml` move together, or validation fails with `turn model "silero" is not recognized`. Under the old default this never bit; under the new one it is the most likely "I want Pipecat" action (spec.md US2 scenario 4). While there, check the invocations at `:67, :81` — they target `examples/`, so they keep their explicit paths. This page is in no other task, so no collision
- [x] T041 [P] Update `docs-site/dev/overview.mdx` (`:11, :80, :114, :120, :145`, modes table `:163-165`) and `docs-site/dev/console.mdx:7` to the in-folder form where they teach a scaffolded workflow
- [x] T042 [P] Update `docs-site/reference/cli/overview.mdx:52-55`, whose existing "running unmute with no arguments" story is about the root command, so the new per-command behaviour reads consistently beside it

### Verify, do not rewrite

- [x] T043 Confirm the emitted runbook templates keep `<source-dir>` at all thirteen sites — `internal/generate/templates/pipecat_v1/README.md.tmpl` (`:83, :87, :127, :424, :425, :429, :436, :700, :799, :800, :806`) and `livekit_v1/README.md.tmpl` (`:31, :72`). Their reader stands in `build/<target>/`. Inventory re-derived by grep on 2026-08-16: an earlier draft listed `:124` and livekit `:69` (neither contains `<source-dir>`) and omitted `:429, :436, :806`. A confirm-only task with a wrong inventory confirms nothing. This also keeps the hardcoded assertion at `internal/generate/pipecat_carrier_telephony_test.go:228` still, which has no `-update` path
- [x] T044 [P] Confirm these keep their explicit paths because they run from the repository root: `docs-site/build/your-first-agent.mdx:240, 252` (`unmute validate examples/simple-prompt`), `docs/HARNESS_TEST.md:86-87, 104-105`, `docs/TESTING.md:406-408, 478-480`, `docs/TRANSFERS.md:488, 526`, and all eleven `examples/*/README.md`. An earlier draft told you to convert `your-first-agent.mdx` to the in-folder form; that would have made a correct page wrong
- [x] T045 [P] Check the two pages quoting the Daily-route refusal verbatim — `docs-site/transfers/pipecat-daily.mdx:91` and `docs-site/dev/telephony.mdx:113` — against the generating string in `internal/ir/build.go:881-885`. The wording does not change, but confirm T021 did not move it

---

## Phase 8: Ship

- [x] T046 Run the gate in order: `make fmt && make lint && make build && make test`. All four must pass. `make smoke` is the fifth constitutional target but stays opt-in and out of the PR gate
- [x] T047 Walk [quickstart.md](quickstart.md) end to end against a freshly built `bin/unmute`, including the original reproduction (`init` → `cd` → `validate`) and the LiveKit scaffold checks
- [x] T048 Confirm no unintended drift: `git diff --stat` shows only `internal/scaffold/testdata/golden/init.txt` and `specs/008-mintlify-user-docs/help.txt` among golden files
- [x] T049 File a short dated amendment in `docs/SCHEMA.md` recording that the scaffold's default target is `livekit` from 2026-08-16, that no authoring shape changes, and that no existing package fails strict decode. Strictly optional under Principle IV, since the authoring surface is unchanged — but SCHEMA.md's four most recent amendments (N20, N33, N43, N44) all record no-shape-change behaviour changes, and N43 (2026-08-15) is about the LiveKit turn detector this default now selects for every new package. Cheap, and in step with how the document has been kept
- [ ] T050 Open the pull request to `main` from `feat/unmute-cli-workdir-livekit-53d1c2`, describing both behaviour changes, the two new gates, and the corrected telephony finding. State the SCHEMA position as a judgment call with its reasoning, not as settled fact

---

## Dependencies & Execution Order

### Phase dependencies

- **Phase 1**: no dependencies
- **Phase 2**: empty by design
- **Phase 3 (US1)** and **Phase 4 (US2)**: depend only on Phase 1, and are independent of each other
- **Phase 5 (US3)**: depends on US1
- **Phase 6**: depends on US1's `Use:`/`Long:` changes (T005–T008)
- **Phase 7**: the CLI reference pages depend on Phase 6; the rest depends on whichever story shipped
- **Phase 8**: depends on everything included in the release

### The one cross-story file

`internal/scaffold/testdata/golden/init.txt` is regenerated by T011 (US1) and
T023 (US2). With both in flight, regenerate once after both land.

### Within each story

US1: T002, T003 → T004 → T005, T006, T007 → T008 → T009, T010 → T011, T012 → T013.
US2: T014–T017 → T018 → T019 → T020 → T021 → T022 → T023, T024 → T025.

### Not parallel, despite appearances

T002 and T026 share `internal/cli/package_dir_test.go`. T005–T008 share the
three command files. T017 and T015 would share `tui_test.go`, which is why T015
gets its own file. T034 deliberately merges two concerns because both write
`docs/TESTING.md`.

**The three skill-bundle tasks — T032, T035, T039 — all write
`internal/skill/assets/references/workflow.md` and `SKILL.md`.** None carries
`[P]`; run them in that order. An earlier draft marked T032 and T035 parallel,
which would have had two writers on one file.

---

## Implementation Strategy

### MVP: User Story 1 only

T001, then T002–T013, then Phase 6 and the grammar tasks (T032, T033, the
grammar half of T034). That answers the original report and nothing else moves.
US2 is a separate increment with its own risk and can follow.

### Incremental delivery

Setup → US1 (ship) → US2 (ship) → US3 → sweep and gates (ship).

### Parallel team strategy

After T001: one person takes US1 (`internal/cli/` plus two template comments),
the other takes US2 (`internal/scaffold/`, `internal/tui/`). They meet at the
shared golden. The doc sweep splits by file, except the three noted collisions.

---

## Notes

- The two gates that matter most are T015 (create-menu agreement, without which
  the default silently drifts) and T028 (the `Usage:` gate, without which this
  feature's own doc changes rot). T031 records both where the repo records gates
- Hand edits with no `-update` path: T016 and T017. Every other golden move is
  mechanical
- Do not hide the fresh-scaffold warning `LiveKit turn placement is a
  preference`. Principle II forbids it; D7 records the decision
- Do not rewrite `TestRunTelephonyCreateGatedOnConnection`'s expectation (T022).
  A failing gate gets the code fixed
- Commit after each task or logical group; stop at any checkpoint to validate a
  story on its own
