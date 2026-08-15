---
description: "Task list for the coding-agent skill"
---

# Tasks: Coding-agent skill for building Unmute voice agents

**Input**: Design documents from `specs/011-coding-agent-skill/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: included. The spec asks for them directly (FR-019, FR-035, FR-036)
and the constitution requires that non-trivial logic leaves one runnable check
behind. Every test named here belongs in the default `make test` suite and needs
no Python.

**Organization**: grouped by user story. Phases 4 to 9 are each a body of
written content plus the checks that hold it, so they can be worked in parallel
by different people once Phase 3 lands.

## Clean-room result, 2026-08-15

Eighteen isolated Sonnet agents were each given a project containing the skill
and nothing else: no `examples/`, no `docs-site/`, no repository. Every package
they produced was re-validated independently rather than taken on trust.

**SC-002 is not met. Seven of ten briefs validated clean on the first attempt;
the bar is eight.**

| Result | Count |
|---|---|
| clean on first validate | 7 |
| one fix round | 3 |

All three failures were telephony packages, and all three traced to two
undocumented required-field interactions:

- `capacity.peak_starts_per_second` becomes required the moment a channel is
  `telephony` (3 of 3 failures hit this)
- a warm transfer needs `outbound: true` on the channel, even on an inbound-only
  line (1 of 3)

Both are now documented in `package.md`, `telephony.md`, and `transfers.md`.
Every non-telephony brief passed. Re-running the ten is the only honest way to
claim the bar, and it has not been re-run.

The other checks passed:

| Check | Result |
|---|---|
| T043, four tool kinds | 4 of 4 chose the right execution block |
| T047, three shapes | 3 of 3 right shape, context decision stated each time |
| quickstart 5d, refusal | refused two invented vendors by name; raised the Pipecat per-task-model limit before writing a file |
| T039, quickstart 5e | 16 of 16 prompts voice-shaped, zero raw URLs |
| secrets | no agent wrote a secret value into any package file |

### Product defects the clean room found, all verified directly

These are not skill defects. They are recorded here because the skill had to be
changed to stop asserting things the code does not do.

1. **A `human_transfer` on a browser-only LiveKit package validates clean,
   compiles clean, and is then absent from the generated project.** No error, no
   warning, no trace in `agent.py` or the compile report. This contradicts
   `docs-site/transfers/overview.mdx` ("it never compiles into something that
   quietly does nothing") and constitution Principle II.
2. **The `secrets:` completeness rule is documented but unenforced.** Deleting
   the whole block from a package that uses `OPENAI_API_KEY` and `SLNG_API_KEY`
   still validates clean. `unmute init` scaffolds no `secrets:` block at all.
3. **`unmute init` writes a prompt that contradicts its own package.**
   `instructions.md` says "This is a phone call"; `agent.yaml` declares only
   `web: realtime_audio`.
4. **`unmute init` scaffolds `DAILY_API_KEY`** into a root `.env.example` that
   nothing keeps in sync with `secrets:`.
5. **`build/your-first-agent.mdx` is stale on the default model.** It shows
   `gpt-4o-mini`; the scaffold writes `gpt-4.1-mini`.

## Status

59 of 68 done. Nine are open and every one of them needs something this session
could not supply:

| Task | Why it is open |
|---|---|
| T035, T036, T039, T043, T047, T052 | each is a session with a real coding assistant. The bundle is written and installs; nobody has yet handed it to Claude Code, Codex, Cursor, or Copilot and watched what it builds |
| T064 | `make smoke` fails on two LiveKit tests because a stray container from another session holds port 7880. Every other smoke package passes. Unrelated to this feature, and not fixed by tampering with someone else's container |
| T067 | quickstart steps 1, 2, 3, 4, 6, and 7 are run and pass. Step 5 is the assistant session above |
| T068 | SC-002, SC-004, SC-005, and SC-007 are all measured by watching an assistant work |

Two changes were made against the plan, both from the `/speckit-analyze`
findings:

- **T057 moved into Phase 1.** The pointer body has to exist before Phase 3 can
  install two destinations, so it was written with the other setup files rather
  than in Phase 9.
- **The L3 golden lives at `internal/skill/testdata/golden/skill_install.txt`**,
  not under `internal/generate/`. Every other package in this repository keeps
  its goldens beside itself.

`spec.md`'s FR-004 was also amended to match what shipped: no assistant
detection, both destinations by default, `--agent` to narrow.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel, different files, no dependency on unfinished work
- **[Story]**: which user story the task serves

## Path conventions

Go code in `internal/`, never `pkg/`. One file per command in `internal/cli/`.
The bundle lives in `internal/skill/assets/` and is embedded. Documentation
pages live in `docs-site/`.

---

## Phase 1: Setup

**Purpose**: authorize the fifth command, then stand up the package it lives in.

- [X] T001 Amend Principle V in `.specify/memory/constitution.md`: the command surface is no longer fixed at four, and `skill` is named as a command that touches no package and is not part of the path from nothing to a spoken agent. Prepend a Sync Impact Report and bump the version to 2.1.0, per the amendment procedure in Governance
- [X] T002 Create `internal/skill/skill.go` with the package doc comment, the `//go:embed assets` directive, and the `Bundle` type holding the embedded `fs.FS` and the version string
- [X] T003 [P] Create `internal/skill/assets/SKILL.md` as a placeholder carrying only the three-field frontmatter (`name`, `description`, `metadata`) so the embed directive resolves
- [X] T004 [P] Create `internal/skill/assets/references/.gitkeep` so the references directory exists before content lands
- [X] T005 Run `make build` and `make test` and confirm both are green with the new package present and no command registered yet

