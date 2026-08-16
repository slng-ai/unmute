---
description: "Implementation tasks for spoken agent handoffs"
---

# Tasks: Spoken Agent Handoffs

**Input**: Design documents from `/specs/014-spoken-agent-handoffs/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/agent-transfer.md`, `quickstart.md`

**Tests**: Each behavior change starts with a focused failing test. The live
harness is the final contract test because offline gates cannot hear a call.

## Phase 1: Setup (Locked Contract)

**Purpose**: Make the authoring promise authoritative before code follows it.

- [X] T001 Append amendment N44 and add the optional `announce` row and semantics in `docs/SCHEMA.md`

---

## Phase 2: Foundational (Blocking Compiler Path)

**Purpose**: Carry and gate the new field through the shared compiler path.

- [X] T002 [P] Add failing build/validation tests for preserved, omitted, blank, templated, and illegal-kind `announce` values in `internal/ir/build_test.go` and `internal/ir/validate_test.go`
- [X] T003 Add `announce` to the strict spec, resolved IR, build validation, and illegal-field gate in `internal/spec/package.go`, `internal/ir/compiler.go`, and `internal/ir/build.go`
- [X] T004 Add `FieldTransferAnnounce`, target support rules, validation use, and emitted-field agreement entries in `internal/target/table.go`, `internal/ir/validate.go`, `internal/generate/livekit_v1.go`, and `internal/generate/pipecat_v1.go`
- [X] T005 [P] Add failing scaffold and maintain-mode round-trip tests for `announce` in `internal/scaffold/scaffold_test.go` and `internal/tui/tui_test.go`
- [X] T006 Carry and edit `announce` through `internal/scaffold/scaffold.go`, `internal/scaffold/templates/agent.yaml.tmpl`, `internal/tui/maintain.go`, and `internal/tui/tui.go`

**Checkpoint**: The field strictly decodes, validates, round-trips, and cannot be silently accepted by an emitter.

---

## Phase 3: User Story 1 - Hear the Handoff Before It Happens (Priority: P1) 🎯 MVP

**Goal**: A configured handoff produces one natural cue that finishes before the receiving agent activates on both shipped targets.

**Independent Test**: Compile a two-agent fixture with `announce`; in each generated transfer method, the requirement guard precedes the cue and the cue precedes the handoff primitive.

### Tests for User Story 1

- [X] T007 [P] [US1] Add a failing LiveKit generator test for announcement presence, omission, and guard-cue-return ordering in `internal/generate/livekit_v1_test.go`
- [X] T008 [P] [US1] Add failing Pipecat generator and real-SDK smoke tests for exact source speech, stopped-speaking playout gating, target context, omission, and guard-cue-activation ordering in `internal/generate/pipecat_v1_test.go` and `internal/generate/pipecat_v1_smoke_test.go`

### Implementation for User Story 1

- [X] T009 [P] [US1] Carry `Announce` through the LiveKit transfer view and await the native outgoing reply before returning the target in `internal/generate/livekit_v1.go`, `internal/generate/livekit_v1_build.go`, and `internal/generate/templates/livekit_v1/agent.py.tmpl`
- [X] T010 [P] [US1] Carry `Announce` through the Pipecat transfer view, speak it without an LLM turn, wait for source playout to stop, then use native `activate_worker` with the existing target context in `internal/generate/pipecat_v1.go`, `internal/generate/pipecat_v1_build.go`, and `internal/generate/templates/pipecat_v1/bot.py.tmpl`
- [X] T011 [US1] Add distinct exact announcement sentences to both directions and remove silent-transfer prompt conflicts in `examples/subagents/agent.yaml`, `examples/subagents/instructions.md`, and `examples/subagents/agents/appointment-manager.md`

**Checkpoint**: Both generated targets have the same gate → spoken cue → activation order; omission emits no cue.

---

## Phase 4: User Story 2 - Continue Naturally After a Handoff (Priority: P2)

**Goal**: A round trip preserves the latest intent and known values, never replays the entry greeting, and never hides a tool-call failure.

**Independent Test**: Compile and run the round-trip fixture; the opening greeting is emitted only for the initial entry, and an unexpected Pipecat tool argument becomes a terminal retryable result rather than an `ErrorFrame`/`IN_PROGRESS` gap.

### Tests for User Story 2

