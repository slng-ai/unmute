---

description: "Task list for 016-upgrade-target-runtimes"
---

# Tasks: Upgrade Target Runtimes and Make Version Support Scalable

**Input**: Design documents from `specs/016-upgrade-target-runtimes/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/)

**Tests**: This repository's constitution makes tests mandatory, not optional: every rule needs a gate, non-trivial logic leaves one runnable check behind, and goldens pin emitted bytes. Test tasks below are therefore required, not a TDD add-on.

**Organization**: Grouped by user story so each ships and verifies on its own.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1, US2, US3 from [spec.md](spec.md)
- Paths are repository-relative

## Path Conventions

Single Go module at the repository root. Source in `internal/`, emitted Python in `internal/generate/templates/`, goldens in `internal/*/testdata/golden/`.

---

## Phase 1: Setup

**Purpose**: Establish a trustworthy baseline before anything moves.

- [X] T001 Confirm the baseline gate is green with `make fmt && make lint && make build && make test`, so any later red is attributable to this feature
- [X] T002 [P] Confirm the two ceilings resolve and import in a throwaway venv (`pipecat-ai==1.7.0`, `livekit-agents==1.6.10`), recording the observed versions in [research.md](research.md) if they differ from R1

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The one recorded home and the comparison logic. Every user story reads this.

**⚠️ CRITICAL**: No user story work starts until this phase is complete.

- [X] T003 Replace `driverVersions` with the support-window record (floor, ceiling, verified date) in `internal/target/driver.go`, set to pipecat 1.5.0-1.7.0 and livekit 1.5.0-1.6.10 verified 2026-08-16, per [data-model.md](data-model.md) section 1
- [X] T004 Extend `CheckVersion` in `internal/target/driver.go` to require all three version parts, compare full triples with the existing `ParseVersion` + `slices.Compare`, and enforce the ceiling, with the messages in [contracts/version-support.md](contracts/version-support.md) section 2
- [X] T005 [P] Give the silero floor one home: `internal/generate/livekit_v1_build.go` reads it from `internal/target/driver.go` instead of the bare `">=1.6.1"` literal
- [X] T006 [P] Create `internal/target/versions_test.go` with window invariants (table non-empty, `floor <= ceiling` per provider, non-empty verified date) and a `CheckVersion` table covering below-floor, above-ceiling, partial version, non-numeric, and in-range cases
- [X] T007 Update the three existing version test tables for the new messages: `TestCheckLiveKitVersion` in `internal/generate/livekit_v1_test.go`, `TestCheckPipecatVersion` in `internal/generate/pipecat_v1_test.go`, and `TestValueChecksFailAtValidate` in `internal/ir/validate_test.go`

**Checkpoint**: The window exists, is enforced with patch precision, and has its own tests. No emitted output has changed yet.

---

## Phase 3: User Story 1 - The declared version is the installed version (Priority: P1) 🎯 MVP

**Goal**: A code target's `version:` becomes the exact install pin on both frameworks, and the feature floors that used to rewrite it silently become gated errors.

**Independent Test**: Compile one package per target, read `build/<target>/pyproject.toml`, and confirm the framework line is `==` the declared version. Then declare 1.5.2 on a warm-transfer package and confirm validation fails naming the feature and its floor.

- [X] T008 [US1] Delete the constraint ladder in `livekitDeps()` in `internal/generate/livekit_v1_build.go` and emit `livekit-agents[extras]==<declared version>`, keeping the `extras["mcp"]` line because the extra is a separate fact from the version
- [X] T009 [US1] Delete the now-unused `livekitVersionMajor` / `livekitVersionMinMinor` / `livekitWarmVerifiedMinor` / `livekitMCPVerifiedMinor` constants in `internal/generate/livekit_v1.go` and rewrite their comment block to point at the recorded home
- [X] T010 [US1] Give the feature-use predicate (does this package have a warm transfer, does it need MCP) one home both `internal/ir/validate.go` and `internal/generate/livekit_v1_build.go` read, or an agreement test that fails when the two definitions diverge
- [X] T011 [P] [US1] Add the feature floor table (warm transfer → 1.6.0, MCP tool source → 1.6.0, both LiveKit) to `internal/target`, per [data-model.md](data-model.md) section 2
- [X] T012 [US1] Gate the declared version against every used feature's floor in `internal/ir/validate.go`, with the messages in [contracts/version-support.md](contracts/version-support.md) section 2 (depends on T010, T011)
- [X] T013 [P] [US1] Add feature-floor gating tests to `internal/ir/validate_test.go`, including the regression case: a warm-transfer package declaring 1.5.2 must fail rather than compile
- [X] T014 [US1] Update the LiveKit emission assertions in `internal/generate/livekit_v1_test.go`: the warm-transfer test that pins `>=1.6,<1.7`, the MCP test that expects `>=1.6` (the `mcp` extra assertion stays), and the multi-vendor test that expects `>=1.5`
- [X] T015 [P] [US1] Bump the three fixtures in `internal/testdata/{remy,safe_core,daily_carrier}/targets.yaml` to the ceilings and correct the stale range comments they carry
- [X] T016 [P] [US1] Bump all eleven `examples/*/targets.yaml` to the ceilings (`simple-prompt`, `subagents`, `salon-support`, `multi-task`, `task-groups`, `outbound-reminder`, `mcp-example`, `livekit-human-transfer`, `pipecat-human-transfer-daily`, `pipecat-human-transfer-twilio`, `twilio-telephony-hello`), removing the undocumented 1.5.0/1.5.2 and 1.5.2/1.6.4 split
- [X] T017 [US1] Regenerate the affected goldens (`-update`, `-update-pipecat`, `-update-catalog`) and read the diff before committing, confirming every framework line moved to `==` and nothing else changed
- [X] T018 [US1] `make test` green

**Checkpoint**: The pin is real on both targets. A declared version can no longer be silently overridden, and an unmet feature floor fails loudly.

---

## Phase 4: User Story 2 - One recorded home, derived everywhere (Priority: P2)

**Goal**: The window seeds the scaffold, is visible from the tool, and no author-facing surface contradicts it.

**Independent Test**: Change the recorded ceiling and confirm validation, the scaffold default, and the reported range all follow with no other edit. Introduce a stale version string in a document and confirm the agreement test fails.

- [X] T019 [US2] Derive the `TargetVersion` defaults in `internal/scaffold/scaffold.go` from the recorded ceilings instead of the two literals, and regenerate `internal/scaffold/testdata/golden/init.txt`
- [X] T020 [US2] Add floor, ceiling, and verified date per code target to the compile report, per [contracts/version-support.md](contracts/version-support.md) section 4
- [X] T021 [US2] Add the supported-framework line to the version output in `internal/cli/root.go`, keeping the existing first line byte-stable against `specs/010-goreleaser-release-pipeline/contracts/version-output.md`
- [X] T022 [US2] Add the agreement sweep over author-facing surfaces (examples, `docs/`, `docs-site/`, `internal/skill/assets/`, `README.md`, the scaffold default), modelled on `TestOneModelIdEverywhere` in `internal/skill/agreement_test.go`: match version strings by shape, guard against a vacuous pass, and document the carve-out for goldens, `internal/testdata/`, and `specs/` history
- [X] T023 [P] [US2] Update the framework version statements in `docs/SCHEMA.md`, `docs/TRANSFERS.md`, `docs/TELEPHONY.md`, `docs/PROVIDER_CATALOG.md`, and `docs/ORCHESTRATOR_SHARED_CONFIGURATION.md`
- [X] T024 [P] [US2] Update the framework version statements across `docs-site/`, including the emitted dependency blocks quoted in `targets/livekit.mdx`, `targets/pipecat.mdx`, and `build/tools/mcp.mdx`
- [X] T025 [P] [US2] Update `internal/skill/assets/references/package.md`, `models.md`, and `telephony.md` so the skill teaches the ceilings
- [X] T026 [P] [US2] Update the version samples in `README.md`
- [X] T027 [US2] `make test` green, with the agreement sweep passing (depends on T022-T026)

**Checkpoint**: One home, and a test that fails when anything drifts from it.

---

## Phase 5: User Story 3 - Current run paths, proven by a human (Priority: P3)

**Goal**: No emitted or documented run mode is deprecated upstream, `--console` is gone, and a human has talked to every example at the ceilings.

**Independent Test**: Grep the tree for deprecated invocations and find none; run `unmute dev --console` and get an explaining error; run every example and hold a conversation.

### Console removal

- [X] T028 [US3] Remove `--console` from `internal/cli/dev.go`: the flag, the dispatch branch, `runDevConsole`, `consolePlan`, `execConsole`, and `requireInferenceCreds`, replacing the flag with the explaining error in [contracts/run-commands.md](contracts/run-commands.md) section 3
- [X] T029 [US3] Update the two Daily refusal messages in `internal/cli/dev.go` and the missing-Docker hint in `internal/cli/dev_web.go` so none offers `--console` as a fallback
- [X] T030 [US3] Remove `Artifact.LiveKitInference` in `internal/generate/artifact.go` and its producers in `internal/generate/livekit_v1.go` and `internal/generate/livekit_v1_build.go` if `runDevConsole` was its only consumer, keeping it if any other reader remains
- [X] T031 [P] [US3] Remove the emitted console scaffolding: the `console` optional-dependency group in `internal/generate/templates/pipecat_v1/pyproject.toml.tmpl`, `console_main()` and its `sys.argv` branch in `bot.py.tmpl`, and the console lines in `README.md.tmpl` and `env.example.tmpl`
- [X] T032 [P] [US3] Update the console tests: the removed-flag cases in `internal/cli/dev_test.go`, the `--console` assertions in `internal/cli/dev_web_runner_test.go`, the console-extra smoke in `internal/generate/pipecat_v1_smoke_test.go`, and the `console_main` truncation in `internal/generate/pipecat_carrier_telephony_test.go`

### LiveKit run-mode migration

- [X] T033 [US3] Change the dev compose command in `internal/generate/templates/livekit_v1/compose.dev.yaml.tmpl` to `python -m livekit.agents start agent.py --log-format colored`
- [X] T034 [US3] Change the `CMD` and its comment in `internal/generate/templates/livekit_v1/Dockerfile.tmpl` to the thin CLI form
- [X] T035 [US3] Change the connector command in `internal/generate/templates/livekit_v1/compose.telephony.connector.yaml.tmpl` and the matching `TelephonyProcess` command in `internal/target/telephony.go` together, keeping them byte-identical
- [X] T036 [US3] Remove the `if __name__ == "__main__": cli.run_app(server)` block and the now-unused `cli` import from `internal/generate/templates/livekit_v1/agent.py.tmpl`, leaving the module-level `server = AgentServer(...)` untouched because the thin CLI discovers it
- [X] T037 [US3] Replace the run instructions in `internal/generate/templates/livekit_v1/README.md.tmpl` with the single worker command
- [X] T038 [US3] Update the dev-compose command assertion in `internal/generate/compose_dev_test.go`, and add a check that no template emits `agent.py dev`, `agent.py console`, or `cli.run_app`
- [X] T039 [US3] Regenerate the affected goldens and run `make test` green

### Documents and amendments

- [X] T040 [US3] Add the dated, numbered amendment to `docs/SCHEMA.md` covering all four changes: the LiveKit pin becomes exact, feature floors become gated errors, versions must name three parts, and the emitted run command changed
- [X] T041 [US3] Amend `.specify/memory/constitution.md` from 2.1.0 to 3.0.0 with a prepended Sync Impact Report, removing `--console` from Principle V and stating what an author without Docker does instead
- [X] T042 [P] [US3] Update the console and run-mode prose in `docs/ARCHITECTURE.md`, `docs/TESTING.md`, `docs/HARNESS_TEST.md`, `docs/TELEPHONY.md`, `docs/TRANSFERS.md`, and `docs/SCHEMA.md`
- [X] T043 [P] [US3] Update `docs-site/`: delete `dev/console.mdx` and its `docs.json` nav entry, and update `dev/overview.mdx`, `dev/telephony.mdx`, `reference/cli/dev.mdx`, `reference/cli/overview.mdx`, `start/quickstart.mdx`, `start/installation.mdx`, `targets/livekit.mdx`, `targets/pipecat.mdx`, `targets/overview.mdx`, `models/llm.mdx`, and `transfers/pipecat-daily.mdx`
- [X] T044 [P] [US3] Update the run-mode table in `internal/skill/assets/references/workflow.md`
- [X] T045 [P] [US3] Update the `--console` invocations in the READMEs of `examples/livekit-human-transfer`, `examples/pipecat-human-transfer-twilio`, and `examples/pipecat-human-transfer-daily`
- [X] T046 [US3] Regenerate the CLI help golden `specs/008-mintlify-user-docs/help.txt` and confirm the docs-site help-quoting test passes

### Human verification (FR-012)

- [X] T047 [US3] Run `make smoke` at the ceilings and confirm both frameworks install the declared version rather than floating
- [X] T048 [US3] Create `specs/016-upgrade-target-runtimes/results.md` with the row-per-example table from [data-model.md](data-model.md) section 4
- [ ] T049 [P] [US3] Live call: `examples/simple-prompt` on both targets, conversation holds
- [ ] T050 [P] [US3] Live call: `examples/subagents`, agent handoff fires
- [ ] T051 [P] [US3] Live call: `examples/salon-support`, its task flow completes
- [ ] T052 [P] [US3] Live call: `examples/multi-task`, task switching works
- [ ] T053 [P] [US3] Live call: `examples/task-groups`, grouped tasks run in order
- [ ] T054 [P] [US3] Live call: `examples/outbound-reminder`, outbound call connects
- [ ] T055 [P] [US3] Live call: `examples/mcp-example`, an MCP tool call returns
- [ ] T056 [P] [US3] Live call: `examples/livekit-human-transfer`, warm transfer completes with the briefing (the case the exact pin most affects)
- [ ] T057 [P] [US3] Live call: `examples/pipecat-human-transfer-daily`, transfer completes
- [ ] T058 [P] [US3] Live call: `examples/pipecat-human-transfer-twilio`, transfer completes over the carrier
- [ ] T059 [P] [US3] Live call: `examples/twilio-telephony-hello`, inbound call answers on both targets
- [ ] T060 [US3] Record every result in `results.md` with versions, date, and person; a failing row blocks the release (depends on T049-T059)

**Checkpoint**: Nothing deprecated is emitted or documented, and every example is proven by a real conversation.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T061 Update the emitted `agent.py` verification comment that names livekit-agents 1.6.9 and its date, and the test asserting it, to the verified ceiling and the live-call date
- [ ] T062 Run the full gate in order: `make fmt`, `make lint`, `make build`, `make test`, `make smoke`
- [ ] T063 Walk [quickstart.md](quickstart.md) end to end as a reader who has not seen this work, and fix anything that does not match reality
- [ ] T064 Re-read the watch items in [research.md](research.md) R8 against what the live calls showed, and record the outcome (worker load threshold, container teardown time) so the next upgrade inherits the finding

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies
- **Foundational (Phase 2)**: needs Setup; blocks every user story
- **US1 (Phase 3)**: needs Foundational
- **US2 (Phase 4)**: needs Foundational, and T019-T021 read the window US1 already exercises; it is cleanest after US1
- **US3 (Phase 5)**: needs Foundational only. The run-path work is independent of the version work and could ship first if preferred; it sits last so the live calls verify the final state of everything
- **Polish (Phase 6)**: needs all three stories

### Within Each Story

- The one recorded home before anything derives from it
- Emission changes before golden regeneration, always
- Every code change before the live call that proves it

### Parallel Opportunities

- T005 and T006 during Foundational
- T011, T013, T015, T016 during US1
- T023, T024, T025, T026 during US2 (four separate documentation surfaces)
- T031 and T032 during console removal
- T042 through T045 during the documentation sweep
- T049 through T059: eleven live calls, one per example, each independently runnable by a human

## Parallel Example: User Story 2 documentation sweep

```bash
# Four documentation surfaces, no shared files:
Task: "Update framework versions in docs/"
Task: "Update framework versions in docs-site/"
Task: "Update framework versions in internal/skill/assets/references/"
Task: "Update version samples in README.md"
```

## Implementation Strategy

### MVP (User Story 1 only)

1. Phase 1 Setup
2. Phase 2 Foundational
3. Phase 3 US1
4. **Stop and validate**: the declared version is what installs, on both targets, and an unmet feature floor fails loudly
5. This alone fixes the silent-override defect and is worth shipping

### Incremental delivery

1. Foundational → the window exists and is enforced
2. US1 → the pin is real (MVP)
3. US2 → one home, nothing contradicts it, drift has a gate
4. US3 → run paths current, amendments landed, every example proven on a live call

## Notes

- Regenerate goldens only after an intentional output change, then read the diff before committing
- Commit after each task or logical group
- Three surfaces must move together whenever emitted behavior changes: the emitted README template, the example's own README, and the matching pages in `docs/`, `docs-site/`, and the skill
- The live calls in Phase 5 are the release gate, not a formality: a failing row blocks the release
