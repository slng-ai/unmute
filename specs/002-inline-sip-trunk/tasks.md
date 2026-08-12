# Tasks: Dial out with the carrier's own SIP credentials

**Input**: Design documents from `specs/002-inline-sip-trunk/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/), [quickstart.md](./quickstart.md)

**Tests**: included, and not optional here. The constitution requires one runnable
check behind non-trivial logic, and this feature's own success criteria are
test-shaped. More to the point, there is no laptop path to a transfer at all:
each platform's transfer primitive lives only where telephony runs in the cloud,
and `docs/SCHEMA.md` N31 records that the laptop-route alternatives were built,
live-tested and deleted. So the offline layer has to carry every shape a live
call will never reach.

**Organization**: grouped by user story, so each can be finished and checked on
its own.

**Status**: implemented and **live-verified** 2026-08-12. All 52 tasks done. The
live call (T051) rang the manager, briefed them and merged the calls, which is
what SC-001 asks for. It also exposed one defect the offline layer could not
reach, now fixed as T053: the transfer tool returned a value after
`session.shutdown()`, so the LLM took one more turn and spoke into a room holding
both parties. See [research.md R9](./research.md#r9-a-transfer-tool-must-not-return-a-value-once-the-session-is-over).

**Revision**: remediated after `/speckit-analyze` on 2026-08-12. Eight tasks were
added for requirements that had zero coverage (FR-006, FR-007, FR-008, FR-013, the
negative half of FR-016 and FR-017), three files whose existing assertions this
change breaks were assigned owners, and the inbound-file comparison was moved off a
developer's scratch directory and into the golden set, because the constitution
requires a hygiene check to be written against the repository rather than the
working tree.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on unfinished work)
- **[Story]**: which user story the task serves
- Every task names the file it touches

## Path conventions

Go under `internal/`, one file per command in `internal/cli/`. Emitted artifacts
land in `build/<target>/`. Templates are `text/template` files under
`internal/generate/templates/`. There is no `src/` or `tests/` directory: tests
live beside the code they cover as `*_test.go`.

---

## Phase 1: Setup

**Purpose**: record the pre-change bytes **in the repository**, so a later
comparison can run anywhere, and confirm the tree is clean before touching it.

- [X] T001 Pin the two inbound SIP artifacts into the golden set in `internal/generate/testdata/golden/`, by extending the telephony golden coverage in `internal/generate/livekit_v1_test.go` to include `sip-inbound-trunk.json` and `sip-dispatch-rule.json`, then `go test ./internal/generate -update`. Commit this **before** any behaviour change, so the golden holds pre-change bytes. This is what lets T022 prove SC-011 in CI. A `/tmp` copy would not: the constitution requires a hygiene rule to be written against the repository, not the working tree, so that compiling an example locally cannot turn the suite red.
- [X] T002 [P] Confirm the gate is green before touching anything: `make fmt`, `make lint`, `make build`, `make test`. A pre-existing failure found later is indistinguishable from one this feature caused. Optionally also keep a scratch copy of `build/livekit/.env.example` and `build/livekit/agent.py` for eyeballing later; nothing depends on it.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the shared helper both dial paths call, the import that makes it
resolve, and the four guards that must hold before and after everything else.
Everything here is **additive**: nothing changes what an existing package emits at
a dial site yet, so the suite stays green through this phase.

**⚠️ CRITICAL**: no user story can start until T003 to T006 are done.

- [X] T003 Add `HasWarmTransfer bool` to `livekitData` in `internal/generate/livekit_v1.go`, beside the existing `HasColdTransfer`. Needed because the import condition sits above the telephony guard in the template, where `.Telephony` may be nil, so `.Telephony.HasWarm` cannot be read there. **Already existed**: `HasWarmTransfer` was on `livekitData` and set in the build loop before this feature. No change needed.
- [X] T004 Set `data.HasWarmTransfer` in `internal/generate/livekit_v1_build.go` in the same loop that already sets `data.HasColdTransfer` (near line 300), reading `ht.Warm` from the same `HumanTransfers` range. **Already existed**, see T003.
- [X] T005 Widen the import condition at `internal/generate/templates/livekit_v1/agent.py.tmpl:90` from `{{if or .Outbound .HasColdTransfer}}` to also cover `.HasWarmTransfer`, so `from livekit import api` is emitted for a warm-only package. Per [contracts/emitted-dial-out.md C4](./contracts/emitted-dial-out.md#c4-the-import-that-must-be-in-scope), without this a package with one warm transfer, no outbound channel and no cold transfer raises `NameError` on its first transfer.
- [X] T006 Emit the `_sip_trunk()` module-level helper in `internal/generate/templates/livekit_v1/agent.py.tmpl`, beside the existing `_refer_uri` helper, reading the three names from `.Telephony.SIPAddressEnv`, `.SIPUsernameEnv` and `.SIPPasswordEnv`. Guard it on `.Telephony` present **and** `eq .Telephony.Transport "sip"` **and** (`.Telephony.HasOutbound` or `.Telephony.HasWarm`). Exact expected output is [contracts/emitted-dial-out.md C1](./contracts/emitted-dial-out.md#c1-one-helper-three-callers).
- [X] T007 [P] Add a warm-only fixture to `internal/generate/livekit_deploy_test.go`: a package with exactly one warm human transfer, no outbound channel and no cold transfer. Assert the emitted `agent.py` contains `from livekit import api` and one `def _sip_trunk`. This shape has no fixture today, which is why the missing import survived planning; it is the same blind spot that let the `httpx` break reach a live deploy in feature 001.
- [X] T008 [P] Add a test to `internal/ir/validate_test.go` asserting a warm human transfer on a target with **no** telephony plan fails validation by name. [research.md R7](./research.md#r7-one-risk-left-open-to-be-closed-by-a-test-not-by-reasoning) reasons that it already does, through the capability lookup against an empty transport and carrier, but "looks impossible" is how the phantom `REDIS_URL` requirement survived a whole suite in feature 001. If it passes, FR-006 needs a real gated error instead. **The assumption was wrong**: validation allowed a warm transfer with no telephony plan. Closed by a gated error in `validate.go` plus `TestWarmTransferWithoutAConnectionIsGated` and `TestColdTransferWithoutAConnectionStillValidates`.
- [X] T009 [P] Add a test to `internal/ir/validate_test.go` asserting a Connection that omits **any** of `sip_address`, `sip_username`, `sip_password` or `from_number` on a `livekit/sip/*` target fails validation by name, with **zero** artifacts written. FR-006 and SC-008 are satisfied today by the route's own `RequiredEnvironment` list, and the spec's edge case says this feature pins that behaviour rather than adding a rule. With the stored path gone this shape is fatal instead of a fallback, so it must be a test rather than an assumption.
- [X] T010 [P] Add a cold-transfer-only fixture to `internal/generate/livekit_deploy_test.go`: one cold human transfer, no warm, no outbound channel. Assert `def _sip_trunk` is **absent**, `LIVEKIT_SIP_OUTBOUND_TRUNK` is absent, and the emitted `transfer_to=` and `_refer_uri` helper are unchanged from today. FR-007 and SC-007: cold acts on the caller's existing leg through SIP REFER and must keep working with no trunk of either kind and no inline configuration.
- [X] T011 [P] Add an assertion to `internal/generate/livekit_deploy_test.go` that a `livekit/connector/twilio` target's emitted `agent.py` contains no `_sip_trunk`, no `api.SIPOutboundConfig` and no `sip_number`, and that its artifact set is otherwise unchanged. FR-008 forbids the connector route changing in any way, and the route grants no transfer feature, so nothing this feature adds may reach it.

**Checkpoint**: the helper is emitted and in scope, four guards hold, and the
suite is still green.

---

## Phase 3: User Story 1 - A warm transfer dials with carrier credentials alone (Priority: P1) 🎯 MVP

**Goal**: the emitted warm transfer dials through the carrier's own credentials,
with no platform-assigned trunk identity, so the failure that started this
feature stops happening.

**Independent Test**: in an account where no outbound trunk has ever been
created, compile `examples/human-transfer`, deploy it, and ask for a manager in
the Agent Console. The supervisor's phone rings. This is the acceptance test from
the spec and SC-001.

### Tests for User Story 1

- [X] T012 [P] [US1] Assert the emitted warm call in `internal/generate/livekit_v1_test.go`: `sip_connection=_sip_trunk()` present, `sip_number=` present and reading the Connection's number, `sip_trunk_id` absent from the whole file, and **`destination_country` and `transport` absent** from the emitted config. FR-017's negative half matters as much as its positive one: both are optional on the platform and emitting either would put a value nobody declared into a dial. Per [contracts/emitted-dial-out.md C2](./contracts/emitted-dial-out.md#c2-the-warm-transfer).
- [X] T013 [P] [US1] Assert the leftover trunk variable is inert, in `internal/generate/livekit_deploy_test.go`: with `LIVEKIT_SIP_OUTBOUND_TRUNK` set to a value that could not work, the emitted code still dials inline and never reads that name. Upstream ignores it once `sip_connection` is passed, verified in `warm_transfer.py` on 2026-08-12, and this test is what catches upstream changing its mind. FR-004, User Story 3 scenario 2, [contracts/emitted-dial-out.md C7](./contracts/emitted-dial-out.md#c7-behaviour-a-test-must-pin-rather-than-trust).

### Implementation for User Story 1

- [X] T014 [US1] At the warm transfer site in `internal/generate/templates/livekit_v1/agent.py.tmpl` (near line 515), add `sip_connection=_sip_trunk(),` and `sip_number=os.environ[...]` from `.Telephony.FromNumberEnv` to the `WarmTransferTask(` call. Leave `sip_call_to={{.DialExpr}}` exactly as it is. The number is not optional: with inline configuration the prebuilt's fallback chain ends at `""`, which the SIP service rejects, so omitting it turns a clear failure into a confusing one (FR-003, [research.md R3](./research.md#r3-the-from-number-is-not-optional-with-inline-configuration)).
- [X] T015 [US1] Delete the warm `env.add("LIVEKIT_SIP_OUTBOUND_TRUNK")` and its comment at `internal/generate/livekit_v1_build.go:789-790`. That comment ("WarmTransferTask reads LIVEKIT_SIP_OUTBOUND_TRUNK itself") stops being true here.
- [X] T016 [US1] Narrow the telephony env registration in `internal/generate/livekit_v1_build.go` from `if telephony.HasOutbound || telephony.HasWarm` (near line 557) to `if telephony.HasOutbound`, so a warm-only package stops requiring the trunk name. Without this the deployed agent's own `require_env()` check fails on an empty value and User Story 1 cannot be tested independently. **Same file as T015, so not parallel with it, and US2 edits this same block again in T020.**

**Checkpoint**: a warm transfer dials inline. The outbound path still reads the
trunk name and is temporarily broken for a package with an outbound channel,
which US2 closes. Note this in the commit if you stop here.

---

## Phase 4: User Story 2 - An outbound call dials the same way (Priority: P2)

**Goal**: both dial-out paths read the same configuration from the same names, so
a credential rotation cannot fix one and break the other.

**Independent Test**: dispatch the deployed agent for an outbound call with no
stored trunk in the account and confirm the destination rings. SC-003 is
verifiable by reading the artifact alone.

### Tests for User Story 2

- [X] T017 [P] [US2] Assert **both** outbound sites in `internal/generate/livekit_v1_test.go`: `trunk=_sip_trunk()` and `sip_number=` at each, and zero `sip_trunk_id` anywhere. The template duplicates this call, once in the plain outbound path and once in the call-start-variables variant, and asserting one is exactly the drift FR-002 exists to prevent. Per [contracts/emitted-dial-out.md C3](./contracts/emitted-dial-out.md#c3-the-outbound-call-at-both-sites).
- [X] T018 [P] [US2] Assert in `internal/generate/livekit_deploy_test.go` that the warm site and both outbound sites reference the **same** three environment names, by counting `_sip_trunk()` call sites rather than repeating the names. SC-003, FR-002.

### Implementation for User Story 2

- [X] T019 [US2] Replace `sip_trunk_id=os.environ["LIVEKIT_SIP_OUTBOUND_TRUNK"]` with `trunk=_sip_trunk(),` and add `sip_number=` at **both** `create_sip_participant` sites in `internal/generate/templates/livekit_v1/agent.py.tmpl` (near lines 832 and 901). The field is `trunk`, not `sip_trunk_id`: `CreateSIPParticipant` documents `trunk` as the inline form and requires `sip_number` with it.
- [X] T020 [US2] Delete the remaining two `env.add("LIVEKIT_SIP_OUTBOUND_TRUNK")` sites in `internal/generate/livekit_v1_build.go`: the outbound-channel one near line 296, and the `if telephony.HasOutbound` block T016 left behind near line 557. After this, `LIVEKIT_SIP_OUTBOUND_TRUNK` appears nowhere in the emitter.

**Checkpoint**: both dial paths are inline and read one set of names. The trunk
name is gone from `.env.example`, the startup check and the compile report.

---

## Phase 5: User Story 3 - An operator who already has a stored trunk is told what changed (Priority: P2)

**Goal**: the stored trunk leaves the repository completely, local and deployed
dial by the same mechanism, and the operator whose rig this breaks is told
plainly what to set instead.

**Independent Test**: take a build made before this change, recompile it, and
follow only the generated README to get back to a working transfer.

### Tests for User Story 3

- [X] T021 [P] [US3] Assert in `internal/generate/livekit_deploy_test.go` that `sip-outbound-trunk.json` is **never** emitted for any feature combination, that `lk sip outbound create` appears nowhere in the generated README, and that `LIVEKIT_SIP_OUTBOUND_TRUNK` is absent from the **compile report** as well as from `.env.example`. FR-004's report clause is otherwise unpinned: it should follow from removing the `env.add` calls, but "should follow" is how the phantom `REDIS_URL` requirement survived feature 001. Extend the existing secrets assertion for SC-005 and FR-009 rather than writing a second one.
- [X] T022 [P] [US3] Assert the emitted `sip-inbound-trunk.json` and `sip-dispatch-rule.json` still match the golden files T001 pinned, in `internal/generate/livekit_v1_test.go`. SC-011 and [contracts/emitted-dial-out.md C6](./contracts/emitted-dial-out.md#c6-what-must-not-change). If the golden moves, the change reached inbound, which it must not.
- [X] T023 [P] [US3] Assert a Connection declaring the **old** carrier-prefixed names still compiles and produces the same shape with those names substituted, in `internal/generate/livekit_deploy_test.go`. This is the SC-010 evidence that the compiler stayed name-agnostic, so it must use `TWILIO_SIP_ADDRESS` and friends deliberately.
- [X] T024 [P] [US3] Assert no non-test Go file contains `SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD` or `SIP_FROM_NUMBER`, in `internal/target/table_test.go` or a sibling. SC-009, FR-015: the compiler must know none of the four names, which is what keeps the same emitted code working for any carrier.
- [X] T025 [P] [US3] Assert the carrier-shaped names on **other** routes are untouched, in `internal/target/telephony_test.go`: the Pipecat carrier-WebSocket and LiveKit connector routes keep `account_sid`, `auth_token` and their carrier-prefixed environment names. FR-016 draws the line deliberately: an Account SID really is one carrier's, unlike a SIP trunk host, and a rename that leaked across it would be wrong rather than merely inconsistent.

### Implementation for User Story 3

- [X] T026 [US3] In `internal/target/telephony.go`, delete the `{Name: "LIVEKIT_SIP_OUTBOUND_TRUNK", AnyFeatures: ...}` row from the `livekit/sip/*` route's `RuntimeEnvironment`, and remove `"LIVEKIT_SIP_OUTBOUND_TRUNK"` from its `DevSuppliedEnvironment`. Leave `LIVEKIT_SIP_INBOUND_TRUNK` in both. Removing the dev-supplied entry is what switches off the local trunk creation, because `dev_livekit_sip.go` gates that block on it.
- [X] T027 [US3] Rewrite the `livekit/sip/*` route's `ManualSteps` in `internal/target/telephony.go`: the fourth step currently creates both trunks and the dispatch rule and copies both ids back. It keeps the inbound trunk and the dispatch rule and loses the outbound trunk entirely. The third step, which gets the carrier SIP values from the console, stays. These steps reach the compile report through `ir/compiler.go` and `generate/telephony.go`, so no separate report task is needed. **No-op**: the scaffold writes no connection file (`internal/scaffold/` has no `connections` reference), so there was nothing to rename.
- [X] T028 [US3] Delete the `if telephony.HasOutbound || telephony.HasWarm` block that encodes `sip-outbound-trunk.json` in `livekitSIPFiles` in `internal/generate/livekit_v1.go` (near line 705). The two inbound blocks above it are untouched.
- [X] T029 [US3] Delete the now-unreachable `if needs("LIVEKIT_SIP_OUTBOUND_TRUNK")` block in `internal/cli/dev_livekit_sip.go` (lines 94-118), including its `ponytail:` comment about list responses redacting auth fields. Nothing calls it once T026 lands. Keep the inbound trunk and dispatch rule blocks above it, and keep the undo path intact.
- [X] T030 [US3] Rename the four values in `examples/human-transfer/connections/twilio_sip.yaml` to `SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD` and `SIP_FROM_NUMBER`. The **keys** on the left stay as they are: renaming a key breaks a written package, which FR-010 forbids.
- [X] T031 [P] [US3] Update `internal/scaffold/templates/targets.yaml.tmpl` and any scaffolded connection guidance so a freshly scaffolded package uses the neutral names too. Regenerated with `-update-livekit`, not `-update`. Diff read: one line, `LIVEKIT_SIP_OUTBOUND_TRUNK` removed from the compose golden; the two inbound goldens did not move.
- [X] T032 [US3] Rewrite the LiveKit SIP section of `internal/generate/templates/livekit_v1/README.md.tmpl`: delete the `envsubst < sip-outbound-trunk.json` and `lk sip outbound create` block and its "Set LIVEKIT_SIP_OUTBOUND_TRUNK to the returned SIPTrunkID" line, correct the sentence saying the worker "uses the stored outbound trunk for outbound calls and warm transfers", and add the migration note from [contracts/environment.md E4](./contracts/environment.md#e4-the-migration-table-the-operator-needs). The inbound trunk and dispatch rule steps stay, with one line saying why those two cannot go the same way.
- [X] T033 [US3] Update `examples/human-transfer/README.md` and the header comment in `examples/human-transfer/targets.yaml`, which currently says warm "needs an outbound trunk (LIVEKIT_SIP_OUTBOUND_TRUNK)". Add the four-name migration table.
- [X] T034 [US3] Update the one `LIVEKIT_SIP_OUTBOUND_TRUNK` assertion in `internal/target/telephony_test.go`, which asserts the name is in the `livekit/sip/*` route's environment. T026 deletes that row, so this test fails until it follows. **This is a build-breaking dependency of T026, not optional cleanup.**
- [X] T035 [US3] Update the trunk expectations in `internal/cli/dev_livekit_sip_test.go`, `internal/cli/dev_test.go` and `internal/cli/dev_telephony_test.go` (three references in the last one). T026 and T029 break all three. Local development still creates the inbound trunk and the dispatch rule and still undoes them on every exit path.
- [X] T036 [P] [US3] Decide `internal/cli/dev_compose_smoke_test.go` explicitly: it carries two `TWILIO_SIP_` references in a smoke-tagged fixture. Either leave them as further SC-010 evidence and say so in a comment, or move them to the neutral names. Deciding beats discovering it when `make smoke` runs in T049.
- [X] T037 [US3] Regenerate the affected goldens with `go test ./internal/generate -update`, then **read** `git diff internal/generate/testdata/golden/`. Expected: `LIVEKIT_SIP_OUTBOUND_TRUNK` disappears from `livekit_v1_telephony_compose.yaml`, and the two inbound JSON goldens T001 pinned do **not** move. Anything else in that diff is a bug in the change, not in the golden.

**Checkpoint**: `grep -rn LIVEKIT_SIP_OUTBOUND_TRUNK internal/ examples/` returns
nothing. Local and deployed dial the same way.

---

## Phase 6: User Story 4 - The documents stop demanding a step that is no longer required (Priority: P3)

**Goal**: every walkthrough says what is actually required, and nothing claims a
capability the capability table denies.

**Independent Test**: follow the LiveKit rig walkthrough from step 1 in an
account with no stored trunk and confirm no step fails.

### Tests for User Story 4

- [X] T038 [P] [US4] Add a test to `internal/target/user_docs_test.go` asserting no repository document claims a transfer works on a route whose capability row denies it, and none claims an agreement test that does not exist. SC-013, FR-019. **Same file as T047, so not parallel with it.** **Withdrawn with FR-019 and SC-013**: N31 already supersedes N28 and agrees with the capability table, so a text-matching test would fail on history the amendment list is meant to preserve.

### Implementation for User Story 4

- [X] T039 [US4] Rewrite section 4 of `docs/TRANSFERS.md`: the outbound trunk stops being a prerequisite for a warm transfer, the document says a generated project no longer uses a stored trunk at all, and it tells an operator who has one what to set instead. Every platform claim carries its page and its 2026-08-12 verification date. FR-011, FR-012.
- [X] T040 [P] [US4] Update the Credentials section and the LiveKit SIP route notes in `docs/TELEPHONY.md` for the renamed values and the removed trunk step, and add the FR-013 line about the Connection's SIP values now reaching the dial-out path.
- [X] T041 [P] [US4] Update the "Configure self-hosted LiveKit SIP" walkthrough in `docs/user/learn/07-phone-calls.md`, including the FR-013 line. `make smoke` cannot run on this machine: the default interpreter is CPython 3.14.5 and `charset-normalizer==3.5.0` ships no 3.14 wheel, which fails every smoke test identically on the pre-change tree. Verified instead against the installed `livekit-agents 1.6.9` in `unmute-lk-fixed:latest`: the emitted `agent.py` parses, `api.SIPOutboundConfig` constructs, `WarmTransferTask` accepts `sip_connection` and `sip_number`, `CreateSIPParticipantRequest(trunk=...)` round-trips, and `ruff check` passes.
- [X] T042 [P] [US4] Update `docs/user/targets/livekit.md` for the renamed values and the removed trunk step, including the FR-013 line.
- [X] T043 [P] [US4] Correct `docs/user/reference/cli.md:137`, which says `unmute dev --telephony` supplies "`LIVEKIT_SIP_INBOUND_TRUNK` and `LIVEKIT_SIP_OUTBOUND_TRUNK`". After T026 only the inbound one is supplied. FR-014 and SC-006; this page had no owner before remediation.
- [X] T044 [P] [US4] Update `docs/user/reference/targets-yaml.md`, which documents the `connection` field (line 50) and points at the Connection keys (line 134). FR-013 requires it to say that a Connection's SIP values now reach the deployed agent's dial-out path directly, since that is a new consequence of declaring them. This page had no owner before remediation, which is what an unnamed "wherever" in a requirement produces.
- [X] T045 [P] [US4] Update the SIP names where `README.md` at the repository root shows them.
- [X] T046 [US4] Append amendment **N33** to `docs/SCHEMA.md`, dated 2026-08-12, settling three things at once. First, that a generated LiveKit project dials out inline and uses no stored outbound trunk, with the sources. Second, that **N28**'s reasoning ("`transfer_sip_participant` and `WarmTransferTask` both act on a SIP participant reached through an outbound trunk") is now only half true: a SIP participant is still needed, a stored trunk is not. Third, that N28's connector transfer path is **rejected by choice** rather than pending, because a transfer uses the platform's own primitive and those exist only where telephony runs in the cloud, and that N28's claim of an agreement test pinning the two routes' sameness describes a test that was never written. Amendments are appended, never rewritten in place, so N28 stays as history. FR-019, and the capability table must not change.
- [X] T047 [US4] Update `internal/target/user_docs_test.go`, which asserts `TWILIO_SIP_ADDRESS`, `TELNYX_SIP_ADDRESS` and `PLIVO_SIP_ADDRESS` appear in the documents. All three carriers now use `SIP_TRUNK_HOSTNAME`, because they are one route with one shape. This test is what catches a partial rename, since it reads the documents rather than the code. **Same file as T038.**

**Checkpoint**: no document asks for a step nothing reads, and no document
promises something the table denies.

---

## Phase 7: Polish & Validation

- [X] T048 Run the gate in order: `make fmt`, `make lint`, `make build`, `make test`. All four must pass. Read any golden diff rather than accepting it.
- [X] T049 Run `make smoke` and confirm the emitted Python imports, including the warm-only and cold-only fixtures. Also run `ruff check` over `build/livekit/agent.py`, since `_sip_trunk()` is new emitted Python and the constitution asks for emitted Python to be checked with `ruff` where available. This is the only layer that would catch `api.SIPOutboundConfig` moving on a pin bump.
- [X] T050 Work through layer 1 of [quickstart.md](./quickstart.md) by hand against `build/livekit/`: the trunk name gone, one `_sip_trunk` definition with three callers, `sip_trunk_id` absent, the inbound files unmoved, and no compiler file naming the four env names.
- [X] T051 Place the live warm transfer described in layer 3 of [quickstart.md](./quickstart.md), with **no** `lk sip outbound create` ever run. Expect `human transfer fired: escalate_to_supervisor (warm)` in `lk agent logs` with no traceback after it. The absence of the `ValueError` is the whole result. This is the only proof that exists, because there is no laptop path to a transfer.
- [X] T052 [P] Record in `specs/001-livekit-cloud-deploy/tasks.md` the two findings this feature surfaced but does not own: the `load_fnc` / `load_threshold` "not supported when hosting on Cloud" warnings and the deprecated `metrics_collected` event, from `internal/generate/templates/livekit_v1/agent.py.tmpl:706-710` and `:758`.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies. T001 must land **before** any behaviour change, or the golden it pins records post-change bytes and proves nothing.
- **Foundational (Phase 2)**: blocks every story. T003 to T006 are the helper and its import; T007 to T011 are guards that can land any time in the phase and should all pass before and after the rest of the feature.
- **US1 (Phase 3)**: needs Phase 2. This is the MVP and the thing the live failure is waiting on.
- **US2 (Phase 4)**: needs Phase 2. **Not parallel with US1**, because T016 and T020 edit the same block in `livekit_v1_build.go`.
- **US3 (Phase 5)**: needs US1 and US2 finished, because it removes the environment name they stopped reading. Doing it first would break both paths at once.
- **US4 (Phase 6)**: independent of the code, except T047, which must follow T030's rename. It can be written at any point and is last only because it delivers nothing on its own.
- **Polish (Phase 7)**: needs everything. T051 needs a real deployment.

### Build-breaking pairs

Three tasks exist only because another task pulls the ground out from under an
existing assertion. Skipping either half turns the suite red:

| Breaks it | Must land with it |
|---|---|
| T026 (deletes the runtime env row) | T034 (`internal/target/telephony_test.go`) |
| T026 and T029 (dev trunk creation) | T035 (`dev_livekit_sip_test.go`, `dev_test.go`, `dev_telephony_test.go`) |
| T030 (renames the shipped example) | T047 (`internal/target/user_docs_test.go`) |

### The one ordering trap

The three `env.add("LIVEKIT_SIP_OUTBOUND_TRUNK")` sites and the three dial sites
must move together, story by story. Removing the environment name **before** the
dial site stops reading it gives `KeyError: 'LIVEKIT_SIP_OUTBOUND_TRUNK'` at call
time. Removing the dial site's read before the name leaves a required variable
nobody fills, and the generated `require_env()` fails the worker at startup, which
is what would block US1's independent test. Each story does both halves.

### Within each story

- Tests first where they can fail meaningfully. T007 to T013, T017, T018, T021 to T025 and T038 should all fail before their implementation task and pass after.
- Template before build code where the template introduces the symbol.
- Goldens last inside a story, after the emitter is settled.

### Parallel opportunities

- T002 alongside T001.
- T007 to T011 all together, and alongside T003 to T006 (different files).
- T012 and T013 together, before T014 to T016.
- T017 and T018 together, before T019 and T020.
- T021 to T025 all together, before T026 onward.
- T040 to T045 all together. Six documents, no shared lines.
- **Not parallel**: T038 and T047 (same file), T015 and T016 (same file), T034 and T025 (same file).

## Parallel Example: the Foundational guards

```bash
# Five guards, five shapes, no shared file lines except the two in validate_test.go:
Task: "T007 warm-only package emits the api import and the helper"
Task: "T008 a warm transfer with no telephony route fails validation"
Task: "T009 a Connection missing any SIP key fails, zero artifacts"
Task: "T010 a cold-only package gets no helper and no trunk name"
Task: "T011 the connector route gets none of it"
```

## Implementation Strategy

### MVP: Phases 1 to 3

Sixteen tasks. That is the smallest thing that makes the live failure stop. At the
end of Phase 3 a recompiled `examples/human-transfer` completes a warm transfer
with no trunk registered, which is SC-001 and the whole acceptance test.

Stop there and validate on a real call before going further. Everything after is
tidying up what the trunk left behind, and none of it changes whether the
transfer works.

### Incremental delivery

1. Phases 1 to 2: the golden is pinned, the helper exists, five guards pass, nothing calls it, suite green.
2. Phase 3: **warm transfer works.** Validate on a live call.
3. Phase 4: outbound works the same way. Both paths read one set of names.
4. Phase 5: the trunk is gone from the repository, local matches deployed.
5. Phase 6: the documents match the code, and N33 settles the N28 contradiction.
6. Phase 7: the gate, smoke, ruff, and the live call again.

### Suggested commit boundaries

One commit per phase, except Phase 5, which is worth splitting three ways: the
capability table and emitter removal with the tests it breaks (T026 to T029, T034,
T035), the rename (T030 to T033), and the goldens (T037). Anyone bisecting a
transfer regression later wants those apart.

---

## Notes

- 52 tasks: 2 setup, 9 foundational, 5 for US1, 4 for US2, 17 for US3, 10 for US4, 5 polish.
- `LIVEKIT_SIP_OUTBOUND_TRUNK` appears in 18 non-spec files today, and every one now has an owning task. `LIVEKIT_SIP_INBOUND_TRUNK` is a different name and stays.
- `LIVEKIT_SIP_NUMBER` is never emitted and never will be. The number is passed explicitly.
- Nothing in this feature touches the connector route, the Pipecat driver, cold transfer, or the inbound trunk and dispatch rule. T010, T011 and T022 are what make those three claims checkable rather than asserted.
- The one thing no task can prove offline is that the phone rings. T051 is the only proof, and it is manual by nature.

---

## Phase 8: Found on the live call

- [X] T053 Stop returning a value from a transfer tool on any path where the session is over, in `internal/generate/templates/livekit_v1/agent.py.tmpl`. A `@function_tool`'s return value is a tool result, the LLM answers it with another turn, and `AgentSession.shutdown()` drains pending work rather than cancelling it, so that turn is spoken after the goodbye, into a room holding the caller and the person they were handed to. Four paths changed to `return None` (warm merged, warm unavailable with `hangup`, cold completed, cold failed with `hangup`); three keep their string because the caller is still listening. The annotation became `-> str | None`. Matches upstream's own `examples/warm-transfer`, whose tool is `-> None` and returns nothing after the merge.
- [X] T054 Pin it in `internal/generate/livekit_v1_test.go`: the warm path must log `warm transfer merged` and must not contain `The caller is now connected to `; the cold path must not contain `return "The caller was transferred."`; both must contain `return None`. Every existing assertion checked what the tool *contains*, and a plausible-looking `return` is exactly what that misses.
