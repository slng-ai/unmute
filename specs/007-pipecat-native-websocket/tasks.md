---

description: "Task list for Pipecat native WebSocket telephony (zero hosted infrastructure)"
---

# Tasks: Pipecat native WebSocket telephony (zero hosted infrastructure)

**Input**: Design documents from `/specs/007-pipecat-native-websocket/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Included, and not optional here. The constitution mandates the agreement tests (emitter against capability table) and golden reads, and [quickstart.md](quickstart.md) enumerates the assertions the offline half must make. Every test task names the assertion, not just the file, so no task can be closed by an empty test.

**Organization**: Grouped by user story. US1 alone is a shippable increment.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1 to US5, mapping to the user stories in [spec.md](spec.md)
- Every task names its file path

## Path Conventions

Single Go module, `internal/` layout. Emitted Python lives only in
`internal/generate/templates/pipecat_v1/*.tmpl`. Examples are source packages
under `examples/`; their `build/` directories are disposable output.

---

## Phase 1: Setup and baseline

**Purpose**: capture the before state, so every later "byte-identical" claim is measured rather than asserted.

- [X] T001 Record the pre-feature baseline: compile every example under `examples/` into a scratch copy, keep the output for the diff at T069, and note the command used under Phase 1 findings in `specs/007-pipecat-native-websocket/tasks.md`
- [X] T002 [P] Confirm the three grounding sources are reachable and note their titles for the emitted links (pipecat `twilio-chatbot` example, Twilio Media Streams WebSocket messages, Twilio TwiML Bins) in [research.md](research.md) as F13
- [X] T002a [P] Close research F14 in [research.md](research.md): verify against Twilio's own documentation, dated, whether the outbound `From` must be a number owned in the account or may be a verified caller id, the destination-country permission model, and any restriction on calling unverified destinations. Until this lands, none of the three may appear in emitted text (spec FR-006a)
- [X] T003 [P] Verify the runner's local Twilio mode contract against the installed `pipecat-ai==1.5.0` (`-t twilio -x <host>`, the TwiML it answers with, the `/ws` path) and record the exact flag spellings in [research.md](research.md) beside F12

**Checkpoint**: the baseline exists and every platform fact this feature rests on is dated.

---

## Phase 2: Foundational (blocking prerequisites)

**Purpose**: the route has to exist in the one rulebook, resolve through the IR, and be a legal thing to write down, before any emitter work can be true.

**⚠️ CRITICAL**: no user story work starts until this phase is complete.

- [X] T004 Add the route row `(pipecat, "cloud-websocket", "twilio")` to `internal/target/telephony.go` with docs URL, the five features (route selected, inbound, outbound, cold transfer, hangup) all `provisional` carrying the note "built and offline-proven; no call has been placed through this endpoint yet", `RequiredEnvironment` of `account_sid`/`auth_token`/`from_number`, empty `Processes`, empty `PublicEndpoints`, and `RuntimeEnvironment` naming `PIPECAT_CLOUD_ORGANIZATION`
- [X] T005 Extend `ResolveTelephonyFeature` in `internal/target/telephony.go` so a warm transfer on this route refuses with a message naming what it would take (a hosted callback endpoint), and a `source.*` call source refuses naming where those do work
- [X] T006 [P] Add `TestPipecatCloudWebsocketRouteRow` to `internal/target/telephony_test.go` asserting the row grants exactly the five features, every one provisional, and declares no process and no public endpoint (the row's defining difference from the Daily carrier row)
- [X] T007 [P] Add the transport value to the authoring surface in `internal/spec` and assert in `internal/spec/authoring_surface_test.go` that the route needs no new authoring field
- [X] T008 Resolve the route in `internal/ir/build.go`: accept `transport: cloud-websocket` with `carrier: twilio`, set the telephony plan's services to `["application"]` with a comment saying the application is the agent and the platform hosts it, and require no coordination reason beyond `admission`
- [X] T009 Add the mutual-requirement guards to `internal/ir/build.go`: outbound or a cold transfer without a connection fails naming `account_sid`, `auth_token`, and `from_number` and why each is needed; a connection with no phone channel and no transfer fails as dead weight; a pure-inbound package with no connection is valid
- [X] T010 Refuse the three SIP connection keys on this route in `internal/ir/build.go`, with a message naming the key, the route, and the accepted set
- [X] T011 [P] Add `internal/ir/validate.go` expectations for the route: application service only, no Redis, coordination limited to `admission`
- [X] T012 [P] Add build tests to `internal/ir/build_test.go` covering all four rows of the guard table in [data-model.md](data-model.md) §2 plus the SIP-key refusal, each asserting the message names its fields
- [X] T013 [P] Add validate tests to `internal/ir/validate_test.go` for the route's service and coordination expectations
- [X] T014 Write SCHEMA amendment N38 in `docs/SCHEMA.md`: dated, numbered, stating the new `transport` value, its connection key set, that the three SIP keys are refused on it, that a pure-inbound package needs no connection at all, and whether existing packages fail strict decode (they do not)

**Checkpoint**: `go test ./...` is green, the route is declarable, and nothing is emitted for it yet.

---

## Phase 3: User Story 1 - Inbound with nothing hosted (Priority: P1) 🎯 MVP

**Goal**: an operator's Twilio number reaches the deployed agent, with zero operator-hosted infrastructure.

**Independent Test**: compile the new example, deploy, do the dictated console actions, call the number. Offline: the build has no process artifact and the production runbook names no tunnel and no hosting.

### Tests for User Story 1

- [X] T015 [P] [US1] Create `internal/generate/pipecat_cloud_websocket_test.go` with `TestCloudWebsocketEmitsNoProcessArtifact`, asserting the build's file list equals the plain Pipecat Cloud build's list exactly (no helper, no compose telephony file, no new file of any kind)
- [X] T016 [P] [US1] Add `TestCloudWebsocketManifestDeclaresWebsocketAuth` to `internal/generate/pipecat_cloud_websocket_test.go`, asserting `pcc-deploy.toml` carries `websocket_auth = "none"` on this route and that no other route's manifest gains the line
- [X] T017 [P] [US1] Add `TestCloudWebsocketBinMarkupIsDictated` to `internal/generate/pipecat_cloud_websocket_test.go`, asserting the README's Bin carries the spoken line before `<Connect>`, the compiled agent name, the `_pipecatCloudServiceHost` parameter, and both `{{From}}`/`{{To}}` substitutions
- [X] T018 [P] [US1] Add `TestCloudWebsocketRegionPicksTheEndpoint` to `internal/generate/pipecat_cloud_websocket_test.go`, asserting a declared region renders the regional wss host in every place the host appears, and no region renders the default host
- [X] T019 [P] [US1] Add `TestCloudWebsocketRunbookContract` to `internal/generate/pipecat_cloud_websocket_test.go`, covering [contracts/runbook.md](contracts/runbook.md): the stated counts match the numbered list that follows them (spec SC-001), the three grounding links present, the trunk warning present, the security note naming `websocket_auth` and the capability sentence, the caller-identity definition present wherever the number is asked for (spec FR-006a), and the words "ngrok" and "helper" absent from the whole section
- [X] T019a [P] [US1] Add `TestCloudWebsocketEmitsNoSecretLiterals` to `internal/generate/pipecat_cloud_websocket_test.go`, asserting no secret-looking literal (`AC0`, `sk-`, `pk_`, an E.164 destination) appears in any emitted file or in the README for this route, so FR-011 has a guard on this route as it had on 006's
- [X] T019b [P] [US1] Add `TestCloudWebsocketPureInboundAsksForNothing` to `internal/generate/pipecat_cloud_websocket_test.go`, asserting a fixture declaring only `channels.phone.inbound` with no connection emits an `.env.example` with no carrier names and a `compile-report.json` with no carrier environment, which is the emitted half of FR-005 that the IR tests cannot see

### Implementation for User Story 1

- [X] T020 [US1] Add the `pipecatCloudWebsocket` data group to `internal/generate/pipecat_v1.go` as a third field beside `Telephony` and `DailyCarrier`, with the comment explaining why it is separate rather than a widening (the same quiet-failure risk 006 recorded)
- [X] T021 [US1] Add `buildPipecatCloudWebsocket` to `internal/generate/pipecat_v1_build.go`: refuse a non-twilio carrier by name, share the connection key-set round trip with the other routes, populate the group per [data-model.md](data-model.md) §3, and set `CallEnv` to exactly the names a phone call adds
- [X] T022 [US1] Add one template helper for the wss host in `internal/generate/pipecat_v1_build.go` (regional when a region is declared) and use it everywhere the host is rendered, so the Bin, the outbound command, and the transfer markup cannot disagree
- [X] T023 [US1] Emit the `"twilio"` entry in `transport_params` in `internal/generate/templates/pipecat_v1/bot.py.tmpl`, using `FastAPIWebsocketParams` and leaving the serializer to the runner
- [X] T024 [US1] Add phone-session detection to `internal/generate/templates/pipecat_v1/bot.py.tmpl`, reading `runner_args.transport_type` and `runner_args.call_data` after `create_transport` per [data-model.md](data-model.md) §5, returning None for browser and console sessions
- [X] T025 [US1] Add the per-call environment check to `internal/generate/templates/pipecat_v1/bot.py.tmpl`, running only on a phone session, naming every missing value, and never read by browser or console sessions
- [X] T026 [US1] Add `websocket_auth = "none"` to `internal/generate/templates/pipecat_v1/pcc-deploy.toml.tmpl` on this route only, with the comment recording research F7's docs contradiction and why the value is explicit
- [X] T027 [US1] Add the `websocket` extra to `internal/generate/templates/pipecat_v1/pyproject.toml.tmpl` on this route
- [X] T028 [US1] Write the route's Telephony setup section in `internal/generate/templates/pipecat_v1/README.md.tmpl` per [contracts/runbook.md](contracts/runbook.md) parts zero to two: who does what, the counts, the carrier actions with console paths, the org lookup command, the Bin markup, the trunk warning, and the "nothing, in production" line
- [X] T029 [US1] Add the security note (runbook part five) and the troubleshooting map (part seven) to `internal/generate/templates/pipecat_v1/README.md.tmpl`, one row per named cause from [spec.md](spec.md) Edge Cases
- [X] T030 [US1] Add the three grounding source links to `internal/generate/templates/pipecat_v1/README.md.tmpl` where the setup is dictated
- [X] T031 [US1] Create `examples/human-transfer-cloud-twilio/` (`targets.yaml`, `agent.yaml`, `instructions.md`, `README.md`, `connections/twilio_voice.yaml`) declaring this route with inbound, reusing `telephony-hello`'s connection names verbatim, with a greeting that names its line so it is distinguishable by ear from the other examples
- [X] T032 [US1] Register the new example in `internal/generate/examples_test.go` with markers proving the route's shape (no process artifact, the Bin present) and confirm `internal/generate/examples_lint_test.go` lints its emitted Python
- [X] T033 [US1] Extend `internal/generate/telephony_agreement_test.go` so the new row's every granted feature has an emitted path, and a row with no processes produces a build with no process artifact
- [X] T034 [US1] Regenerate the Pipecat goldens with `go test ./internal/generate -run TestPipecat -update-pipecat`, read every diff, and record the enumerated diffs under Phase 3 findings in `specs/007-pipecat-native-websocket/tasks.md`

**Checkpoint**: the offline half of [quickstart.md](quickstart.md) steps 1 to 6 passes, and inbound is deployable and callable.

---

## Phase 4: User Story 2 - Outbound through your own number (Priority: P2)

**Goal**: one command places a call from the operator's own number and the agent speaks on answer.

**Independent Test**: run the dictated command with a reachable phone. Offline: the command exists exactly when outbound is declared and names no endpoint of the operator's own.

### Tests for User Story 2

- [X] T035 [P] [US2] Add `TestCloudWebsocketOutboundIsPlacedAtTheCarrier` to `internal/generate/pipecat_cloud_websocket_test.go`, asserting the README's command posts to Twilio (not to the platform), carries `direction=outbound` in the inline TwiML, reads `From` and the organization from the environment, and leaves only the destination to be typed
- [X] T036 [P] [US2] Add `TestCloudWebsocketOutboundIsAbsentWhenUndeclared` to `internal/generate/pipecat_cloud_websocket_test.go`, asserting a package without outbound gets neither the command nor any outbound code path

### Implementation for User Story 2

- [X] T037 [US2] Emit the outbound section in `internal/generate/templates/pipecat_v1/README.md.tmpl` per [contracts/carrier-markup.md](contracts/carrier-markup.md) §2 and [contracts/runbook.md](contracts/runbook.md) part three, gated on the outbound declaration
- [X] T038 [US2] Handle the outbound session in `internal/generate/templates/pipecat_v1/bot.py.tmpl`: greet on connect (which fires on answer, so the greeting meets a person), with the comment recording why no ringback handling is needed
- [X] T039 [US2] Add the connection and organization names to `internal/generate/templates/pipecat_v1/env.example.tmpl`, grouped and commented, emitted only when the declaration requires them
- [X] T040 [US2] Add outbound to `examples/human-transfer-cloud-twilio/agent.yaml` and its connection to `targets.yaml`, and note in the example README why outbound is declared (it is how a transfer is tested without waiting for a caller)
- [X] T041 [US2] Regenerate goldens in `internal/generate/testdata/golden/`, read every diff, and record them under Phase 4 findings in `specs/007-pipecat-native-websocket/tasks.md`

**Checkpoint**: US1 and US2 both work; outbound needs no hosted trigger.

---

## Phase 5: User Story 3 - Cold transfer to a human (Priority: P2)

**Goal**: the caller reaches a person through the operator's own carrier account, and a failed transfer never leaves the caller stranded silently.

**Independent Test**: on a live inbound call, ask for the person; then repeat with the destination declining. Offline: the transfer dials an environment name, never a literal, and the failure verbs follow the dial.

### Tests for User Story 3

- [X] T042 [P] [US3] Add `TestCloudWebsocketTransferUpdatesTheLiveCall` to `internal/generate/pipecat_cloud_websocket_test.go`, asserting the emitted transfer announces first, then updates the call by its id, and composes `<Dial answerOnBridge="true">` around a destination read from the environment
- [X] T043 [P] [US3] Add `TestCloudWebsocketTransferFailurePathIsSequential` to `internal/generate/pipecat_cloud_websocket_test.go`, asserting the spoken failure line and the reconnect stream follow the `<Dial>` in the same markup, and that the reconnect names the same service host as the Bin
- [X] T044 [P] [US3] Add `TestCloudWebsocketTransferHonestyIsWritten` to `internal/generate/pipecat_cloud_websocket_test.go`, asserting the emitted README states the fresh-session limit and the destination-hangs-up-first case when a transfer is declared
- [X] T045 [P] [US3] Add a warm-transfer refusal test to `internal/generate/pipecat_cloud_websocket_test.go`, asserting the message says which thing it means and does not claim the platform cannot do it

### Implementation for User Story 3

- [X] T046 [US3] Emit the cold-transfer tool body for this route in `internal/generate/templates/pipecat_v1/bot.py.tmpl`: announce, compose the markup from [contracts/carrier-markup.md](contracts/carrier-markup.md) §3, one REST update keyed on the call id, and the `_TRANSFER_RESULT` guard reused from 006 so a second request replays rather than re-fires
- [X] T047 [US3] Emit the transfer honesty paragraph in `internal/generate/templates/pipecat_v1/README.md.tmpl` per [contracts/runbook.md](contracts/runbook.md) part six, linking the route comparison
- [X] T048 [US3] Add the cold transfer to `examples/human-transfer-cloud-twilio/agent.yaml` and `instructions.md`, with the destination as an environment name and prompt text that never reads a number aloud
- [X] T049 [US3] Regenerate goldens in `internal/generate/testdata/golden/`, read every diff, and record them under Phase 5 findings in `specs/007-pipecat-native-websocket/tasks.md`

**Checkpoint**: all three call flows are emitted; the route is feature-complete offline.

---

## Phase 6: User Story 4 - Picking the right route, and the example set (Priority: P3)

**Goal**: an author can tell which Twilio route to use, and the shipped examples are one per use case.

**Independent Test**: mostly offline. The comparison exists, the example set matches [spec.md](spec.md) FR-016, and a stale declaration fails by name. One part is not offline and cannot be: `telephony-hello`'s audit closes only when the operator confirms the deployed agent works (FR-016a, T071f).

### Tests for User Story 4

- [X] T050 [P] [US4] Add `TestCloudWebsocketSeamIsWordsNotShapes` to `internal/generate/pipecat_cloud_websocket_test.go`, asserting this route's work reaches no other target's output and that carrier-specific console paths stay in the carrier half of the runbook
- [X] T051 [P] [US4] Add a docs agreement test asserting the route comparison names all three Twilio routes on the Pipecat target and states what each hosts, in `internal/generate/pipecat_cloud_websocket_test.go` or the nearest docs-checking test

### Implementation for User Story 4

- [X] T052 [US4] Move `examples/telephony-hello`'s pipecat target from `carrier-websocket` to `cloud-websocket` in `examples/telephony-hello/targets.yaml`, keeping the LiveKit target and the shared `.env` untouched
- [X] T053 [US4] Audit the offline half of `examples/telephony-hello` per spec FR-016a: its route declaration, its README claims, its connection comment, its channel declaration, and the accuracy of its LiveKit half, fixing whatever the route move made stale. This half does not close the audit on its own
- [X] T054 [US4] Delete `examples/human-transfer-daily-twilio/` and remove it from `internal/generate/examples_test.go`, replacing its Daily-carrier coverage with a test fixture so the 006 route keeps its guards without shipping an example
- [X] T055 [US4] Record in `specs/006-pipecat-carrier-telephony/tasks.md` that the route lost its public example, why, and that its open live tasks now run against a fixture, and add a dated note at the top of `specs/006-pipecat-carrier-telephony/quickstart.md` saying its step 1 commands no longer resolve and naming the fixture that replaces the example. Leave 006's `plan.md` and `research.md` as written: they are the history of a completed feature, not instructions
- [X] T056 [P] [US4] Write the three-Twilio-routes comparison in `docs/TELEPHONY.md`: what each hosts, what each needs from the carrier account, what transfers can do on each, and a plain recommendation for the common case
- [X] T057 [P] [US4] Update `docs/user/targets/pipecat.md` with the route, its artifact notes, and the fact that no Pipecat route offers warm transfer today
- [X] T058 [P] [US4] Update `docs/user/learn/07-phone-calls.md` with the authoring-facing route choice and the reorganized example set
- [X] T059 [P] [US4] Update `docs/TRANSFERS.md`: add this route's transfer semantics with the fresh-session limit stated plainly beside the Daily route's behaviour, and repair the two references to the deleted example (the package list around line 284 and the environment section around line 334), replacing them with the new example and this route's single-group environment
- [X] T060 [P] [US4] Update `examples/README.md` so the telephony examples read as one per use case, naming which target each uses, and fix two entries that are wrong or about to be: `human-transfer` is described as having a Pipecat target when its `targets.yaml` declares only LiveKit (already stale today), and `telephony-hello` is described as carrier-websocket, which T052 changes

**Checkpoint**: the example set and the docs tell one story.

---

## Phase 7: User Story 5 - Hearing the phone path before deploying (Priority: P3)

**Goal**: `unmute dev --telephony` runs the whole phone path locally with one command, and gives the number back.

**Independent Test**: run it, call the number, stop the session (including with an interrupt), read the number's configuration back and confirm it is what it was.

### Tests for User Story 5

- [X] T061 [P] [US5] Add dev tests to `internal/cli/dev_test.go` asserting `--telephony` on this route reaches the local flow rather than a refusal, and that it refuses by name when carrier credentials are absent
- [X] T062 [P] [US5] Add a test to `internal/cli/dev_telephony_test.go` asserting the number restore runs on every exit path, interrupt included, and that no carrier markup is created locally

### Implementation for User Story 5

- [X] T063 [US5] Replace the Daily-route refusal branch in `internal/cli/dev.go` with the local flow for this route, keeping the Daily and carrier-websocket messages exactly as they are
- [X] T064 [US5] Orchestrate the local run in `internal/cli/dev_telephony.go` (or a new `internal/cli/dev_cloud_websocket.go`): start the compiled agent locally in the runner's Twilio mode with the tunnel host, reusing `startQuickTunnel` from `internal/cli/dev_tunnel.go` and `autoConfigureCarrierWebhook` from `internal/cli/dev_twilio.go` for the set-and-restore pair
- [X] T065 [US5] Print the session's facts once in `internal/cli/dev_telephony.go`: the local agent's port, the tunnel address, the number borrowed, and the line saying it will be restored on exit
- [X] T066 [US5] Add the local development subsection to `internal/generate/templates/pipecat_v1/README.md.tmpl` per [contracts/runbook.md](contracts/runbook.md) part two and a half, naming cloudflared, never ngrok, and stating that the production Bin is untouched
- [X] T067 [US5] Extend `TestCloudWebsocketRunbookContract` in `internal/generate/pipecat_cloud_websocket_test.go` so the tunnel words are forbidden in the production parts only, and required in the local development subsection

**Checkpoint**: an author can hear the phone path without deploying, and the number survives it.

---

## Phase 8: Polish, the gate, and the live runs

- [X] T068 [P] Run `make fmt`, `make lint`, and `go test ./...`, and confirm ruff is clean on every emitted example via `go test ./internal/generate -run TestPublicExamplesEmitLintCleanPython`
- [X] T069 Diff every example's build output against T001's baseline, confirm every untouched route is byte-identical, and list the deliberate changes (the new `examples/human-transfer-cloud-twilio`, the `examples/telephony-hello` target move, the deleted `examples/human-transfer-daily-twilio`) under Phase 8 findings in `specs/007-pipecat-native-websocket/tasks.md`
- [X] T070 [P] Prove the emitted image end to end with Docker per [quickstart.md](quickstart.md) offline step 6 and record the result
- [ ] T071 Run the live half of [quickstart.md](quickstart.md) and record each result, dated, in the Live Run Record below
- [ ] T071f Close the `examples/telephony-hello` audit (spec FR-016a, the live half): deploy it to Pipecat Cloud and have the operator confirm a real inbound call and a real outbound call work, recording their dated confirmation in the Live Run Record below. The example is not audited until they say the deployed agent works
- [ ] T072 Lift `provisional` in `internal/target/telephony.go` only for the capabilities the live runs actually proved, and update the note on the rest to say what is still missing
- [ ] T073 Update the route comparison and `docs/TRANSFERS.md` status text so no document claims more than the Live Run Record shows

---

## Live Run Record

Fill in as the live tasks complete. A capability row may not lose `provisional` without a dated line here.

| Task | Flow | Date | Outcome | Notes |
|---|---|---|---|---|
| T071a | Inbound answered on a Twilio number | 2026-08-13 | **pass, with a caveat** | Spoken line immediate (the carrier says it). Greeting: `First bot speech latency=2.190s` inside the process, but 10 to 15s wall-clock on a **cold** container, so SC-002's 10s bound holds only with a warm instance. Three findings came out of this run: the organization slug, the pipeline sample rates, and cold start (see Findings) |
| T071b | Outbound rings through the operator's number | | | what caller identity the recipient saw |
| T071c | Cold transfer completes, and the decline drill | 2026-08-13 | **completed transfer: pass.** Decline drill: not yet run | `human transfer fired: send_to_billing (cold)`, the carrier update measured at **0.157s** in the platform's own latency breakdown, `{"transferred": true}` returned to the model, and the session wound down as Twilio applied the new document. Counts 1 of 5 against SC-004; the decline drill and the fresh-session limit are still unobserved |
| T071d | Failure mapping drill | | | which troubleshooting rows the drill corrected |
| T071e | `dev --telephony` local call and number restore (SC-008) | | | the configuration read back after an interrupt, compared byte for byte to the before state |
| T071f | `telephony-hello` deployed and confirmed working (FR-016a) | | | the operator's own confirmation, inbound and outbound, on the deployed agent |

---

## Findings

Written as the phases closed, on 2026-08-13. The offline half of
[quickstart.md](quickstart.md) passes; the live half is T071 and belongs to the
operator.

### Phase 1: the baseline, and the fact that closed F14

**T001, the baseline.** Every example was copied to a scratch tree and compiled
with the pre-feature binary: 228 emitted files across ten packages. The command,
reproducible:

```sh
cp -R examples/ <scratch>/examples/ && rm -rf <scratch>/examples/*/build
go build -o <scratch>/unmute . && for d in <scratch>/examples/*/; do <scratch>/unmute compile "$d"; done
```

**T002a closed F14 in full**, which the analyze pass had flagged as blocking three
claims from emitted text. All four parts are now verified against Twilio's own
documentation and dated in [research.md](research.md): `From` must be a number
owned in the account **or a verified outgoing caller id**; verified caller ids are
a real alternative; destination-country permissions are per-account with three
risk classes; a trial account can only call verified destinations, with errors
21219, 14111, and 32100 saying so. The emitted README's outbound section and its
troubleshooting map now state all of it, which they were barred from doing before.

**T003** re-read the runner's local Twilio mode and recorded the exact flag
spellings, including the one that matters: `-x/--proxy` takes a **hostname**, not
a URL, and the webhook the runner answers is `POST /`. The dev flow passes
`public.Host` for that reason.

### Phase 3 to 5: one deliberate golden diff, and one design amendment

**T034/T041/T049, the goldens.** Both Pipecat goldens changed in exactly two
places, both comment text, both deliberate: the cold-transfer guard's comments
said "a second REFER" and "the REFER is still in flight" on a template now shared
by a route that uses no REFER. They now say "a second one" and "the first one".
No emitted code, no structure, no environment name moved.

**The transfer announcement moved into the markup**, and the contract was amended
rather than quietly diverged from
([contracts/carrier-markup.md](contracts/carrier-markup.md) §3, dated). The
original wording had the agent speak the line and then update the call. Applying
the update replaces the call's document, which tears the media stream down, so the
agent would be cut off mid-word by its own transfer, and pipecat 1.5.0 exposes no
speech-finished event to wait on. The carrier speaks the line instead: it plays
after the update and before the destination rings, which is what FR-007 asks for.

### Phase 6 and 7: the two things the example move broke, and both are fixed

Moving `examples/telephony-hello`'s Pipecat target off `carrier-websocket` broke
two tests that had used it as *the* carrier-websocket example, both repointed
rather than weakened: `TestPipecatWebDevNeedsNoTelephonyEnv` now builds the
carrier-websocket fixture directly, and `TestDailyRouteWorkDoesNotReachOtherTargets`
lost `_TRANSFER_RESULT` from its Daily-only marker list, because 007 reuses that
guard deliberately (data-model §6). Deleting the 006 example needed the same care:
`internal/cli/dev_test.go`'s refusal test now reads the new fixture
`internal/testdata/daily_carrier`.

**T064 deviated from its own wording, deliberately.** The task said to reuse
`autoConfigureCarrierWebhook`. That function derives the webhook URL from a route
endpoint, and this route has none: the local path belongs to pipecat's runner, not
to anything this repository emits. So the dev flow calls
`configureTwilioVoiceWebhook`, which is the set-and-restore primitive that
function wraps. Same carrier code, one layer down, and the comment says why.

### Phase 8: the gate, and the bug the gate found

**T068** `make fmt`, `make lint` (0 issues), `go test ./...` (all packages ok),
and the emitted-Python ruff gate all pass.

**T069, byte-identity.** Nine of the ten pre-existing examples are
**byte-identical** to the T001 baseline, compared file by file. The deliberate
changes, and nothing else:

| Change | Why |
|---|---|
| `examples/human-transfer-cloud-twilio` added | the new example (FR-016) |
| `examples/human-transfer-daily-twilio` removed | replaced by it (FR-016, D13) |
| `examples/telephony-hello` source and Pipecat build | the target move (T052) and the audit (T053). Its LiveKit build is byte-identical |
| `examples/human-transfer-daily/build/pipecat/bot.py`, two comment lines | the REFER rewording above |
| `examples/README.md` | T060 |

**T070 proved the image, and found a real bug doing it.** The emitted image
builds on `dailyco/pipecat-base:0.1.27-py3.12` and serves the platform's
contract: `/readyz` 200, `/livez` 200, `/nope` 404, `POST /bot` 200. Two facts
worth recording for the next reader:

1. **The base image listens on 8080**, not 7860. Only the port mapping in a local
   probe cares; the platform handles it.
2. **`pipecatcloud.agent.WebSocketSessionArguments` subclasses pipecat's
   `WebSocketRunnerArguments`**, verified inside the container, so
   `create_transport` accepts what the platform's `/ws` route hands the bot. This
   is the 006-style trap (`Unsupported runner arguments type`) checked rather than
   assumed.

Then the bug. Driving a **synthetic Twilio Media Streams handshake** into the
running container failed on the pure-inbound shape with
`auto_hang_up is enabled but missing required parameters: account_sid, auth_token`.
The framework's transport factory always asks the carrier serializer for REST
hangup, and that serializer refuses to be built without credentials for it. So
FR-005's "a pure-inbound package needs no carrier credentials" was, as built,
false: every call would have crashed. Recorded as [research.md](research.md) F15,
which also corrects F4's last sentence.

The fix keeps the promise instead of retreating from it: on the **no-connection
shape only**, the emitted bot builds the transport itself with
`auto_hang_up=False`, which is exactly the behaviour FR-005 specifies for that
shape (the call ends when the stream closes, because the markup has nothing after
`<Connect>`). A credentialed package is untouched and uses the framework's path;
a test asserts both directions.

Both shapes were then re-driven through the same synthetic handshake and both
reach the same point: `phone call CA0123456789abcdef (inbound)` from the emitted
session detection, then the entry agent activating. The only errors after that are
401s from the deliberately fake model keys. That is the whole inbound chain proven
offline, minus the carrier leg and minus real credentials.

### The first live calls, 2026-08-13

Recorded as they happened, because three of the four things that went wrong were
ours and none of them was visible offline.

**1. The Bin named the organization's display name.** `pipecat.nicoferdi` instead
of `pipecat.zonal-bison-orange-168`. The caller heard the carrier's spoken line and
then silence, and **the agent's log was empty for the whole call**, because the
platform refuses the connection before the agent is involved. Two runbook fixes
came out of it: the value is now described by its shape rather than by a column
heading (the CLI's headings differ from the platform docs'), and the
troubleshooting map now says that an empty agent log is itself the diagnosis. Two
suspects were cleared by checking rather than guessing: the regional endpoint
resolves and is documented, and `websocket_auth` is a top-level manifest key
exactly where we emit it.

**2. Ten to fifteen seconds of silence before the greeting was a cold start.** The
platform's own `First bot speech | latency=2.190s` covers only the part inside the
process; the rest was the container being scheduled and pipecat being imported,
because the deployment had scaled to zero. The map now explains that, and says
`--min-agents 1` ends it, and that the flag has to be passed on every deploy
because this manifest is regenerated.

**3. The pipeline was resampling every frame for nothing.** Twilio Media Streams
is 8 kHz mono both ways; pipecat defaults to 16 kHz in and 24 kHz out. Fixed by
`_pipeline_audio_rates()`, applied per session so a browser run on the same file
keeps the defaults.

**4. One thing was not ours.** A 17.6s STT time-to-first-byte in the platform's
latency breakdown, against 2.2s for the LLM and 0.34s for the TTS on the same
event loop. A later call on the same deployment showed 0.315s, so it was a cold
gateway session at the provider. The runbook now teaches reading that breakdown
and which of the three owners each line points at.

**And the cold transfer worked on its first live attempt**, which is the flow that
had the most new code behind it.

### What is not done, and why

T071, T071f, T072, and T073 are the live half. Nothing has been called through
this endpoint yet, so all five capability rows stay `provisional` with the note
"built and offline-proven; no call has been placed through this endpoint yet", and
every document says the same. A row loses `provisional` only against a dated line
in the Live Run Record above.

---

## Dependencies and Execution Order

### Phase dependencies

- **Phase 1 (setup)**: no dependencies
- **Phase 2 (foundational)**: needs Phase 1; **blocks every user story**
- **Phase 3 (US1)**: needs Phase 2. The MVP
- **Phase 4 (US2)** and **Phase 5 (US3)**: need Phase 3's data group and template scaffolding; independent of each other in principle, but both edit `bot.py.tmpl` and `README.md.tmpl`, so they serialize in practice
- **Phase 6 (US4)**: needs Phase 3 for the new example to exist before the old one is deleted
- **Phase 7 (US5)**: needs Phase 2 only for the route to resolve, so it can run beside Phases 4 to 6 (different files: `internal/cli/`)
- **Phase 8**: needs every phase whose work it checks

### The sequencing traps

Every task that regenerates goldens (T034, T041, T049, and the example moves in T052 and T054) touches the same golden files, so they never run in parallel with each other. Each reads its own diff before accepting.

T031 (the new example) must land before T054 (deleting the old one), or the repository briefly ships no carrier telephony example on Pipecat.

T004's route row must land before T033's agreement test, and the agreement test must fail first for the right reason: a granted feature with no emitted path.

### Parallel opportunities

- T002, T002a, and T003 together
- T006, T007, T011, T012, T013 together once their subjects exist
- All of US1's test tasks (T015 to T019b) together
- T056 to T060 together (different documents)
- T061 and T062 together
- Phase 7 as a whole runs beside Phases 4 to 6

---

## Implementation Strategy

### MVP first (User Story 1 only)

1. Phase 1 baseline, Phase 2 foundation
2. Phase 3 to the checkpoint
3. **Stop and validate**: one live inbound call, recorded
4. Shippable here: an operator's number reaches the agent with nothing hosted, capabilities still provisional, every other route untouched

### Incremental delivery

Phase 3 (inbound) → Phase 4 (outbound) → Phase 5 (cold transfer) → Phase 6 (the example set and the route choice) → Phase 7 (local dev) → Phase 8 (the gate and the live runs). Each phase adds a testable flow without touching the previous one's artifacts.

### Rollback confidence

The compiler change is additive behind `transport: cloud-websocket`, so every other route's build is byte-identical (T069). The example reorg is the only non-additive part, and it is deliberate, recorded, and reversible from git history.

## Notes

- [P] means different files and no dependency on an incomplete task
- Tests name their assertion, not just their file, so a task cannot be closed by an empty test
- Every platform claim written into an emitted file or a document carries its source and verification date from [research.md](research.md)
- No em or en dashes in emitted text or documents; plain wording throughout
- Commit after each task or logical group; stop at any checkpoint to validate a story on its own
