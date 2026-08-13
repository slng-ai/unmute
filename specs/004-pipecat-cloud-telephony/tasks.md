---

description: "Task list for Pipecat Cloud telephony on the Daily route"
---

# Tasks: Pipecat Cloud telephony on the Daily route

**Input**: Design documents from `specs/004-pipecat-cloud-telephony/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Included. The spec requests them explicitly in FR-021 through FR-026, FR-030, and FR-031, and the constitution requires a runnable check behind non-trivial logic.

**Organization**: Grouped by user story so each is independently implementable and testable.

**Revision**: Regenerated 2026-08-12 after `/speckit-analyze`. Six coverage gaps and one critical scope error were fixed. The changes: caller identity left User Story 5 (it was a new authoring field carrying the constitution's full compliance checklist, and three tasks did not cover it); the Daily transfer gained an idempotency guard it never had; and five requirements that had zero tasks now have them. Task IDs were renumbered rather than appended so the sequence still reads in execution order.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete work)
- **[Story]**: Which user story the task serves
- Every task names its file path

## Path Conventions

Go CLI, `internal/` not `pkg/`, per the constitution. Paths are repository-relative from the worktree root. Emitted Python lives only in `internal/generate/templates/` as `text/template` files and is never maintained by hand.

## Two facts that drive the task order

**One.** `internal/ir/build.go:804` sets the telephony plan only when `raw.Connection != "" && telephony`. The Daily route has neither a connection nor a telephony channel, so `Target.Telephony` is `nil` on it. Every existing route fact is read through that plan, which means the Daily route currently has no way to carry any fact at all. T003 opens that seam and four of five stories depend on it.

**Two.** `bot.py.tmpl:298` calls the Daily transfer primitive with no idempotency guard, and the mechanism the carrier routes use for it is forbidden on this route. Spec FR-008 requires at most one attempt per call. This is a caller-facing behavioural gap, not a test gap, and it lands in US2.

---

## Phase 1: Setup

**Purpose**: Establish a known-good baseline so later failures are attributable.

- [X] T001 Confirm the baseline gate is green by running `make fmt`, `make lint`, `make build`, and `make test` from the worktree root, and record any pre-existing failure in `specs/004-pipecat-cloud-telephony/quickstart.md` before changing code
- [X] T002 [P] Build a local binary with `go build -o /tmp/unmute .` for the manual contract checks in `specs/004-pipecat-cloud-telephony/quickstart.md`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Give the Daily route a way to carry route facts without a telephony plan, and lock the door on new authoring fields before anyone reaches for one.

**⚠️ CRITICAL**: US1's instruction work, US2, US3, and US5 all read the T003 seam. Only US4 is independent of it.

- [X] T003 Add the `RouteAccountPrerequisite` struct and a plan-free `RouteAccountPrerequisites(provider Provider, transport, carrier string) []RouteAccountPrerequisite` lookup to `internal/target/telephony.go`, with the fields and rules from `specs/004-pipecat-cloud-telephony/data-model.md`
- [X] T004 Add the single `daily_dialout` prerequisite instance in `internal/target/telephony.go`, with `NeededBy` naming `cold_transfer` and outbound, its `Docs` URL, and its `Verified` date of 2026-08-12 per the research verification log
- [X] T005 [P] Add unit tests in `internal/target/table_test.go` asserting the lookup returns `daily_dialout` for the Daily route, returns nothing for the carrier websocket routes, and that no prerequisite has an empty `NeededBy`
- [X] T006 [P] Add a unit test in `internal/target/table_test.go` asserting every prerequisite carries a non-empty `Docs` and `Verified`, matching how every other provider claim in the rulebook is recorded
- [X] T007 [P] Add a test in `internal/spec/` asserting the derived authoring schema contains no hosting-model field and no telephony channel on the Daily route, satisfying spec FR-030 so a later change that adds either fails a test instead of arriving quietly

**Checkpoint**: Route facts are reachable for a route with no telephony plan, and the "no new authoring field" rules are enforced rather than merely written down.

---

## Phase 3: User Story 1 - A Daily phone agent answers a real call (Priority: P1) 🎯 MVP

**Goal**: An inbound Daily call reaches the agent instead of failing while the transport is built.

**Independent Test**: Compile the Daily example and assert the emitted `bot.py` builds a Daily transport parameter object that accepts inbound call fields, then instantiate it against the real pinned package. No phone account needed.

### Tests for User Story 1 ⚠️

> Write these first and watch them fail. The current code emits the generic parameter class, so the property assertion must fail before the fix.

- [X] T008 [P] [US1] Add an L1 generation test in `internal/generate/pipecat_v1_test.go` asserting a Daily-route package emits a `transport_params` entry for the `daily` key whose class accepts inbound call fields, expressed as a property so an upstream rename fails honestly rather than passing a stale string match
- [X] T009 [P] [US1] Add an L1 test in `internal/generate/pipecat_v1_test.go` asserting a non-Daily package keeps the generic parameter class for every key and emits no Daily-specific import, guarding the scoping decision in research D2
- [X] T010 [P] [US1] Add an L4 smoke test in `internal/generate/pipecat_v1_smoke_test.go` that installs the emitted Daily project and instantiates its transport parameters against real `pipecat-ai==1.5.0`, which is the only layer that can resolve the open import-path question from research D1
- [X] T011 [P] [US1] Extend the T010 smoke to also instantiate with an empty request body, proving spec FR-006: a session carrying no call details behaves exactly as today, which is the framework's documented no-op path and the one every non-phone session takes

### Implementation for User Story 1

- [X] T012 [US1] Add the transport-parameter-class fields to `pipecatData` in `internal/generate/pipecat_v1.go`, carrying the class and its import as one unit so an emitted class structurally cannot lose its import
- [X] T013 [US1] Select the Daily parameter class in `internal/generate/pipecat_v1_build.go`, gated on the target transport being `daily-sip` so no other route churns, and confirm the import path against the smoke result from T010 rather than against the docs
- [X] T014 [US1] Render the class and its conditional import in `internal/generate/templates/pipecat_v1/bot.py.tmpl`, replacing the hardcoded generic entry at the `daily` key in the `transport_params` map
- [X] T015 [US1] Add the attach-a-phone-number instructions to `internal/generate/templates/pipecat_v1/README.md.tmpl` for the Daily route, satisfying spec FR-007 and the README requirement in `contracts/artifacts.md`, describing the platform's managed dial-in webhook and stating that no webhook server of the author's own is involved
- [X] T016 [US1] Regenerate goldens with `go test ./internal/generate/ -update-pipecat`, then read the diff and confirm only the Daily golden moved; a diff in any other golden means T013 was not scoped and must be narrowed
- [X] T017 [US1] Run `make smoke` and confirm the Daily bot imports and instantiates, then record the resolved import path in `specs/004-pipecat-cloud-telephony/research.md` under D1's open item

**Checkpoint**: The defect that blocks step 4 of the transfer recipe is fixed and proven against the real package.

---

## Phase 4: User Story 2 - Cold transfer completes on the managed Daily route (Priority: P1)

**Goal**: The caller reaches a person and the agent leaves, at most one transfer is ever attempted, and every failure path keeps the caller connected.

**Independent Test**: Run the recipe in `docs/TRANSFERS.md` §4 Pipecat rig, including the failure drill and a double-request drill, and record the dated result.

**Note**: The transfer lowering itself already exists. What does not exist is the idempotency guard, which is real missing behaviour rather than a missing test.

### Tests for User Story 2 ⚠️

- [X] T018 [P] [US2] Add an L1 test in `internal/generate/pipecat_v1_test.go` asserting a Daily project emits an in-process guard so a second transfer request produces no second attempt, written against the observable property so the carrier routes and the Daily route share one assertion despite using different mechanisms (spec FR-008, `data-model.md` TransferAttemptGuard)
- [X] T019 [P] [US2] Add an L1 artifact test in `internal/generate/pipecat_v1_test.go` asserting a Daily project declares no service and no public endpoint of its own, per spec FR-027 and `contracts/artifacts.md` invariant 1, checked against the artifact's runtime description rather than README prose
- [X] T020 [P] [US2] Add an L1 regression test in `internal/generate/pipecat_v1_test.go` asserting the carrier websocket routes keep their current services, endpoints, and credentials, per spec FR-027 and invariant 2
- [X] T021 [P] [US2] Add an L1 test in `internal/generate/pipecat_v1_test.go` asserting neither route shape's credentials appear in the other, by set comparison so a credential added later cannot quietly appear in both
- [X] T022 [P] [US2] Add an L1 test in `internal/generate/pipecat_v1_test.go` asserting no transfer tool is emitted on any carrier websocket route, guarding the base branch's deliberate deletion against a later change to shared emitter code

### Implementation for User Story 2

- [X] T023 [US2] Add the in-process transfer guard to `internal/generate/templates/pipecat_v1/bot.py.tmpl` at the Daily cold-transfer branch near line 298, so a second request returns the already-transferred result without calling the platform primitive again; do **not** reach for the shared control store, which `contracts/artifacts.md` forbids on this route and the constitution forbids leaving idle
- [X] T024 [US2] Mark the guard with a `ponytail:`-style comment naming why it is in-process rather than in the shared store, so the simplification reads as intent rather than oversight, per the constitution's complexity rule
- [X] T025 [US2] Fix whatever T019 through T022 reveal in `internal/generate/pipecat_v1_build.go`, or record in `specs/004-pipecat-cloud-telephony/quickstart.md` that they passed unchanged, since these are regression guards and may need no production change
- [X] T026 [US2] Regenerate goldens with `-update-pipecat` and read the diff; only the Daily golden should move, gaining the guard
> **The three below need real accounts and real money and were not run.** They need
> a Pipecat Cloud account, a Daily domain with dial-out granted, and two answerable
> phones. The rig recipe now includes the double-request drill (step 6). Everything
> the recipe depends on is built and proven offline against real `pipecat-ai`
> 1.5.0; `docs/TRANSFERS.md` records the row as provisional with that evidence and
> says plainly that offline proof is not a phone call.

- [ ] T027 [US2] Run the live recipe from `docs/TRANSFERS.md` §4 Pipecat rig end to end: deploy, attach the number, then the answer test, the cold transfer test, the failure drill, and a double-request drill asking for a transfer twice in one call
- [ ] T028 [US2] Record the dated run result in the Status section of `docs/TRANSFERS.md`, and land any correction the run found in that document before touching code, per its own stated rule
- [ ] T029 [US2] Tear the rig down per `specs/004-pipecat-cloud-telephony/quickstart.md`: release the test-only number, delete the deployed agent, remove the test credential set

**Checkpoint**: partially reached. A double request cannot double-transfer a caller, and the guard is tested. The Daily cold transfer row stays **provisional**, because no credentialed run has happened; the Status table now says so per row instead of one sentence covering three.

---

## Phase 5: User Story 3 - Prerequisites are named before a call is placed (Priority: P2)

**Goal**: The author learns about Daily's dial-out requirement from `validate`, before spending money on a live call.

**Independent Test**: Validate a Daily cold-transfer package with no accounts anywhere and confirm the prerequisite is named; validate a Daily package without a transfer and confirm it is not.

### Tests for User Story 3 ⚠️

- [X] T030 [P] [US3] Add an L2 command test in `internal/cli/validate_test.go` asserting `validate` on a Daily cold-transfer package names the dial-out prerequisite on stderr and still exits 0, because a prerequisite is a route fact and not a failure
- [X] T031 [P] [US3] Add an L2 command test in `internal/cli/validate_test.go` asserting `validate` on a Daily package with neither cold transfer nor outbound prints no prerequisite text, which is the half of the rule that stops it becoming a standing banner
- [X] T032 [P] [US3] Add an L1 test in `internal/generate/pipecat_v1_test.go` asserting the prerequisite reaches both the emitted README and `compile-report.json`
- [X] T033 [P] [US3] Add an L1 test in `internal/generate/pipecat_v1_test.go` asserting every credential the Daily route needs, including the transfer destination, appears in the emitted startup check so a missing value fails by name rather than as an unanswered call (spec FR-011); if it already passes, record that it is pre-satisfied rather than deleting the test
- [X] T034 [P] [US3] Extend the existing emitter-versus-capability-table agreement test in `internal/target/table_test.go` to cover prerequisites, so the rulebook, the emitted project, and `docs/user/` cannot disagree

### Implementation for User Story 3

- [X] T035 [US3] Read the prerequisite lookup from T003 in `internal/ir/validate.go` and attach the matching prerequisites to the target validation row, keyed on the capabilities the package actually uses, without giving it any of the four capability tags per research D3
- [X] T036 [US3] Print the prerequisite from the validate report path in `internal/cli/validate.go` through `cmd.ErrOrStderr()`, never `fmt.Println`, keeping the exit code at 0
- [X] T037 [US3] Carry the prerequisites into `pipecatData` in `internal/generate/pipecat_v1.go` and into the report in `internal/generate/pipecat_v1_build.go`
- [X] T038 [US3] Add the prerequisite section to `internal/generate/templates/pipecat_v1/README.md.tmpl`, emitted only when the package uses a capability that needs one
- [X] T039 [US3] Regenerate goldens with `-update-pipecat` and read the diff; the Daily golden gains the section and no other golden should move

**Checkpoint**: A paid account feature is no longer discovered by a failed live call.

---

## Phase 6: User Story 4 - Region is declared once and everything follows it (Priority: P2)

**Goal**: One declared region reaches every emitted reference, and a region that cannot be honoured is refused.

**Independent Test**: Compile for a non-default region and assert every emitted reference agrees; compile with no region and assert the instructions state the default and that the credential store follows it.

**Note**: Research D4 found the emitted README already handles most of this. Scope here is the validation side and the regression guard, not a rewrite.

### Tests for User Story 4 ⚠️

- [X] T040 [P] [US4] Add an L1 test in `internal/generate/pipecat_v1_test.go` asserting every emitted region reference resolves from the one declared value, covering the deploy manifest region line and the credential-store command
- [X] T041 [P] [US4] Add an L1 test in `internal/generate/pipecat_v1_test.go` asserting that with no region declared the emitted instructions state the default region and that the credential store follows the same default, which are two facts because they fail independently
- [ ] T042 [P] [US4] **Not done as written; superseded.** Asked for an L2 test that a region "that cannot be honoured" fails, exit 1. Done for the three cases that are knowable without a region list, as `TestValidateRefusesARegionItCannotHonour` (empty entry, the same region twice, more than one region on Pipecat) plus `TestValidateForwardsAnUnknownRegionCode`, which pins that an unrecognised code still compiles. A test that an unknown *code* fails would require the allow-list T044 was asked to add, which the contract forbids. See T044.
- [X] T043 [P] [US4] Add an L1 test in `internal/generate/pipecat_v1_test.go` asserting the forwarded region appears in `compile-report.json`, since Principle II requires every forwarded-unchecked value to be inspectable

### Implementation for User Story 4

- [ ] T044 [US4] **Not done: it contradicts the locked contract, so the contract wins (Principle IV).** `docs/SCHEMA.md` N18 and N32 state that region codes are forwarded exactly as written, that the platform CLI is the validator, and that no list of codes is kept in this repository because both platforms change theirs without notice. Research D4 considered validating against the four known regions and rejected it for the same reason, so this task over-reached past its own plan. Adding a refusal would refuse packages that are correct. No new refusal was added; the three existing ones are now covered by T042's tests.
- [X] T045 [US4] Confirm the credential-store region facts already emitted in `internal/generate/templates/pipecat_v1/README.md.tmpl` satisfy T040 and T041, and adjust only the wording a test proves missing rather than rewriting the section

**Checkpoint**: The most likely silent misconfiguration in the setup is now impossible to ship.

---

## Phase 7: User Story 5 - The agent places outbound calls on Daily (Priority: P3)

**Goal**: A Daily package can declare that the agent places calls.

**Independent Test**: Declare an outbound Daily agent, compile, and assert the prerequisite is named and the instructions describe how a call is started.

**Scope note**: Caller identity left this story on 2026-08-12. It is a new authoring field and the constitution requires the full compliance checklist for one, which three tasks did not cover. It is now Out of Scope in the spec as its own feature.

### Tests for User Story 5 ⚠️

- [ ] T046 [P] [US5] **Blocked: there is no way to declare outbound calling on this route.** A telephony channel requires a resolved connection plan and the Daily route has none, so `channels.phone` on it fails at build time: `target "pipecat" requires connection for telephony`. Verified by trying it. Adding a path would be new authoring surface, which FR-002 rules out and T007 now tests against, and the constitution prices an authoring change at a full amendment cycle. Declaring outbound on the Daily route is its own feature. T047 and T048 are done, so the emitted project describes how the platform starts an outbound call and what identity the recipient sees.
- [X] T047 [P] [US5] Add an L1 test in `internal/generate/pipecat_v1_test.go` asserting the emitted instructions describe how an outbound call is started and state what identity the recipient sees, given the package cannot choose one

### Implementation for User Story 5

- [X] T048 [US5] Render the outbound instructions in `internal/generate/templates/pipecat_v1/README.md.tmpl`, including that the recipient sees a provider-chosen identity, and that international dial-out is enabled separately per domain per the research verification log
- [X] T049 [US5] Regenerate goldens with `-update-pipecat` and read the diff

**Checkpoint**: Outbound calling on Daily is declarable and its prerequisite is visible.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: The command boundary, backward compatibility, and the documents. None belongs to a single story.

### The `--telephony` boundary (spec FR-028)

- [X] T050 [P] Add an L2 test in `internal/cli/dev_test.go` asserting `unmute dev --telephony` on a Daily target exits 1 and that the message names the route, names the browser and console modes that do work on it, and points at the deploy path, checking message content and not only the exit code
- [X] T051 Implement the refusal in `internal/cli/dev.go`, returning a wrapped error rather than calling `os.Exit`, and make sure it does not claim telephony is unsupported on the route, which would be false since Daily is the only Pipecat telephony route there is

### Backward compatibility (spec FR-031)

- [X] T052 Add a test that compiles every package under `examples/` and asserts each emits byte-identical output to before this feature, except the Daily example whose changes are enumerated, or fails with a message naming what to change; this is the only thing that makes spec FR-004 and SC-008 real rather than asserted

### Documents

- [X] T053 [P] Correct the adopted stance in `docs/DEPLOYMENT.md` so remote deployment uses the managed clouds, dated, with the superseded stance left visible as history per the amendment procedure
- [X] T054 [P] Scope the cloud-free claim in `docs/TELEPHONY.md` to local runs so it no longer reads as a claim about deployment, without deleting it, because it stays true of `unmute dev`
- [X] T055 [P] Add a numbered dated amendment to `docs/SCHEMA.md` recording that Pipecat telephony is the Daily route, and narrow the §4.9 statements that currently describe Pipecat carrier-WebSocket inbound and outbound more broadly than the code supports; append, never rewrite in place
- [X] T056 [P] Update the Pipecat target page under `docs/user/` to follow the route facts from `internal/target`, keeping the two-way sync test green
- [X] T057 Correct the Status section in `docs/TRANSFERS.md` so the LiveKit rows either record their dated run results or stay explicitly provisional, per spec FR-029; the document must not claim a proof state that has not happened
- [X] T057a Correct the false capability claim in `docs/TRANSFERS.md` §1, per spec FR-032: the `pipecat` Daily `warm` cell and the "Why the two 'no' rows are firm" paragraph currently state that Pipecat has no warm primitive on any route, which the documentation contradicts. Restate it as: Daily documents warm transfer, this project does not emit it yet because the pattern requires the generated bot to own audio control, verified 2026-08-12, and link the Daily PSTN page. Change **no** route tag, capability row, or authoring field: emitting warm is feature 005
- [X] T057b [P] Audit every other "no" or "not supported" statement about a provider in `docs/TRANSFERS.md`, `docs/SCHEMA.md` §9, and `docs/user/` for the same defect, and make each one say whether it means the platform cannot do it or this project does not emit it yet, with a verification date, per spec SC-013 and the constitution's rule that any statement of support must say which it means

### Final gate

- [X] T058 Run the full offline half of `specs/004-pipecat-cloud-telephony/quickstart.md` and confirm every listed signal
- [X] T059 Run `make fmt`, `make lint`, `make build`, `make test` and confirm zero failures with zero Python
- [X] T060 Run `make smoke` and confirm it passes, or skips cleanly when `uv` is absent, and never entered the pull request gate

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies
- **Foundational (Phase 2)**: after Setup. T003 and T004 block US1's instruction work, US2, US3, US5. T007 blocks nothing but should land early, since its whole job is to fail if someone adds a field
- **US1 (Phase 3)**: the params fix needs only Setup; T015 needs T003
- **US2 (Phase 4)**: needs US1 complete. The only genuine cross-story dependency, and it is physical: you cannot watch a transfer on a call that never connects
- **US3 (Phase 5)**: needs T003 and T004
- **US4 (Phase 6)**: independent of Phase 2 entirely; can run any time after Setup
- **US5 (Phase 7)**: needs T003, T004, and shares the README template with US1 and US3
- **Polish (Phase 8)**: documents can start immediately. T052 must run after every code change. T057 through T060 come last

### Story Dependencies

- **US1 (P1)**: independent. The MVP.
- **US2 (P1)**: depends on US1.
- **US3 (P2)**: independent of US1 and US2 once T003 lands.
- **US4 (P2)**: fully independent of every other story.
- **US5 (P3)**: independent of US1, US2, and US4; shares the README template with US1 and US3.

### Golden-file serialization

T016, T026, T039, T049 all regenerate Pipecat goldens. **Do not run them in parallel.** Each must be followed by reading the diff before the next begins, per the constitution's golden-file rule. T016's whole purpose is proving that only the Daily golden moved.

### Template contention

`internal/generate/templates/pipecat_v1/README.md.tmpl` is touched by T015, T038, T045, and T048. Sequence them.

### Parallel Opportunities

- T005, T006, T007 together
- T008, T009, T010, T011 together
- T018 through T022 together
- T030 through T034 together
- T040 through T043 together
- T046 and T047 together
- T053, T054, T055, T056 together
- US4 in parallel with any other story, by a second person

---

## Parallel Example: User Story 1

```bash
# All four US1 tests together, before any implementation:
Task: "L1 property test for the Daily transport parameter class in internal/generate/pipecat_v1_test.go"
Task: "L1 scoping test that non-Daily packages keep the generic class in internal/generate/pipecat_v1_test.go"
Task: "L4 smoke instantiating the Daily project against real pipecat-ai 1.5.0 in internal/generate/pipecat_v1_smoke_test.go"
Task: "L4 smoke instantiating with an empty body to prove the no-call-details path in internal/generate/pipecat_v1_smoke_test.go"
```

---

## Implementation Strategy

### MVP: User Story 1 only

1. Phase 1 Setup
2. Phase 2 Foundational (T003 and T004 are needed only for T015; the params fix itself needs neither)
3. Phase 3 US1
4. **Stop and validate**: an inbound Daily call now reaches the agent. This alone unblocks the recipe that is stuck today.

### Incremental delivery

1. Setup, then US1 → the defect is fixed and proven offline
2. US2 → the guard lands and the live run happens → the headline outcome
3. US3 → the paid prerequisite stops being a surprise
4. US4 → the silent region misconfiguration becomes impossible
5. US5 → outbound calling becomes declarable
6. Polish → the documents stop stating a stance the project does not hold

### Parallel team strategy

With two people: one takes US1 then US2, the critical path and the only place a live account is needed. The other takes US4 immediately, then US3 and US5 in sequence because they share the README template. The document tasks T053 through T056 are independent of all code.

---

## Notes

- Total: 60 tasks. US1 has 10, US2 has 12, US3 has 10, US4 has 6, US5 has 4, with 7 in setup and foundational and 11 in polish.
- Only T027 through T029 need real accounts and real money. The other 57 are offline.
- T017 closes the one deliberately unresolved research item, the Daily parameter import path, by importing the real package rather than reading more documentation.
- T025 and several US2 tests are regression guards that may pass without any production change. T025 says so explicitly, so a green test is not mistaken for a missed task.
- T023 is the only task in this list that changes runtime behaviour a caller can feel and was not in the original plan. It exists because `/speckit-analyze` found spec FR-008 had no implementation on this route.
- Commit after each task or logical group. Stop at any checkpoint to validate a story on its own.
