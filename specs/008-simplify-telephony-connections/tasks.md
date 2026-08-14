---

description: "Task list for 008-simplify-telephony-connections"
---

# Tasks: The connection owns the phone route

**Input**: Design documents from `/specs/008-simplify-telephony-connections/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: Test tasks are included because the spec requires them by name — FR-027
rewrites an agreement test that would otherwise pass vacuously, FR-027a adds one —
and because Principle III makes an agreement test mandatory wherever one fact is
stated twice. They are not optional here.

**Organization**: Tasks are grouped by user story. Stories are independently
shippable in the order below; see Dependencies for the two places that is not
strictly true and why.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on incomplete work)
- **[Story]**: US1–US5, mapping to the user stories in spec.md
- Every task names its file

## Path conventions

Go source under `internal/`, one package per surface. Example packages under
`examples/`. User documentation under `docs/user/`, normative documents at
`docs/`. No new package is created by this feature.

---

## A note on sequencing, before Phase 1

This is a breaking authoring change: the moment `spec.Target` stops accepting
`transport`, every shipped example fails to load. The phases below are ordered so
**every commit leaves `make test` green**, by adding the new shape first, teaching
the builder to read either, migrating the examples, and only then removing the old
shape. The transitional fallback is three lines and is deleted by T024.

If you would rather take one red step in the middle, collapse T010–T024 into a
single commit. The task list works either way; only the number of green
checkpoints changes.

---

## Phase 1: Setup

**Purpose**: Establish the baseline that later checks are measured against, and
de-risk the one technique the design depends on.

- [X] T001 Run `make test`, `make lint`, `make fmt` on a clean tree and record the result in the branch's first commit message, so "exactly two goldens change" (research R5) is measurable rather than asserted.
- [X] T002 Spike: confirm `jsonschema.For[Package]` omits a field tagged `json:"-"` while `goccy/go-yaml` still decodes it via its `yaml:` tag. Add a temporary field to `internal/spec/package.go`, assert absence with the existing `searchSchema` helper in `internal/spec/authoring_surface_test.go`, then revert. Research R7 depends on this; if it fails, stop and re-plan the moved-field rejection before writing anything else.
- [X] T003 [P] Capture the current generated `.env.example` for all five telephony examples into the scratch directory, for the diff review in T045.

**Checkpoint**: baseline recorded, R7's technique proven or disproven.

---

## Phase 2: Foundational (blocking prerequisites)

**Purpose**: Additive changes that let both the old and the new authoring shape
load, so the examples can migrate without a red tree. Nothing here removes
anything.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T004 Add `Transport` and `Carrier` string fields to `Connection` in `internal/spec/package.go`, with `json` and `yaml` tags, keeping `Kind` for now.
- [X] T005 Add `Destinations map[string]string` to `AgentFile` in `internal/spec/package.go`, beside the existing `Secrets` field.
- [X] T006 In `internal/ir/build.go` `buildTarget`, read `Transport` and `Carrier` from the named connection when the connection declares them, falling back to `raw.Transport`/`raw.Carrier` when it does not. Mark the fallback with a comment naming T024 as its deletion task.
- [X] T007 In `internal/ir/build.go` `buildTarget`, read `Destinations` from `pkg.Agent.Destinations` when non-empty, falling back to `raw.Destinations`. Same deletion comment, naming T036.
- [X] T008 [P] Add `SelectableTelephonyRoutes()` to `internal/target/telephony.go`, returning only routes whose `Features` map contains `TelephonyRouteSelected`. This is the single home for "which routes can an author actually pick" (research R6); do not duplicate the predicate at any call site.
- [X] T009 [P] Add a table test in `internal/target/telephony_test.go` asserting `SelectableTelephonyRoutes()` excludes both Exotel rows and includes every route that has a `route` feature.

**Checkpoint**: both shapes load, the resolved IR is unchanged, `make test` green.

---

## Phase 3: User Story 1 — Read a package's phone route from one file (P1) 🎯 MVP

**Goal**: A target names one connection and nothing else about the route. The
connection file declares `transport`, `carrier`, and its environment names, and
is readable on its own.

**Independent Test**: `grep -rn "transport:\|carrier:" examples/*/targets.yaml`
prints nothing, all five examples validate and compile to the same route and the
same emitted file set, and a target that still declares `transport` fails with a
message naming the connection file.

### Migrate the examples (both shapes load, so these are safe in any order)

- [ ] T010 [P] [US1] Migrate `examples/twilio-telephony-hello`: move `transport`/`carrier` from both targets into `connections/twilio_voice.yaml` and `connections/twilio_sip.yaml`, delete `kind:` from both, and rewrite the header comments so they explain the route from the connection's point of view.
- [ ] T011 [P] [US1] Migrate `examples/livekit-human-transfer`: `transport: sip` and `carrier: twilio` into `connections/twilio_sip.yaml`, delete `kind:`.
- [ ] T012 [P] [US1] Migrate `examples/pipecat-human-transfer-twilio`: `transport: cloud-websocket` and `carrier: twilio` into `connections/twilio_voice.yaml`, delete `kind:`. Rewrite the `targets.yaml` comment block, which currently explains the route as "three fields select it".
- [ ] T013 [US1] Split `examples/outbound-reminder`'s shared connection: create `connections/twilio_websocket.yaml` (`transport: carrier-websocket`) and `connections/twilio_connector.yaml` (`transport: connector`), each with `carrier: twilio` and the same three environment names, delete `connections/twilio_voice.yaml`, and point each target at its own file.
- [ ] T014 [US1] Create `examples/pipecat-human-transfer-daily/connections/daily.yaml` holding `transport: daily-sip` and nothing else, and point the target at it with `connection: daily`. This package has no `connections/` directory today.

### Migrate the test fixtures (analysis F3, F5 — missing from the first task list)

- [ ] T014a [US1] Migrate `internal/testdata/daily_carrier/targets.yaml`: move `transport: daily-sip` and `carrier: twilio` into `connections/twilio_sip_daily.yaml`, delete its `kind:`.
- [ ] T014b [US1] Migrate `internal/testdata/safe_core/targets.yaml`, the hardest package in the repository for this change and the only one exercising all four providers. Its `pipecat` target's `transport: daily-sip` moves into a new one-line connection file; its `vapi` and `deepgram` targets simply **lose `carrier: twilio`** with no connection invented for them, which is what T015a's table change makes safe; its four identical `destinations` maps merge into one agent-level map under T036. Confirm `go run . validate internal/testdata/safe_core` still prints four green targets — it does today, and that is the acceptance check.
- [ ] T014c [US1] Migrate the ~10 sites in `internal/ir/build_test.go` and `internal/ir/validate_test.go` that assign `target.Transport, target.Carrier, target.Connection = …` directly. These stop compiling the moment T015 lands, and nothing in the first task list pointed at them (analysis F4).
- [ ] T014d [US1] Confirm `internal/ir/testdata/golden/compiler.txt` is **byte-identical** after T014a–T014c. This is the direct assertion of FR-003a — the resolved IR did not move — and it only proves anything once the fixtures above are on the new shape (analysis F10).

### Remove the old shape

- [ ] T015 [US1] Tag `Transport` and `Carrier` on `Target` in `internal/spec/package.go` with `json:"-"` so they leave the derived authoring schema while still decoding, per research R7.
- [ ] T015a [US1] Remove the `carrier` condition from the four rows in `internal/target/table.go` that gate on it for providers with no route: `ColdTransfer`/Deepgram (:411), `WarmTransfer`/Vapi (:420), `WarmTransfer`/Deepgram (:421), `VoicemailDetection`/Deepgram (:427). Replace each `controlNamedCarrier("twilio", …)` with `control()` and move the Twilio requirement into a comment on the row addressed to whoever builds that driver. **This must land with T015, not after**: with the field gone and the rows unchanged, `internal/testdata/safe_core` fails with `Deepgram transfer requires carrier Twilio in the generated bridge`, a fix no author can perform (research R11, analysis F1).
- [ ] T015b [P] [US1] Add a test in `internal/ir/validate_test.go` proving a `vapi` and a `deepgram` target with **no** carrier still resolve cold transfer, warm transfer, and voicemail detection. `internal/testdata/safe_core` covers only the Deepgram cold-transfer row; the other three are unreachable from any package and would otherwise change untested.
- [ ] T015c [US1] Leave `routedControls` (`internal/target/table.go:474`) alone. It conditions on `carrier` **and** `transport` for `dtmf_send`, `dtmf_receive`, `hold`, `ivr_navigation`; a driverless target has no transport today either, so those already gate and removing `carrier` changes nothing. Verify rather than assume, then move on.
- [ ] T016 [US1] Remove `Kind` from `Connection` in `internal/spec/package.go` and set `ir.Connection.Kind` to the constant `"telephony"` in `internal/ir/build.go:110`, so the resolved schema and its goldens do not move (data-model §2).
- [ ] T017 [US1] Add the moved-field rejection for `transport` and `carrier` in `internal/ir/build.go` `buildTarget`, keyed on the field and applying to every provider, reporting through `pkg.Location("targets.yaml", …)` with the exact facts in `contracts/errors.md` §1: file, line, key, value, and the connection file it belongs in.
- [ ] T018 [US1] Add the removed-field rejection for `kind:` in a connection, per `contracts/errors.md` §2.
- [ ] T019 [US1] Require `transport` on every connection in `internal/ir/build.go`; a connection with no transport declares no route.

### Collapse the guards the new shape makes unreachable

- [ ] T020 [US1] Delete the `daily-sip` + telephony + no-carrier guard at `internal/ir/build.go:823-828`. A target can no longer name a route and omit its carrier.
- [ ] T021 [US1] Delete the `daily-sip` + carrier + no-telephony-channel silent-downgrade guard at `internal/ir/build.go:829-835`, for the same reason.
- [ ] T022 [US1] For each guard deleted in T020 and T021, replace its test in `internal/ir/build_test.go` with one proving the condition is now **unrepresentable** — the old input no longer parses — rather than deleting the test. Research R2 flags this as the way these deletions go wrong.

### Route validation

- [ ] T023 [US1] Implement the route check in `internal/ir/build.go`: with a carrier, the triple must resolve through `SelectableTelephonyRoutes()`; without a carrier, the transport must be one with a carrier-less form, which today is `daily-sip` alone. **Research R10**: a flat triple lookup here breaks `pipecat-human-transfer-daily`, because the carrier-less Daily route has no row in the capability table.

### Delete the transition

- [ ] T024 [US1] Remove the `raw.Transport`/`raw.Carrier` fallback added in T006. The connection is now the only source.

### Tests and docs for User Story 1

- [ ] T025 [P] [US1] Rewrite the two route-shape tests in `internal/spec/authoring_surface_test.go` to the new form: the Daily carrier form and the cloud-websocket forms now declare their route in a connection file. Keep both tests' original purpose — asserting the derived schema grew no property — and add `transport`, `carrier` to the names asserted absent from a target.
- [ ] T026 [US1] Rewrite `TestExampleReadmesNameTheirDeclaredTransports` in `internal/generate/examples_test.go` to read transports from each example's **connections** rather than its targets (FR-027). Prove it is not passing vacuously: change one connection's transport without touching its README and confirm the test fails.
- [ ] T027 [P] [US1] Update the five example `README.md` files for the new shape, including the route tables in `twilio-telephony-hello` and the "three fields select it" prose in `pipecat-human-transfer-twilio`, and add a connections section to `pipecat-human-transfer-daily/README.md`, which has none.
- [ ] T028 [P] [US1] Update `internal/generate/templates/pipecat_v1/README.md.tmpl` and `internal/generate/templates/livekit_v1/README.md.tmpl` wherever the emitted runbook describes the authoring shape (FR-025), **and add the browser-before-phone rule there** — FR-020 names the emitted README as one of its three surfaces and the first task list covered only the other two (analysis F6).
- [ ] T029 [US1] Append a numbered, dated amendment to `docs/SCHEMA.md` for the route move, superseding the §6.1 route-field row and the §6.3 connection example. Appended, never rewritten in place; the superseded text stays as history.
- [ ] T030 [P] [US1] Update `docs/user/reference/targets-yaml.md`: drop the `transport`, `carrier`, **and `destinations`** rows from the field table (FR-022 requires all three and the first task list named only two — analysis F7), point at the connection for the route and at `agent.yaml` for the destinations.
- [ ] T031 [P] [US1] Update `docs/TELEPHONY.md` and `docs/user/learn/twilio-walkthrough.md` everywhere they show a route.

**Checkpoint**: US1 complete. A target names one connection; all five examples
compile to the same route and file set they did before; the old shape fails with a
message naming the fix.

---

## Phase 4: User Story 2 — Test in the browser with no phone credentials (P1)

**Goal**: A phone-enabled package opens a browser session with no carrier
credentials set and no `channels.web` entry.

**Independent Test**: Run `unmute dev` on `twilio-telephony-hello` with every
`TWILIO_*` and `SIP_*` variable unset, and on `livekit-human-transfer`, which
declares only `channels.phone`. Both open a browser session.

**Note**: research R9 expects this to already work. These tasks turn a behaviour
nobody wrote down into a regression guard, which is what makes it safe to
refactor route resolution above it.

- [ ] T032 [P] [US2] Add a test in `internal/cli/dev_test.go` proving `unmute dev` on a package with a telephony channel and a declared connection neither requires nor reports the connection's environment variables.
- [ ] T033 [P] [US2] Add a test in `internal/cli/dev_test.go` proving the browser path does not require a `channels.web` entry, using a package that declares only `channels.phone` (FR-018a).
- [ ] T034 [US2] Add a test proving `unmute dev --telephony` **does** report every missing route variable by name before starting anything (FR-019), so T032 cannot be satisfied by weakening the telephony path.
- [ ] T035 [P] [US2] State the browser-first rule in the five example READMEs and in `docs/user/learn/07-phone-calls.md`: the browser is the default way to test and the phone route is opt-in (FR-020).

**Checkpoint**: US2 complete. The most common workflow in the project is held by a
test.

---

## Phase 5: User Story 5 — Know where every phone value goes (P2)

**Goal**: Destinations live in `agent.yaml` as environment names, `secrets:` lists
every name the author wrote, and the docs account for every name in
`.env.example` and mention no others.

**Independent Test**: Open any telephony example's `agent.yaml` and find every
author-written environment name in `secrets:`; open its README and find every name
its `.env.example` lists, and no name it does not.

### Destinations move

- [ ] T036 [US5] Move `destinations` in all three examples that declare them — `examples/livekit-human-transfer`, `examples/pipecat-human-transfer-twilio`, `examples/pipecat-human-transfer-daily` — and in the two fixtures, `internal/testdata/safe_core` (four targets, identical maps, so the merge is lossless) and `internal/testdata/daily_carrier`, from `targets.yaml` to the top level of `agent.yaml`. Then remove the `raw.Destinations` fallback added in T007.
- [ ] T036a [US5] Replace `internal/testdata/safe_core`'s literal `billing_line: "+14155550123"` with an environment-variable name, which FR-004d now requires, and check `docs/SCHEMA.md` §7 and `docs/user/reference/safe-core.md` for any claim about literal destinations the narrowing makes false (FR-008e, analysis F5).
- [ ] T037 [US5] Tag `Destinations` on `Target` in `internal/spec/package.go` with `json:"-"` and add the moved-field rejection per `contracts/errors.md` §1, naming `agent.yaml` as the new home.
- [ ] T038 [US5] Restrict destination values to `UPPER_SNAKE` environment variable names in `internal/ir/build.go`, rejecting the literal E.164 and `sip:` forms with the message in `contracts/errors.md` §9 (FR-004d). `validDestination` at `build.go:776` is the current check and is where the narrowing goes.
- [ ] T039 [P] [US5] Update `TestV16_ExampleDestinationsAreEnvironmentNames` in `internal/generate/examples_test.go` to read destinations from the agent rather than from each target. It already asserts the env-name rule for examples; T038 makes it a schema rule.
- [ ] T040 [US5] Confirm `internal/tui/maintain.go:263` and `internal/scaffold/scaffold.go:373` still resolve destinations correctly through `ir.Target.Destinations`, which keeps its shape. If either needed an edit, R1 was implemented wrong.

### Secrets

- [ ] T041 [US5] Delete the connection exemption in `referencedEnvNames` (`internal/ir/validate.go:1241`) — the comment "Connection env names are exempt: they are declared in their own file" is the exemption — and add two loops noting connection `environment:` values and `destinations` values with their sites, per research R4.
- [ ] T042 [US5] Confirm the resulting report warns and exits 0, matching `docs/SCHEMA.md` §4.12's existing rule for every environment name (FR-005e). Do not add a telephony-specific severity.
- [ ] T042a [P] [US5] Add a test asserting the FR-005c boundary holds: a package is never required to declare `REDIS_URL`, `LIVEKIT_URL`, or `PIPECAT_CLOUD_ORGANIZATION` in `secrets:`. Without it, nothing fails when driver-supplied names leak into the cross-check and every phone package starts carrying boilerplate (analysis F8).
- [ ] T042b [US5] Add a test asserting SC-008 directly: across the five telephony examples, zero author-written environment names are missing from `secrets:`. The underlying check only warns, and a warning is easy to stop reading (analysis F9).
- [ ] T042c [US5] Append the dated, numbered `docs/SCHEMA.md` exception FR-005i requires, stating the cost of writing one environment name in two files with only a warning-level agreement. Same shape as the tool `output` exception (N22). This is the constitutional resolution of the Principle III deviation; recording it only in `plan.md` leaves it where the next reader will not look (analysis F2).
- [ ] T043 [P] [US5] Add `secrets:` blocks to `examples/livekit-human-transfer/agent.yaml` and `examples/pipecat-human-transfer-twilio/agent.yaml`, which have none, covering their connection environment names, destination names, and model keys.
- [ ] T044 [P] [US5] Add a `secrets:` block to `examples/pipecat-human-transfer-daily/agent.yaml` covering its destination name and model keys. Its connection declares no environment at all.
- [ ] T045 [US5] Extend the existing `secrets:` blocks in `examples/twilio-telephony-hello/agent.yaml` (7 names) and `examples/outbound-reminder/agent.yaml` (3 names) per `contracts/environment.md`.
- [ ] T046 [US5] Regenerate the two goldens that legitimately change — `.env.example` for `livekit-human-transfer` and `pipecat-human-transfer-twilio`, which switch to the labelled two-list form because those packages gained a `secrets:` block. Review the diff against T003's capture rather than running `-update` blind. **Any third golden that moves is a bug.**

### Documentation of environment names

- [ ] T047 [US5] Add the check FR-027a describes to `internal/generate/examples_test.go`, reusing the shape of `TestV11_TransfersDocListsEveryRequiredEnv`: every name in a telephony example's generated `.env.example` must appear in that example's `README.md` and in `docs/user/learn/07-phone-calls.md`. One-way only — it must never fail on a name the page mentions and `.env.example` does not.
- [ ] T048 [P] [US5] Scope the new check to the five telephony examples (FR-005f0) and say so in a comment, naming the four examples with no README as the reason it is not universal, so the narrowing reads as a decision.
- [ ] T049 [P] [US5] Document every `.env.example` name in each of the five example READMEs, following the table in `examples/pipecat-human-transfer-daily/README.md` as the model.
- [ ] T050 [US5] Delete the paragraph in `examples/twilio-telephony-hello/README.md` naming `UNMUTE_OUTBOUND_TOKEN`, `UNMUTE_PUBLIC_URL`, and `REDIS_URL` as variables the reader does not set (FR-005h), and any equivalent paragraph in the other four.
- [ ] T051 [US5] Append the second `docs/SCHEMA.md` amendment: destinations' new home and narrowed value form, and the removal of the `secrets:` exemption in §4.12. It MUST record the dropped per-target destination override as a **removed capability**, not as tidying (FR-004b). Merge with T029's amendment if both land in one commit.
- [ ] T052 [P] [US5] Update `docs/user/reference/agent-yaml.md` for `destinations` in its new home, and `docs/user/reference/secrets.md` for the removed exemption and the warn-not-fail severity.

**Checkpoint**: US5 complete. One block answers "what does this package need to
run", and the docs account for exactly the names `.env.example` lists.

---

## Phase 6: User Story 3 — Get told exactly what is wrong with a route (P2)

**Goal**: Each mistake in a connection produces one message naming the file, the
key, and the accepted values or the fix.

**Independent Test**: Break one thing at a time in a scratch copy of an example
and walk the table in `quickstart.md` §4. Every row produces the message in
`contracts/errors.md`.

**Note**: several of these already work and need only their message improved or a
test added. The tasks say which.

- [ ] T053 [P] [US3] Add the accepted-set and provider-supported-routes text to the unsupported-route failure per `contracts/errors.md` §4, reading the list from `SelectableTelephonyRoutes()` so no Exotel row is ever suggested (FR-011a).
- [ ] T054 [US3] Add the conditional-requiredness wording to `validateTelephonyEnvironment` in `internal/ir/build.go:885-915` — where a key is required only because the package places or redirects calls, the message says so (FR-012, `contracts/errors.md` §5). The check itself needs no structural change (research R3).
- [ ] T055 [US3] Validate connection environment **values** as shell identifiers in `internal/ir/build.go`, with the message in `contracts/errors.md` §6 including why a leading digit fails silently at export (FR-013).
- [ ] T056 [US3] Add the connection-list text to the unknown-connection failure at `internal/ir/build.go:782-785` (FR-014, `contracts/errors.md` §7).
- [ ] T057 [US3] Widen the connection-nothing-uses guard at `internal/ir/build.go:809` so a connection is used by a telephony channel **or** a control that dials, and the message says which of the two is missing (FR-016). `packagePlacesCalls` at `build.go:875` already reads controls the right way and is the model.
- [ ] T058 [US3] Add the connection-no-target-names warning: stderr, exit 0, checked across every declared target rather than only the `--target` selection (FR-015, `contracts/errors.md` §7).
- [ ] T059 [US3] Improve the warm-transfer refusal in `internal/ir/validate.go:1121-1131` to name the connection, the transport that connection declares, and the transport a warm transfer requires (FR-016a, `contracts/errors.md` §8). This is the message that gained information from the new shape.
- [ ] T060 [P] [US3] Add table-driven tests in `internal/ir/build_test.go` and `internal/ir/validate_test.go` covering every row of `quickstart.md` §4, asserting the facts each message carries rather than exact strings.
- [ ] T061 [P] [US3] Assert severity explicitly in those tests: six failures exit 1 with nothing written; the two warnings exit 0 (`data-model.md` §4).
- [ ] T062 [US3] Create `docs/user/reference/connections.md`: the file shape, the three valid shapes, per-route environment keys, which keys are conditional on placing calls, and per-route transfer support (FR-023, FR-023a).
- [ ] T063 [US3] Add the where-does-each-value-go table from `contracts/environment.md` to that page (FR-023b), and add the page to `docs/user/_sidebar.md` — research R8 flags that a new page missing from the sidebar is a page with no door.

**Checkpoint**: US3 complete. Every refusal names the fix.

---

## Phase 7: User Story 4 — Scaffold and console produce the new shape (P3)

**Goal**: `unmute init` and the interactive console write packages their own
validator accepts, and offer the route as one choice.

**Independent Test**: Scaffold a telephony package for each route offered and run
`unmute validate` on it unchanged. All pass.

- [ ] T064 [US4] Update `internal/scaffold/scaffold.go` to write the new shape: a connection file declaring the route, a target naming it, and destinations in `agent.yaml`. `Data.Transport`/`Data.Carrier` at `scaffold.go:46-47` and the `daily-sip` branches at `:249-257` and `:428` are the sites.
- [ ] T065 [US4] Replace the two free-text route inputs in `internal/tui/tui.go:2127-2190` with a single route picker reading `SelectableTelephonyRoutes()` (FR-026, FR-011a). Do not build a second list.
- [ ] T066 [US4] Update `internal/tui/maintain.go:134-135`, which reads `Transport` and `Carrier` off a target, to read them from the target's connection.
- [ ] T067 [P] [US4] Update the external-setup summary at `internal/tui/tui.go:467` and the destinations helper text at `:2208`, which says destinations are references in `targets.yaml`.
- [ ] T068 [P] [US4] Add a test asserting a scaffolded telephony package validates unchanged, for every route the picker offers.
- [ ] T069 [P] [US4] Confirm the TUI picker agrees with the capability table, per the existing "TUI pickers against capability table" agreement test the constitution requires stay green.

**Checkpoint**: US4 complete. The generators write what the validator accepts.

---

## Phase 8: Polish & cross-cutting

- [ ] T070 Rewrite `docs/user/learn/07-phone-calls.md` as the end-to-end path: pick a route, write the connection, declare the names in `secrets:`, set the destinations, learn which transfers the route supports, test in a browser before testing on a phone (FR-024a). It links out to `docs/user/reference/connections.md` rather than repeating it. No sixth page is added.
- [ ] T071 [P] Re-read every example README end to end against its package. Prose rots and no test holds it; CLAUDE.md requires reading the example page before claiming the work is done.
- [ ] T072 [P] Confirm `TestExampleAndDocLinksIntoExamplesResolve` still passes after the new page and the `outbound-reminder` connection rename.
- [ ] T073 Run the full `quickstart.md` §1–§5 and record the result. §1's "exactly two goldens changed" is the acceptance check for research R5.
- [ ] T074 [P] Run `make smoke` (needs Python). It should be unaffected, because the resolved IR did not change and no template input moved — which is what makes running it worth the minute.
- [ ] T075 Verify `grep -rn "transport:\|carrier:\|destinations:" examples/*/targets.yaml` and `grep -rn "^kind:" examples/*/connections/*.yaml` both print nothing (SC-003).
- [ ] T076 [P] Grep `internal/spec/` and `internal/ir/` for any path that still reports a bare "unknown field" for a field this feature moved, per Principle II's rule that a moved field must name its new form.
- [ ] T077 Place one live call per route before claiming a route works, per `quickstart.md` §7. This feature changes no route behaviour, so a failure is either pre-existing or a migration mistake, and the second is worth finding before merge. No `provisional` tag moves.

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies. T002 is a gate — if the `json:"-"` technique fails, the moved-field rejection needs re-planning before any code is written.
- **Foundational (Phase 2)**: depends on Setup. **Blocks every user story.**
- **US1 (Phase 3)**: depends on Foundational. Blocks nothing, but see below.
- **US2 (Phase 4)**: depends on Foundational only. Genuinely independent of US1 — it tests behaviour that exists today.
- **US5 (Phase 5)**: depends on Foundational only. Independent of US1: destinations and secrets are a different axis from the route move.
- **US3 (Phase 6)**: depends on **US1**, not merely on Foundational. Its messages describe the new shape, so they cannot be written before the shape exists. This is the one real cross-story dependency.
- **US4 (Phase 7)**: depends on **US1** for the same reason — a scaffold cannot write a shape that does not load yet.
- **Polish (Phase 8)**: depends on all stories you intend to ship.

### Story dependency graph

```text
Setup → Foundational ─┬─► US1 (P1, MVP) ─┬─► US3 (P2)
                      │                  └─► US4 (P3)
                      ├─► US2 (P1)
                      └─► US5 (P2)
```

### Within US1

Examples **and fixtures** first (T010–T014d), because both shapes load at that
point and migrating while the old shape still works is what keeps the tree green.
T014c is the one that gates the rest: those test files stop compiling the moment
T015 lands. Then removal (T015–T019), then the guard collapse (T020–T022), then
the route check (T023), then delete the transition (T024). Tests and docs (T025–T031) land with the code
they describe, per CLAUDE.md's three-places rule.

### Parallel opportunities

- **Phase 1**: T003 runs alongside T001–T002.
- **Phase 2**: T008 and T009 are independent of T004–T007.
- **US1 example migration**: T010, T011, T012 are three different packages and run in parallel. T013 and T014 are sequential within their own packages only.
- **US1 docs**: T027, T028, T030, T031 touch four different files.
- **US2**: T032, T033, T035 are independent.
- **US5**: T043, T044 are different packages; T039, T048, T049, T052 are different files.
- **US3**: T053, T060, T061 are independent of each other.
- **Cross-story**: with more than one person, US2 and US5 proceed alongside US1 after Foundational.

---

## Parallel Example: User Story 1 migration

```bash
# Three example packages, three people, no shared file:
Task: "Migrate examples/twilio-telephony-hello (T010)"
Task: "Migrate examples/livekit-human-transfer (T011)"
Task: "Migrate examples/pipecat-human-transfer-twilio (T012)"

# Then the two that are more than a field move:
Task: "Split outbound-reminder's shared connection into two files (T013)"
Task: "Give pipecat-human-transfer-daily its first connections/ directory (T014)"
```

---

## Implementation Strategy

### MVP: Setup + Foundational + US1

Stops at a coherent, shippable state: the authoring contract is simplified, every
example **and both fixtures** are migrated, the old shape fails with a message
naming the fix, and the SCHEMA amendment records it. Destinations still sit on targets and `secrets:`
still exempts telephony names — both are honest, documented, and unchanged from
today.

### Recommended order after MVP

1. **US2** next, despite being listed after US1. It is four tasks, it guards the
   most common workflow in the project, and it costs nothing to carry.
2. **US5**, the second-largest chunk and the one that answers the question that
   grew this feature.
3. **US3**, which needs US1 and turns the refusals into instructions.
4. **US4**, lowest priority: the generators write a shape that already works by
   hand.

### Three places to slow down

- **T015a** — research R11, analysis F1. Removing `carrier` from targets and
  leaving the four capability rows conditioned on it fails `safe_core` with a fix
  no author can perform. The two halves land together or not at all. It is also
  the one place this feature touches the capability rulebook, so it deserves the
  extra minute.
- **T023** — research R10. The carrier-less Daily route has no row in the
  capability table, so a flat triple lookup breaks
  `pipecat-human-transfer-daily` with a message that reads like a broken example.
- **T046** — research R5. Exactly two goldens change, and both change because a
  package gained a `secrets:` block, not because the route moved. A third moving
  golden means the resolved IR shifted, which the whole design exists to prevent.
  T014d is the earlier, sharper version of the same check: `compiler.txt` stays
  byte-identical.

---

## Notes

- Tasks added by `/speckit-analyze` remediation on 2026-08-14 carry letter
  suffixes (T014a–T014d, T015a, T015b, T036a, T042a–T042c) so the original
  numbering and every cross-reference in `plan.md` and `contracts/` stay valid.
  They are in execution order within their phase; the suffix is not a lower
  priority.
- `[P]` means different files and no dependency on incomplete work.
- Commit after each task or logical group; every commit should leave `make test` green, which is what the Phase 2 transitional fallback buys.
- The resolved IR shape does not change. If a generator, a `cli/dev*` file, or a golden other than the two in T046 needs an edit, stop: research R1 was implemented wrong and the blast radius is about to grow by twenty files.
- Two warnings exist in this feature (unused connection file, name missing from `secrets:`). Everything else is a hard error before any artifact is written.