**Checkpoint**: the package compiles and the constitution permits what comes next.

---

## Phase 2: Foundational

**Purpose**: the install logic, as pure functions with no cobra in sight. This is
what every user story depends on, and it is all testable without a command.

**⚠️ No user story work starts until this phase is done.**

- [X] T006 Implement the `Manifest` type in `internal/skill/manifest.go`: the `version` string, the `files` map of forward-slash relative path to lowercase hex SHA-256, and JSON marshalling, per data-model.md
- [X] T007 Implement `readManifest` and `writeManifest` in `internal/skill/manifest.go`, converting between forward slashes in the file and `filepath` separators on disk, so a manifest written on Windows still matches on macOS
- [X] T008 Implement the `Destination` type and the two fixed destinations in `internal/skill/skill.go`: canonical at `.agents/skills/unmute` and pointer at `.claude/skills/unmute`, built with `filepath.Join`
- [X] T009 Implement the `Assistant` name to destination mapping in `internal/skill/skill.go` for `claude`, `codex`, `cursor`, `copilot`, and `all`, deduplicating when two names share a destination
- [X] T010 Implement `Plan` in `internal/skill/skill.go`: compute the per-file decision for every file in a destination against the on-disk state and the manifest, returning the full decision set before anything is written, per the install decision table in data-model.md
- [X] T011 Implement `Apply` in `internal/skill/skill.go`: write a destination whole or not at all, removing what it wrote on failure, then write the manifest last
- [X] T012 [P] Write `internal/skill/skill_test.go` covering hashing, manifest round-trip through both path separators, every row of the install decision table, assistant-name deduplication, and rollback leaving no partial destination behind

**Checkpoint**: install logic is complete and tested with no command and no content.

---

## Phase 3: User Story 1 - Install the skill into a project (Priority: P1) 🎯 MVP

**Goal**: `unmute skill install` exists, writes both destinations from the
embedded bundle, and is honest about what it did.

**Independent Test**: run it in an empty directory and in one where a file was
edited by hand; confirm the files appear, that a second run does not destroy
edits, and that the whole thing works with the network off.

### Implementation

