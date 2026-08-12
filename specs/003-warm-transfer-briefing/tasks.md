---

description: "Task list for the warm transfer briefing feature"
---

# Tasks: Brief the manager, then hand the call over

**Input**: Design documents from `specs/003-warm-transfer-briefing/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: Test tasks are included. They are not optional here. The end-to-end behaviour is a live phone call that cannot run in CI, so FR-019 requires an offline test for the shape of every requirement a live call would otherwise be the only witness to.

**Organization**: grouped by user story. **The phases are in implementation order, not priority order.** User Story 2 is built first, because it is the smallest change and it is what makes the other two provable: three live calls have already been spent guessing at behaviour that produced no log lines. Each phase still says its priority, and each story is still independently testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel, meaning a different file with no dependency on an unfinished task
- **[Story]**: US1 briefing, US2 observability, US3 the exit when the manager does not decide
- Paths are repository-relative from `/Users/nicoferdi/Documents/GitHub/unmute_cli`

## The one thing already done

[Research R1](./research.md#r1-the-version-gap) closed on 2026-08-12 against the real
`livekit-agents` 1.6.9 inside `unmute-lk-fixed:latest`. It found that the instructions hook
class is **`WorkflowInstructions`**, renamed from `InstructionParts` with no alias, so the
old name must appear in no template and no emitted file. Every other platform fact is
byte-identical to the earlier reading. No task below re-opens that.

---

## Phase 1: Setup

**Purpose**: know the starting state, so that every later diff is attributable.

- [ ] T001 Confirm the baseline gate is green before any edit: `make fmt`, `make lint`, `make build`, `make test`. Record the result. `make smoke` is not the gate: it is environmentally broken on this machine for the pre-existing CPython 3.14 wheel reason, confirmed 2026-07-19 and again 2026-08-12.
- [ ] T002 [P] List every golden that contains a warm or cold transfer, so the later diff review is against a known set rather than whatever moved: `grep -l 'WarmTransferTask\|transfer_sip_participant' internal/generate/testdata/golden/*.txt`. Write the list into this file under Notes.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the two emitted import changes both stories need. They live in the same region of the same template, so they are sequential and they block both story phases.

**⚠️ CRITICAL**: no story work begins until this phase is complete and T005 passes.

- [ ] T003 Add a conditional `import time` to the emitted standard-library import block in `internal/generate/templates/livekit_v1/agent.py.tmpl`, gated on the package having a transfer of either kind (`or .HasWarmTransfer .HasColdTransfer`). It sorts after `os`, which is where `ruff` will want it.
- [ ] T004 Add `WorkflowInstructions` to the existing `from livekit.agents.beta.workflows import ...` line in `internal/generate/templates/livekit_v1/agent.py.tmpl`, gated on `.HasWarmTransfer`, alphabetically before `WarmTransferTask`.
- [ ] T005 In `internal/generate/livekit_v1_test.go`, assert the three import outcomes: a warm package imports both `time` and `WorkflowInstructions`; a cold-only package imports `time` and **not** `WorkflowInstructions`; and the retired name `InstructionParts` appears in **no** emitted artifact and in **no** file under `internal/generate/templates/`. The last one is the assertion that would have caught R1's finding on its own. It walks the working tree on purpose: it asserts what a template *contains*, not what is committed, so the constitution's git-not-working-tree rule for hygiene checks does not apply. Say that in a comment so the next reader does not have to work it out.

**Checkpoint**: emitted projects still compile and import, and the retired name cannot come back.

---

## Phase 3: User Story 2 - A live transfer can be read from the logs (Priority: P2, built first)

**Goal**: after a transfer, `lk agent logs` alone says which phase it reached, how much conversation the briefing received, and how it ended.

**Independent Test**: force each outcome once (accepted, no answer, declined) and read the log for each without watching the call. Delivers value on its own even if User Story 1 is never built, because it turns the current guesswork into evidence.

### Implementation

- [ ] T006 [US2] In the warm branch of `internal/generate/templates/livekit_v1/agent.py.tmpl`, read the conversation into a `briefing_ctx` local **once**, emit `logger.info("warm transfer dialling out: handing over %d conversation messages", len(briefing_ctx.items))` immediately before the dial, and pass that same local as `chat_ctx=`. One read, so the number logged and the number handed over cannot differ (contract C4, FR-003, FR-006).
- [ ] T007 [US2] In the warm branch of `internal/generate/templates/livekit_v1/agent.py.tmpl`, start `started = time.monotonic()` before the dial and add whole elapsed seconds to both outcome lines. Add the missing unavailable line on the `return_to_caller` branch, which logs nothing today, so both failure branches are visible and not just the hangup one (FR-007).
- [ ] T008 [US2] In the cold branch of `internal/generate/templates/livekit_v1/agent.py.tmpl`, add `logger.info("cold transfer referring the caller out")` before the request, start the same monotonic clock, and add elapsed seconds to the completed and failed lines. Behaviour must not change, only logging (FR-009, contract C11).

### Tests

- [ ] T009 [P] [US2] In `internal/generate/livekit_v1_test.go`, assert the warm log contract from [contracts/transfer-log.md](./contracts/transfer-log.md): the fired line, the dialling line with the message count, and both outcome lines with elapsed seconds. Assert the `chat_ctx=` argument and the count both read the same local.
- [ ] T010 [P] [US2] In `internal/generate/livekit_v1_test.go`, assert the cold log contract: the fired line, the referring line, and both outcome lines with elapsed seconds.
- [ ] T011 [US2] In `internal/generate/livekit_v1_test.go`, add one test that walks every `logger.` call in an emitted `agent.py` and asserts none of them carries a destination or a credential: no `os.environ` read, no `_refer_uri`, no `_sip_number`, and no literal that looks like an E.164 number. This is FR-008 and L5 enforced structurally rather than by review.
- [ ] T012 [US2] Regenerate the goldens listed in T002 with the package's own update flag, then **read the diff** before committing. `livekit_v1_sip-inbound-trunk.json` and `livekit_v1_sip-dispatch-rule.json` must not move at all.

**Checkpoint**: every transfer outcome is legible from the log alone. This is the point at which User Story 1 becomes diagnosable, and it is worth deploying on its own before continuing.

---

## Phase 4: User Story 1 - The manager hears why they were called (Priority: P1) 🎯 the feature's value

**Goal**: a supervisor's first agent turn names who is on hold, what they want, what was already offered, and one answerable question. Never a greeting.

**Independent Test**: call the agent, give a name, a stylist and a complaint, ask for a manager, answer the supervisor's phone, say hello, and listen to the first sentence.

### Implementation

- [ ] T013 [US1] Add the `_BRIEFING_PERSONA` module-level constant to `internal/generate/templates/livekit_v1/agent.py.tmpl`, beside the other transfer helpers, gated on `.HasWarmTransfer`. It must say all four things in [contracts/emitted-briefing.md](./contracts/emitted-briefing.md) section 2: open with the handover and never greet (P1), tell the colleague the caller goes through as soon as they are ready (P2), what to say when the transcript is thin (P3), and decline when the colleague cannot take it or goes quiet (P4). Above it, a comment naming what it replaces, why each part is there, and that it was verified against livekit-agents 1.6.9 on 2026-08-12 (contract C7).
- [ ] T014 [US1] In the warm dial site of `internal/generate/templates/livekit_v1/agent.py.tmpl`, replace `extra_instructions=...` with `instructions=WorkflowInstructions(persona=_BRIEFING_PERSONA, extra=<the authored briefing>)`. Omit the `extra` argument entirely when the package declares no briefing: an empty string deletes the platform's own section instead of keeping its default (contract C3).

### Tests

- [ ] T015 [P] [US1] In `internal/generate/livekit_v1_test.go`, assert contract C1 through C6: `instructions=WorkflowInstructions(` present; `extra_instructions` absent from every emitted file; the persona passed by constant reference and never inlined; the `extra` argument absent for a warm transfer with no authored briefing and carrying the authored text verbatim when there is one; the constant emitted exactly once for a package with two warm transfers.
- [ ] T016 [P] [US1] In `internal/generate/livekit_v1_test.go`, pin one distinctive phrase from each of persona parts P1, P2 and P3, so a later edit that drops one of them fails a test rather than a live call. P4 belongs to User Story 3 and is pinned in T019.
- [ ] T017 [US1] Reuse the warm-only fixture already in `internal/generate/livekit_inline_trunk_test.go` to assert a warm-only package gets the persona and the hook, and extend the cold-only assertion there so a cold-only package gets neither (contract C6).
- [ ] T018 [US1] Compile `examples/human-transfer` and verify the emitted `agent.py` against the real 1.6.9 package: `ruff check` for style, then inside `unmute-lk-fixed:latest` confirm it imports and that the emitted `WorkflowInstructions(...)` call resolves with the persona substituted. This is the only offline check that catches a rename like the one R1 found.
- [ ] T019 [US1] Regenerate the goldens from T002 under `internal/generate/testdata/golden/` and read the diff. The persona text lands in them, so this diff is large and must be read as prose, not skimmed.

**Checkpoint**: the briefing is emitted and provable offline. What remains for it is one live call, and T012's log lines tell you which of R6's three hypotheses was right if it still fails.

---

## Phase 5: User Story 3 - The caller is never left on hold without end (Priority: P3)

**Goal**: a caller comes back to the agent when the manager does not take the call, whatever the manager does or does not say.

**Independent Test**: answer the supervisor's phone, stay on the line, never say yes or no, and time how long the caller hears hold music.

**Read first**: this story is **not independently deliverable**. Its only emitted change is one paragraph of the persona that T013 introduces in Phase 4, so it cannot be built first. It is independently testable, which is what accepting it needs. And it ships as a **mitigation, not a bound**. [Research R5](./research.md#r5-why-there-is-no-hard-bound) shows why: the platform has no post-answer timeout, the awaited result is shielded so a timeout would leave a live consultation with the caller still muted and the music still playing, and the only thing that stops both is private. The emitted code therefore uses the platform's own decline tool as the exit. The plan's Complexity Tracking holds the two ways to get a real bound, and neither is taken without the user asking for it.

### Implementation

- [ ] T020 [US3] Confirm persona part P4 from T013 covers all three cases the story needs: the colleague says no, the colleague goes quiet, and the conversation moves on without an answer. If T013 shortened it, restore all three in `internal/generate/templates/livekit_v1/agent.py.tmpl`.

### Tests

- [ ] T021 [P] [US3] In `internal/generate/livekit_v1_test.go`, pin the decline instruction in the emitted persona, including the reason it exists, so nobody trims it as verbose without reading why.

### Documents

- [ ] T022 [US3] In `docs/TRANSFERS.md`, state the limit where an operator will meet it: after the manager answers, nothing on the platform bounds the consultation, the emitted agent asks its briefing model to decline instead, and a caller can in principle hold until the manager hangs up. Cite the source and the date.
- [ ] T023 [P] [US3] Submit documentation feedback upstream asking for a post-answer bound, using the LiveKit docs feedback tool, since the flow's own documentation promises the caller comes back when a transfer does not happen and today that promise holds for ringing only. Record that it was sent, and where.

**Checkpoint**: the stuck case is less likely, visible when it happens, and written down where the next person will look.

---

## Phase 6: Documents and closing

- [ ] T024 [P] Add amendment **N34** to `docs/SCHEMA.md`, dated 2026-08-12, appended and never rewriting an earlier one: the emitter now owns the manager-facing persona for a warm transfer, the deprecated instruction parameter is gone, and the consultation has no hard bound with the reason and the source. Principle II requires a known exception to be a dated numbered amendment that states the cost, and the second half of this is that exception.
- [ ] T025 [P] Update the warm-transfer section of `docs/TRANSFERS.md` with what the manager now hears and the three log lines a transfer leaves behind. Coordinate with T022, which edits the same file: run them in sequence, not in parallel.
- [ ] T026 [P] Update `examples/human-transfer/README.md` with the expected first sentence on the supervisor's phone and the expected log lines for each outcome, so the example is self-checking.
- [ ] T027 [P] Update `internal/generate/templates/livekit_v1/README.md.tmpl` so the generated project itself says what the manager hears and how to read the transfer in the logs. It is the only document a deployed operator has.
- [ ] T028 Check whether `docs/user/learn/07-phone-calls.md` and `docs/user/targets/livekit.md` describe what the manager hears during a warm transfer. Update them only if they do. An unnamed "wherever" is how a page gets missed, so record the answer either way.
- [ ] T029 Run the full gate: `make fmt`, `make lint`, `make build`, `make test`. Then `git diff internal/generate/testdata/golden/` one last time and read it whole.
- [ ] T030 Run [quickstart.md](./quickstart.md) layer 2 live and record for each run the first sentence the supervisor heard, the message count, the outcome line and the duration. **Run A three consecutive times**, which is what SC-001 asks for, and **Run C three times**, which is what SC-005 asks for now that the exit is best-effort and its reliability is an unknown number. Runs B and D once each. A Run C that produces no outcome line is a recorded failure, not a retry: if the exit does not hold, bring the plan's two alternatives back to the user with the observation attached.

---

## Dependencies & Execution Order

### Phase dependencies

- **Phase 1 Setup**: no dependencies.
- **Phase 2 Foundational**: needs Phase 1. **Blocks both story phases**, because a story that emits `WorkflowInstructions` without importing it produces a project that does not start.
- **Phase 3 User Story 2**: needs Phase 2. Independent of User Story 1.
- **Phase 4 User Story 1**: needs Phase 2. Independent of User Story 2 in code, dependent on it in practice: without Phase 3's log lines, a failed briefing is undiagnosable, which is the situation this whole feature exists to end.
- **Phase 5 User Story 3**: needs T013 from Phase 4, because its only emitted change is one part of that persona.
- **Phase 6**: needs whichever stories are being shipped.

### Why User Story 2 comes before User Story 1

The spec ranks the briefing higher, and it is right to. The build order is the other way for one reason: on 2026-08-12 a warm transfer failed on a live call and produced no log line at all, so three separate hypotheses fit the same evidence and none could be ruled out without reading framework source. Phase 3 costs about fifteen lines and turns the next live call into a measurement. Building Phase 4 first would mean testing a prompt fix with the same blindfold on.

### Within each story

- Template edits before their tests, because the tests assert emitted text.
- All tests before the golden regeneration, so a golden never records a shape no test defends.
- Goldens read, never regenerated blind. This is a constitution gate, not a preference.

### Parallel opportunities

- T002 runs alongside T001.
- T009 and T010 are different assertions in the same test file: parallel in the sense of independent, sequential in practice because they touch one file.
- T015, T016 and T021 are independent of each other once T013 and T014 land.
- T024, T026 and T027 are three different files and genuinely parallel. T022 and T025 are the same file and must not be.

### The one sequencing trap

T022 (User Story 3) and T025 (Phase 6) both edit `docs/TRANSFERS.md`. Do T022 first, then T025 on top of it. Neither is marked `[P]` against the other for that reason.

---

## Implementation Strategy

### The smallest useful increment

Phase 1, Phase 2, Phase 3. That is the observability story on its own: about fifteen lines of emitted Python and three tests. Deploy it and make one live warm transfer. Whatever happens, the log now says which of [research R6](./research.md#r6-why-the-briefing-failed-on-the-live-call)'s three hypotheses was right, and the message count settles the most likely one on its own. This is worth doing before Phase 4 even though Phase 4 is the higher-value story.

### The feature as the author asked for it

Add Phase 4. That is the briefing: the persona constant and the supported instructions hook. Redeploy and repeat the live call. The expected outcome is a supervisor who is told who is on hold before they ask.

### Everything

Add Phase 5 and Phase 6. Phase 5 is one paragraph of persona, one test, one document and one piece of upstream feedback. Phase 6 is the amendment and the four documents, which is what stops the next person rediscovering all of this.

### If Run C fails

The mitigation is a prompt and prompts are probabilistic. A failed Run C is not a reason to hold the feature: the caller-facing behaviour is strictly better than today either way. It is a reason to bring the plan's two alternatives back to the user with a real observation attached, which is more than either of them has now.
- [ ] T031 [P] Assert the Pipecat driver did not move: run `go test ./internal/generate/ -run Pipecat` and confirm `internal/generate/testdata/golden/pipecat_v1.txt` is byte-identical to `HEAD`. FR-018 says the Pipecat driver and its Daily warm transfer MUST NOT change, and until this task existed nothing in the plan said how that would be known.
- [ ] T032 [P] Assert the authoring surface neither widened nor broke: compile a package written before this change with no edit, and confirm the derived authoring schema is byte-identical to `HEAD`. This is FR-016 and SC-009, which had no check of their own. It can run any time after Phase 2.


---

## Notes

- The retired name `InstructionParts` must appear nowhere in `internal/generate/templates/` or any emitted file. T005 is the test that keeps it out.
- No task adds an authoring field, a dependency, or a Go change outside `internal/generate`. If one starts to, the design was wrong and the plan should be revisited rather than the constraint.
- T002 writes its golden list here. Fill it in when the task runs.