- [X] T012 [P] [US2] Add a failing LiveKit generator test covering initial entry, return handoff, and `history: reset` without greeting replay in `internal/generate/livekit_v1_test.go`
- [X] T013 [P] [US2] Add failing Pipecat generator and real-SDK smoke tests for preserved tool schema plus terminal handling of unexpected kwargs in `internal/generate/pipecat_v1_test.go` and `internal/generate/pipecat_v1_smoke_test.go`

### Implementation for User Story 2

- [X] T014 [P] [US2] Mark only startup-created LiveKit entry agents as initial and continue normally on later entry in `internal/generate/templates/livekit_v1/agent.py.tmpl` and its generator view/build code if needed
- [X] T015 [P] [US2] Add one signature-preserving generated Pipecat direct-tool guard and apply it to all worker tools in `internal/generate/templates/pipecat_v1/bot.py.tmpl` and import/build logic in `internal/generate/pipecat_v1_build.go`
- [X] T016 [US2] Split the appointment manager's compound question and remove the misleading availability description that encouraged an undeclared argument in `examples/subagents/agents/appointment-manager.md` and `examples/subagents/tools/check_availability.yaml`

**Checkpoint**: Return handoffs continue instead of greeting, and malformed tool arguments produce a visible corrective result with no false success.

---

## Phase 5: User Story 3 - Keep Existing Silent Handoffs (Priority: P3)

**Goal**: Packages that omit `announce` remain valid and gain no new handoff speech.

**Independent Test**: Compile an existing handoff fixture with no field and assert neither target emits an outgoing announcement argument or call.

### Tests and Implementation for User Story 3

- [X] T017 [US3] Extend omission/backward-compatibility coverage across load, build, LiveKit, and Pipecat tests in `internal/ir/build_test.go`, `internal/generate/livekit_v1_test.go`, and `internal/generate/pipecat_v1_test.go`

**Checkpoint**: Silent handoffs stay silent without package edits.

---

## Phase 6: Documentation, Goldens, and Live Proof

**Purpose**: Update every user surface, run every gate, then prove the spoken experience.

- [X] T018 [P] Update the emitted handoff runbooks in `internal/generate/templates/livekit_v1/README.md.tmpl` and `internal/generate/templates/pipecat_v1/README.md.tmpl`
- [X] T019 [P] Update the source example and public documentation in `examples/subagents/README.md`, `docs-site/build/orchestration/handoffs.mdx`, and `docs-site/reference/agent-yaml.mdx`
- [X] T020 [P] Update coding-agent guidance in `internal/skill/assets/references/orchestration.md` and the exact round-trip script in `docs/HARNESS_TEST.md`
- [X] T021 Regenerate affected goldens and prove the new tests fail without their fixes, then pass with them in `internal/generate/testdata/golden/`
- [X] T022 Run `make fmt`, `make lint`, `make build`, `make test`, and `make smoke` from the repository root
- [X] T023 Compile and inspect `examples/subagents/build/livekit/agent.py` and `examples/subagents/build/pipecat/bot.py` against `quickstart.md`
- [ ] T024 Run five human round-trip calls per target and record exact tool order, one greeting, announcement order, IDs, and zero errors in `docs/HARNESS_TEST.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- Phase 1 fixes the contract and blocks implementation.
- Phase 2 carries the field and blocks all user stories.
- US1 is the MVP and blocks the live proof.
- US2 depends on the existing transfer lowering but is otherwise independent of US1's authoring cue.
- US3 uses the same generator seams and follows US1.
- Documentation and live proof follow all three stories.

### Parallel Opportunities

- T002 and T005 touch separate test surfaces.
- T007 and T008 are independent target tests.
- T009 and T010 are independent target implementations.
- T012 and T013 are independent target regressions.
- T014 and T015 are independent target fixes.
- T018, T019, and T020 are separate documentation surfaces.

## Parallel Example: User Story 1

```text
Task: T007 LiveKit failing generator test
Task: T008 Pipecat failing generator test

Then:

Task: T009 LiveKit native lowering
Task: T010 Pipecat native lowering
```

## Implementation Strategy

1. Lock N44 and make the shared compiler tests red.
2. Carry the field once through spec, IR, capability, scaffold, and TUI.
3. Deliver US1 on both native handoff APIs as the MVP.
4. Fix LiveKit continuity and Pipecat tool failure handling from the same live run.
5. Prove omission, update all five documentation surfaces, run gates, then repeat the human call.

## Format Validation

All tasks use a checkbox, sequential ID, optional parallel marker, required user-story label in story phases, a concrete action, and exact file paths.