- [X] T013 [US1] Create `internal/cli/skill.go` with `newSkillCmd` and its `install` subcommand, using `RunE`, no `os.Exit`, and errors wrapped with `%w`, per the repository's command rules
- [X] T014 [US1] Add the `--agent`, `--dir`, and `--force` flags in `internal/cli/skill.go`, matching `contracts/cli-skill-command.md`
- [X] T015 [US1] Register the command in `internal/cli/root.go` by adding `newSkillCmd()` to the existing `AddCommand` call
- [X] T016 [US1] Implement the output in `internal/cli/skill.go`: one line per file with what happened to it, then the destination summary and the next step, written through `cmd.OutOrStdout()`, with any styling going through `internal/style` and no color literal in this file
- [X] T017 [US1] Implement the error paths in `internal/cli/skill.go`: unknown `--agent` fails naming the value and listing every supported name, an unwritable directory fails with the path and wrapped cause, and locally changed files are refused in one message naming all of them
- [X] T018 [US1] Write `internal/cli/skill_test.go` as L2 tests through the real cobra tree with output captured: fresh install, already current, upgrade from an older version, refusal on a changed file, `--force` overwrite, `--agent codex` writing only the canonical destination, and an unknown agent name

### Feeding the existing agreement tests

These three go red the moment T015 lands. They are part of this story, not
cleanup.

- [X] T019 [US1] Add `{"skill"}` and `{"skill", "install"}` to `helpCommands` in `internal/cli/help_capture_test.go`, and add `skill` to the `pages` map in `TestDocsSiteCLIPagesQuoteHelp`
- [X] T020 [US1] Write `docs-site/reference/cli/skill.mdx` documenting the command and quoting `--agent`, `--dir`, and `--force` exactly as the help prints them
- [X] T021 [US1] Add `reference/cli/skill` to the CLI group in `docs-site/docs.json`, and name the command in `docs-site/reference/cli/overview.mdx`
- [X] T022 [US1] Re-capture the help with `go test ./internal/cli -run TestHelpCaptureMatchesBinary -update` and read the diff before committing
- [X] T023 [US1] Add the L3 golden for the installed file tree in `internal/generate/testdata/golden/skill_install.txt` and the test that pins it

- [X] T024 [US1] Run the quickstart steps 2, 3, and 4 by hand, including the offline install, and confirm each expected outcome

**Checkpoint**: the skill reaches a project. Everything after this is content.

---

## Phase 4: User Story 2 - One brief, one working single agent (Priority: P1)

**Goal**: an assistant with the skill turns a paragraph into a package that
validates, using SLNG to listen and speak and OpenAI to think.

**Independent Test**: give an assistant a short brief and no other help; the
package it writes passes `unmute validate` with zero errors and can be run and
spoken to.

### The entry document

- [X] T025 [US2] Write `internal/skill/assets/SKILL.md` in full, per `contracts/skill-bundle.md`: what Unmute is, the routing table to every reference, the build loop, the default models, the decisions the assistant must state out loud, that the documentation wins, and that `build/` is never edited. Under 500 lines

### References for a first build

- [X] T026 [P] [US2] Write `internal/skill/assets/references/package.md`: the files an author writes and what each is for, with pointers to `reference/agent-yaml` and `reference/targets-yaml`
- [X] T027 [P] [US2] Write `internal/skill/assets/references/models.md`: SLNG to listen and speak and OpenAI to think as the defaults, the vendors that exist per role per target with SLNG first in every list, and pointers to `models/stt`, `models/tts`, `models/llm`
- [X] T028 [P] [US2] Write `internal/skill/assets/references/workflow.md`: write, validate, read file, line, and column, fix, repeat, then run and listen; plus the path for an assistant that cannot run commands, with pointers to the CLI pages
- [X] T029 [P] [US2] Write `internal/skill/assets/references/variables.md`: variables, where their values come from, and secrets as environment variable names only, with pointers to `reference/variables` and `reference/secrets`
- [X] T030 [P] [US2] Write `internal/skill/assets/references/conversation.md`: greeting, interruption, inactivity, turn detection, with pointers to `models/turn-detection` and the agent reference
- [X] T031 [P] [US2] Write `internal/skill/assets/references/examples.md`: the map from a need to the example package that shows it, with a pointer per row into `examples/`

### Checks

- [X] T032 [P] [US2] Write the vendor agreement test in `internal/skill/agreement_test.go`: the vendors `models.md` names per role per target equal what `internal/target/catalog_*.go` holds, SLNG first
- [X] T033 [P] [US2] Write the provider agreement test in `internal/skill/agreement_test.go`: the target providers the bundle names equal `ir.Provider`, and every support claim says whether it means validation or generation
- [X] T034 [P] [US2] Write the command agreement test in `internal/skill/agreement_test.go`: every command and flag the bundle names exists in the cobra tree, reusing the capture pattern from `internal/cli/help_capture_test.go`
- [ ] T035 [US2] Run quickstart steps 5a, 5b, 5c, and 5f by hand against a real assistant and record what happened
- [ ] T036 [US2] Confirm SC-002 by running ten one-paragraph briefs and counting how many validate clean on the first attempt; the bar is eight

**Checkpoint**: an assistant can build and run a working single agent.

---

## Phase 5: User Story 3 - Prompts that sound right out loud (Priority: P2)

**Goal**: every instruction file the assistant writes follows voice prompting
rules rather than chat habits.

**Independent Test**: ask for instructions for a given role and check the result
against the prompt structure and the voice formatting rules; then hand it a
chat-shaped prompt and ask it to make it voice-ready.

- [X] T037 [US3] Write `internal/skill/assets/references/prompting.md`: prompt structure, output rules for spoken text, tool-use rules, goals, guardrails, and speech realism, adapted from the `voice-agent-prompting` source into Unmute's vocabulary
- [X] T038 [US3] Extend `prompting.md` with the per-surface section: what changes for an agent's instructions, a delegated task's, a group step's, a greeting, and a tool description, plus how to convert a chat-shaped prompt and report what changed
- [ ] T039 [US3] Run quickstart step 5e by hand: read an instructions file the assistant wrote and confirm no markdown formatting, no bullet lists, no raw URLs, no unspoken digits

**Checkpoint**: agents built through the skill sound right, not just compile.

---

## Phase 6: User Story 4 - Tools of every kind (Priority: P2)

**Goal**: the assistant picks the right tool kind for a plain-English ask and
writes the file correctly.

**Independent Test**: make four requests, one per kind, and confirm each
produces a valid tool file wired to the right agents that validates on the
chosen target.

- [X] T040 [US4] Write `internal/skill/assets/references/tools.md`: the four kinds with their file shape, what is legal and what is not, and which targets accept each, with pointers to `build/tools/webhook`, `build/tools/python`, `build/tools/mcp`, `build/tools/prebuilt`
- [X] T041 [US4] Extend `tools.md` with webhook authentication schemes, what is out of scope in this version, writing the Python handler alongside its tool file, hidden values the model cannot see or overwrite, and which agents and tasks see a tool
- [X] T042 [US4] Write the execution-kind agreement test in `internal/skill/agreement_test.go`: the tool kinds `tools.md` names equal the execution blocks on the `Tool` struct in `internal/spec`, no more and no fewer
- [ ] T043 [US4] Ask an assistant for one tool of each kind and confirm all four validate on the chosen target

**Checkpoint**: an agent built through the skill can do things, not just talk.

---

## Phase 7: User Story 5 - Climbing the complexity ladder (Priority: P3)

**Goal**: the assistant picks the right shape for the brief and decides context
sharing on purpose rather than by default.

**Independent Test**: three briefs, one wanting a handoff, one wanting a
delegated job with a typed answer, one wanting an ordered sequence; confirm the
right shape each time and a stated context decision each time.

- [X] T044 [US5] Write `internal/skill/assets/references/orchestration.md`: the four rungs and the rule for choosing between them, with pointers to `build/orchestration/overview`, `handoffs`, `tasks`, `task-groups`, `choosing-a-structure`
- [X] T045 [US5] Extend `orchestration.md` with every context decision: how much history crosses a handoff, which variables cross, what a delegated job sees at the start, whether group steps share context or each start clean, and what returns to the caller when a group ends
- [X] T046 [US5] Extend `orchestration.md` with machine-checked guards on a handoff, typed results, mapping result fields into variables, and the list of places a target refuses a shape or an option so the assistant raises it before writing files
- [ ] T047 [US5] Give an assistant the three briefs and confirm the shape and the stated context decision in each case, with zero silent defaults

**Checkpoint**: the part assistants get wrong most often is covered.

---

## Phase 8: User Story 6 - Onto a phone line, then into production (Priority: P3)

**Goal**: the assistant knows the routes, what each supports, where Unmute stops
and the carrier starts, and what the operator still owns.

**Independent Test**: three briefs, one inbound phone agent on a named carrier,
one adding a handover to a person, one asking how to go live; confirm a
supported route, its limits named before writing, and a clean split between what
Unmute generates and what the operator does by hand.

- [X] T048 [US6] Write `internal/skill/assets/references/telephony.md`: choosing a route from orchestrator, transport, and carrier together rather than from a brand name, what each route can and cannot do, inbound and outbound, and testing over a real phone locally, with pointers to the telephony pages
- [X] T049 [US6] Extend `telephony.md` with the boundary: Unmute never buys numbers and never creates carrier applications or carrier trunks, and no route is presented as more proven than the compile report says
- [X] T050 [P] [US6] Write `internal/skill/assets/references/transfers.md`: cold and warm, which routes support which shape, what the caller experiences in each, and naming a destination without putting a number in the package, with pointers to the transfers pages
- [X] T051 [P] [US6] Write `internal/skill/assets/references/deploy.md`: what the generated project needs from the operator, what stays the operator's job, and why a local dev run is not a deployment, with pointers to the deployment pages
- [ ] T052 [US6] Give an assistant the three briefs and confirm a shipping route, its limits named before any file is written, and the operator split stated

**Checkpoint**: full coverage of the documented surface is reached.

---

## Phase 9: User Story 7 - The skill stays true (Priority: P3)

**Goal**: the skill is named in the contributor rules alongside the other places
a change lands, and a stale claim fails a test rather than reaching a reader.

**Independent Test**: change a contract fact the skill states, run the suite,
and confirm it goes red naming the skill file.

- [X] T053 [US7] Write the pointer test in `internal/skill/agreement_test.go`: every documentation pointer resolves to a real page under `docs-site/`, and every reference file except `prompting.md` carries at least one; the exemption is named in the test with its reason
- [X] T054 [P] [US7] Write the entry budget and orphan tests in `internal/skill/agreement_test.go`: `SKILL.md` is under 500 lines, every file under `references/` is reachable from it, and every reference it names exists
- [X] T055 [P] [US7] Write the frontmatter test in `internal/skill/agreement_test.go`: both `SKILL.md` files carry exactly `name`, `description`, and `metadata`, and the pointer's `name` and `description` match the canonical one's
- [X] T056 [P] [US7] Write the no-secrets test in `internal/skill/agreement_test.go`: no file in the bundle contains anything shaped like a credential, environment variable names only
- [X] T057 [US7] Write `internal/skill/assets/pointer/SKILL.md`, the Claude Code pointer body: the same `name` and `description`, and a body saying where the canonical bundle is and to read it first
- [X] T058 [US7] Amend the "Three places document a change, not one" section of `CLAUDE.md`: it is five places now, the emitted README template, the example's own README, the page in `docs/`, the page in `docs-site/`, and the skill. Rename the heading to match
- [X] T059 [US7] Add `internal/skill/` to the pipeline or packages table in `docs/REPO_MAP.md` so the next reader can find the bundle
- [X] T060 [US7] Run quickstart step 6 and prove both drift checks bite: break an execution block and confirm red naming `references/tools.md`, then rename a docs page and confirm red naming the reference whose pointer broke

**Checkpoint**: the skill cannot go stale quietly.

---

## Phase 10: Polish and cross-cutting

- [X] T061 [P] Read every reference file against the nine rules in `docs-site/README.md`, especially plain language with no dashes as punctuation, only two targets presented as targets, and no route over-claimed
- [X] T062 [P] Confirm every Python snippet in the bundle passes `ty` and `ruff`, per the repository rule on hand-written Python
- [X] T063 Run the full gate: `make fmt`, `make lint`, `make build`, `make test`
- [ ] T064 Run `make smoke` locally and confirm it still skips cleanly without `uv` and passes with it
- [X] T065 [P] Run `mint validate` and `mint broken-links` in `docs-site/` after the new CLI page lands
- [X] T066 Confirm the page count invariant: the number of `.mdx` files under `docs-site/` equals the number of page entries in `docs-site/docs.json`
- [ ] T067 Walk the whole of `quickstart.md` end to end on a clean machine
- [ ] T068 Confirm each success criterion in the spec has been measured, not assumed, and record the numbers for SC-002, SC-004, SC-005, and SC-007

---

## Dependencies and execution order

### Phase dependencies

- **Phase 1 Setup**: T001 first. The constitution amendment authorizes the command, so it leads rather than trails.
- **Phase 2 Foundational**: depends on Phase 1. Blocks every user story.
- **Phase 3 US1**: depends on Phase 2. Blocks every content story, because content that cannot be installed cannot be tested.
- **Phases 4 to 9**: all depend on Phase 3. After that they are independent of each other and can run in parallel.
- **Phase 10**: depends on every story you intend to ship.

### Story dependencies

- **US1** is the only story with a hard dependency chain in front of it, and everything else waits on it.
- **US2** owns `SKILL.md`, so it must land before any other content story touches the routing table. Later stories add their own line to it, which is the one shared file across phases 5 to 8. Coordinate that edit or take the phases in order.
- **US3, US4, US5, US6** are independent of each other. Different reference files, different tests.
- **US7** is best done last, because its pointer, orphan, and budget tests assert against content that has to exist first.

### Parallel opportunities

- T003 and T004 in Setup.
- T012 is a single file and can be written alongside T006 to T011 by the same person, test first if you prefer.
- In US2, T026 to T031 are six different files and can all be written in parallel, and T032 to T034 are three independent tests.
- In US6, T050 and T051 are independent of T048 and T049.
- In US7, T054 to T056 are independent tests.
- Phases 5, 6, 7, and 8 can be staffed to four people at once once Phase 3 is done.

---

## Parallel example: User Story 2

```bash
# Six reference files, six people or six sittings, no shared file:
Task: "Write internal/skill/assets/references/package.md"
Task: "Write internal/skill/assets/references/models.md"
Task: "Write internal/skill/assets/references/workflow.md"
Task: "Write internal/skill/assets/references/variables.md"
Task: "Write internal/skill/assets/references/conversation.md"
Task: "Write internal/skill/assets/references/examples.md"
```

---

## Implementation strategy

### MVP: Phases 1 to 4

Setup, Foundational, US1, and US2. That is a skill you can install that produces
a working single agent with the right models. It is demonstrable, it is the
thing the feature is actually for, and everything after it is coverage.

Stop there and validate before going further. If US2's ten-brief measurement
(T036) comes in under the bar, the fix is `SKILL.md` and the six references, not
more references.

### Incremental delivery

1. Phases 1 to 3: the skill installs. Nothing to say yet, but the pipe works.
2. Phase 4: a working single agent. **Ship and validate here.**
3. Phase 5: it sounds right.
4. Phase 6: it can do things.
5. Phase 7: it can be more than one agent.
6. Phase 8: it can answer a phone and go live.
7. Phase 9: it cannot go stale.

Each step is worth having on its own, and none of them breaks the one before.

### Notes

- Commit after each task or logical group.
- `SKILL.md` is the one file several phases touch. Every content phase adds its
  routing line. Keep those edits small and take the phases in order if two
  people are working at once.
- The three help-capture repairs in US1 are not cleanup. They go red as soon as
  the command is registered, and the branch is broken until they land.
- Where a task says confirm or run by hand, that is because no test can hold it.
  Do not mark it done from reasoning.
