---

description: "Task list for Pipecat carrier telephony, bring your own number to the Daily route"
---

# Tasks: Pipecat carrier telephony, bring your own number to the Daily route

**Input**: Design documents from `specs/006-pipecat-carrier-telephony/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: included and mandatory. The spec asks for them (FR-018), each contract names its own contract test, and the constitution requires one runnable check behind non-trivial logic. Every test task names the assertion, not just the file.

**Organization**: by user story, in priority order. Phase 2 is genuinely blocking: the route row does not exist yet, four shipped guards refuse this route by design, and about twenty-four gating sites would mistake it for a carrier-websocket route, so nothing renders correctly until all of that is handled deliberately.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1 inbound, US2 outbound, US3 cold transfer, US4 second carrier
- Paths are repository-relative

## Read before starting

Five facts from [research.md](research.md) that a reasonable implementer would otherwise get wrong, restated because getting them wrong costs a live debugging session:

1. **Giving this route a telephony plan arms about twenty-four hidden switches.** Every site that reads `.Telephony != nil` today means "carrier-websocket". T011a audits and narrows them before the helper is emitted. Skipping it produces a carrier build carrying the whole carrier-websocket artifact set and no deploy manifest.
2. **The route grants no call sources**, because the code that fills them is the adapter this route does not emit. The refusal is deliberate and tested (T016a, research D11). Granting them would be the exact failure the constitution forbids: green validation, empty values on a live call.
3. **The platform start endpoint cannot mint the interconnect room usefully.** Its response carries no SIP endpoint, so the helper creates the room itself and calls start with `createDailyRoom: false` (research F1). Do not follow the twilio-daily-sip guide's cloud snippet, which contradicts the official example repo (research R12).
4. **Daily dial-out has no SIP credential auth on any documented surface** (research F3, R6). The Connection therefore accepts `sip_address` but never `sip_username` or `sip_password`, and the carrier authenticates Daily by IP allow-list.
5. **Every outbound leg on a carrier target is a SIP URI at the carrier's termination address**, `sip:{E164}@{sip_address}`, including the cold transfer destination (research F2). Daily-only targets keep dialing E.164 unchanged.

Per spec decision 7, the official Pipecat documentation and the Daily documentation it links are the source of truth here. Where a repository document disagrees, the repository document changes by dated amendment.

---

## Phase 1: Setup and baseline

**Purpose**: capture the before-picture, so the "nothing else moved" promise (FR-003, SC-007) is provable rather than asserted.

- [ ] T001 Record the pre-feature baseline: run `go run . compile examples/human-transfer-daily` plus every other example into a scratch directory outside the repository, and note the exact file list and byte sizes in the Live Run Record section of this file. This is the artifact T060 diffs against, and it cannot be reconstructed after the change lands
- [ ] T002 [P] Read `internal/generate/testdata/golden/pipecat_v1.txt` and note which sections belong to the daily-sip fixture (`internal/testdata/safe_core` compiles a daily-sip pipecat target, so this golden already covers the no-carrier Daily route). Write the list of sections that MUST NOT move in this feature into the notes below T060
- [ ] T003 [P] Confirm what the pinned pipecat version already provides for the helper's server needs: check `internal/generate/templates/pipecat_v1/pyproject.toml.tmpl` and the pinned `pipecat` extras for `fastapi`, `uvicorn`, and an async HTTP client (`pipecat[daily]` pulls `aiohttp`). Record which of the three are already present, because T020 adds only what is genuinely missing and the constitution prices every new dependency

---

## Phase 2: Foundational (blocking prerequisites)

**Purpose**: make `(pipecat, daily-sip, twilio)` a real route, invert the four shipped guards that refuse it, and narrow the roughly twenty-four gating sites that would otherwise mistake it for a carrier-websocket route (research items 1 to 5). Every inversion is deliberate and paired with the amendment that authorises it.

**CRITICAL**: no user story work can begin until this phase is complete. Until the route row exists, `ResolveTelephonyFeature` returns `Gated: unsupported telephony route` for every feature and nothing renders.

### The amendment first, because it is what authorises the inversions

- [ ] T004 Append amendment **N37** to `docs/SCHEMA.md`, dated 2026-08-12, numbered after N36. It records: the Daily route gains carrier legs; on a `daily-sip` target `carrier:`, `connection:`, and `channels.phone` are now valid together and mutually required; the Connection key set for `(pipecat, daily-sip, twilio)` is `account_sid`, `auth_token`, `sip_address`, `from_number`, and why `sip_username`/`sip_password` are refused (research F3); the no-carrier form keeps its exact meaning and stays connection-free and channel-free; no new authoring field exists. It states plainly that this **supersedes N34's clause** rejecting a `channels.phone` entry on the Daily route, that the supersession follows the official Pipecat and Daily documentation per spec decision 7, and that N34's text stays as history. Append, never rewrite in place

### The route row

- [ ] T005 Add the `(pipecat, daily-sip, twilio)` row to `TelephonyRoutes()` in `internal/target/telephony.go`. `RequiredEnvironment` is exactly `account_sid`, `auth_token`, `sip_address`, `from_number`. Features are exactly route selection, inbound, outbound, cold transfer, and hangup, every one `Provisional` with `Docs` pointing at `https://docs.pipecat.ai/pipecat/telephony/daily-sip` and `Verified: "2026-08-12"`. Grant **no `source.*` features**: their fill path lives in the carrier-websocket adapter this route does not emit, so granting them would validate green and deliver empty values on a live call (research D11, R14, spec FR-004). Hangup is granted on verified evidence, so do not re-litigate it: `bot.py.tmpl` pushes an end frame at line 238 when a tool declares it ends the call and at line 338 on the transfer's hangup branch, neither gated on the transport (research R13). While in this file, extend the refusal note in `ResolveTelephonyFeature` (the switch around line 350 that already appends a fix hint for cold and warm transfer) with a case for the `source.*` features, naming where those sources do work, so the refusal names the fix rather than only the no (spec FR-004, asserted by T016a). `Processes` is the helper: name `telephony-helper`, command `uv run telephony_helper.py`, health and readiness `/healthz`. `PublicEndpoints` are `POST /call` gated on inbound, `POST /outbound` gated on outbound, and `GET /healthz`. Write one comment stating plainly that on this route `Processes`, `PublicEndpoints`, and `Services` describe the **operator-run helper**, not the deployed agent, which still exposes nothing of its own: every other route means the application by those fields, and a reader will assume the same here (data-model section 1). `RuntimeEnvironment` carries only what the **deployed agent** reads (`DAILY_API_KEY`); the helper's own names are a driver fact and arrive in T019. Set no `AutoWebhookEndpoint`: the CLI never writes a carrier webhook on this route. `ManualSteps` summarises the four carrier actions from `contracts/runbook.md`, with the README holding the full text
- [ ] T005a [P] Examine the scaffold's `daily-sip` branch in `internal/scaffold/scaffold.go` (lines 257 and 428) and make it correct for the carrier form: a scaffolded carrier target needs the carrier field, the Connection, and the phone channel together, and a scaffolded Daily-only target must keep emitting neither. The constitution's compliance review names the scaffold templates as a mandatory surface of any authoring-surface change (spec FR-002a), so if the honest answer is that nothing changes, record that finding in the task rather than leaving the surface unexamined. Keep `internal/scaffold/scaffold_test.go` lines 382 and 401 green, which pin the pairing error for the form that still refuses it
- [ ] T005b [P] Do the same for the interactive console in `internal/tui/tui.go`: the transport branch at line 2164 and the carrier prompt at line 2189 already exist, so check whether choosing `daily-sip` plus a carrier can produce a package the build then refuses, which would be a console that offers an invalid combination. Fix or record. The console is the second mandatory surface named by the compliance review (spec FR-002a), and `internal/tui/tui_test.go:1012` pins current behaviour through the accessible renderer
- [ ] T006 [P] Refresh the `daily_dialout` prerequisite summary in `internal/target/telephony.go` so it states that the approval covers SIP dial-out as well as PSTN dial-out, that it is per domain and granted on request, and that a carrier target needs no purchased Daily number (research F2, R7). Keep its `NeededBy` set and keep its empty carrier so it still matches every carrier on the transport. Update `Verified` to 2026-08-12
- [ ] T007 Add the new key to `emittedTelephonyFeatures` in `internal/generate/telephony_agreement_test.go` and add a matching `pipecatDailyCarrierEmittedTelephonyFeatures` map to `internal/generate/pipecat_v1.go`, holding exactly the set T005 grants, which is settled and not a judgement call: route selection, inbound, outbound, cold transfer, hangup. **No `source.*` entries**, since this map is hand-written and would otherwise make the agreement test pass on a claim the emitter cannot keep (research D11). Depends on T005: without it, `TestTelephonyRouteEmitterAgreement` goes red the moment the row lands, because its switch returns an empty map for any unrecognised key

### The four guard inversions

- [ ] T008 In `internal/ir/build.go` around lines 788 to 793, allow the connection-plus-phone-channel pairing on a `daily-sip` target that names a carrier, and keep refusing it on a `daily-sip` target with no carrier. Both existing errors keep their wording for every other case. A `daily-sip` target that names a carrier without a connection, or a connection without a carrier, must fail naming the missing field
- [ ] T009 In `internal/ir/build.go` around lines 943 to 970, add the service and coordination shape for the new route: `services` is `["application"]` only, with **no redis**, because this route has no shared control store by design (specs/004 FR-027) and the transfer guard is in-process. Keep `Coordination: "shared"` and one coordination reason, `admission` with consumer `application`, mirroring exactly what the LiveKit connector route already does for a Redis-free route. Do not append the Pipecat `call_correlation` and `callback_idempotency` reasons: both describe Redis-backed records this route does not keep
- [ ] T010 In `internal/ir/validate.go` `validateTelephonyPlan` around lines 1470 to 1486, add a case for the Pipecat daily-sip carrier route: allowed services `application` only, required services `application` only. Do **not** touch the `default` branch, which must keep requiring redis for the carrier-websocket routes, and do not weaken the `len(plan.Processes) == 0` guard: the helper process from T005 satisfies it honestly
- [ ] T011 In `internal/generate/pipecat_v1_build.go` `buildPipecatTelephony` at line 219, accept the daily-sip carrier form alongside carrier-websocket. The daily-sip branch keeps the positive-capacity check and the Connection vocabulary round trip (both directions: missing required key, unaccepted key, each naming the route), and adds **neither** `REDIS_URL` nor `UNMUTE_PUBLIC_URL`. The carrier-websocket branch keeps every byte of its current behaviour, including its own env additions and its `twilio, telnyx, plivo` carrier switch
- [ ] T011a **Read this before T011 lands, because T011 is what arms the trap.** Roughly twenty-four sites read `.Telephony != nil` as "this is a carrier-websocket route", and giving this route a telephony plan switches every one of them on: nine in `internal/generate/templates/pipecat_v1/README.md.tmpl`, eleven in `bot.py.tmpl`, one each in `Dockerfile.tmpl` and `pyproject.toml.tmpl`, plus `internal/generate/pipecat_v1.go:439`, `internal/generate/pipecat_v1_build.go:158`, `:306`, and `:784`. Enumerate them with a grep, then narrow each to the carrier-websocket transport (or to the new route where that is what it means), so a carrier Daily build cannot silently gain the carrier-websocket artifact set, its Dockerfile lines, its Redis wiring, or its README sections. Record the final site list in this task, because the count is the only proof the audit was exhaustive rather than partial
- [ ] T012 In `internal/generate/pipecat_v1_build.go` `humanTransferTool` around lines 784 to 786, narrow the refusal: it must still refuse a transfer on any carrier-websocket route with its current message, and must now allow one on the daily-sip carrier form. On a carrier target the cold destination becomes the SIP URI composed at the Connection's `sip_address` (research F2); on a Daily-only target it stays exactly what it is today

### Foundational tests, written before the code above is trusted

- [ ] T013 [P] Add to `internal/target/table_test.go`: `RouteAccountPrerequisites(Pipecat, "daily-sip", "twilio")` yields the one `daily_dialout` row, so the carrier form inherits it; and the existing `(Pipecat, "daily-sip", "")` case still yields exactly one. Extend `TestTelephonyRouteEvidenceIsExactAndProvisionalWithoutSmoke` coverage to the new key so a feature without docs or a verification date fails
- [ ] T014 [P] Add to `internal/target/telephony_test.go`: the new row's `RequiredEnvironment` is exactly the four keys in order, `sip_username` and `sip_password` are absent, the row carries no `AutoWebhookEndpoint`, and its `RuntimeEnvironment` carries no trunk ID and no Redis URL. Keep `TestTelephonyAutoWebhookIsATwilioFactOnly` green, which means asserting the new route deliberately opts out
- [ ] T015 [P] Add to `internal/ir/build_test.go`: a `daily-sip` target with carrier, connection, and a phone channel builds a plan whose key is the new triple, whose services are `["application"]` with no redis, and whose one coordination reason is `admission`; a `daily-sip` target with a carrier but no connection fails naming the connection; one with a connection but no carrier fails naming the carrier; and the existing no-carrier Daily target still builds with neither
- [ ] T016 [P] Add to `internal/ir/validate_test.go`: the new route validates with no redis service and zero errors, and a fabricated plan on the new route that declares a redis service fails with `telephony route declares unexpected service "redis"`. Pin that the carrier-websocket routes still require redis, so T010 cannot have relaxed the wrong branch
- [ ] T016a [P] Add to `internal/ir/validate_test.go`: a package declaring a variable sourced from a telephony call source (caller number, called number, call identifier, direction) on the new carrier route **fails**, and the message names both the source and the route, so an author learns the fix rather than only the no. This is the loud half of research D11: the refusal is the feature, and without this test a later change could grant the source silently. Pin alongside it that the same declaration still passes on a carrier-websocket route, where the fill path exists
- [ ] T016b [P] Add to `internal/ir/validate_test.go`: a warm transfer on the new carrier route still fails, and the message says which thing it means (Daily documents the pattern, this project does not emit it yet) rather than claiming the platform cannot do it, per spec FR-006 and the N34 wording rule. Extend `TestPipecatWarmHumanTransferFailsEverywhere` rather than writing a parallel test, so there is one home for the claim
- [ ] T017 [P] Add to `internal/spec/authoring_surface_test.go`: replace the stance half of `TestDailyRouteNeedsNoConnectionOrChannel` deliberately. Keep the assertion that a no-carrier Daily target loads with no connection and no channels; add that a carrier Daily target loads with all three; keep the assertion that the derived authoring schema grows **no** new property, which is what proves FR-001. Reference N37 in the test comment so the next reader finds the authorisation

**Checkpoint**: `go test ./...` passes. The route exists, validates, and refuses nothing it should allow. No artifact has changed yet.

---

## Phase 3: User Story 1 - A Twilio number reaches the deployed agent, from the README alone (Priority: P1) 🎯 MVP

**Goal**: a call to the operator's Twilio number is answered by the deployed Pipecat Cloud agent, set up from the generated README alone.

**Independent Test**: from a clean compile, an operator with only a Twilio account, their number, and a Pipecat Cloud account follows the "Telephony setup" section and places one call. No other document is consulted.

### Tests for User Story 1

- [ ] T018 [P] [US1] Create `internal/generate/pipecat_carrier_telephony_test.go` with the artifact contract per `contracts/forwarding-helper.md`: a carrier build emits `telephony_helper.py`, a no-carrier Daily build emits none; the carrier build still emits `pcc-deploy.toml` and still emits **no** `telephony.py`, `telephony_shared.py`, `telephony_state.py`, or `compose.telephony.yaml` (this is the assertion that catches a missed T011a site); the helper appears in `compile-report.json` generated files; and the helper's rendered text contains no secret-looking literal, no hardcoded account identifier, no third-party asset URL, and no `sip_username`
- [ ] T019 [P] [US1] In the same file, add the environment split per `contracts/environment.md`: every helper-only name (`PIPECAT_CLOUD_API_KEY`, and the optional `UNMUTE_HOLD_AUDIO_URL` and `UNMUTE_DAILY_ROOM_GEO`) reaches `.env.example` and the report but **never** the platform secret-set instructions; every agent-side name (the Connection's `account_sid`, `auth_token`, `sip_address`, plus `DAILY_API_KEY`) does reach the secret-set instructions; the optional names are marked optional; and the no-carrier `.env.example` is byte-identical to the pre-feature golden

### Implementation for User Story 1

- [ ] T020 [US1] Create `internal/generate/templates/pipecat_v1/telephony_helper.py.tmpl` per `contracts/forwarding-helper.md`. Startup names every missing required value and exits non-zero before serving.
      Hold audio follows research D4: play the operator's looped audio URL when that optional value is set, and otherwise loop a short spoken line. Bake in **no** third-party asset URL, not even the official example's, because a generated project that depends on someone else's host plays silence the day it moves and nobody knows why. `POST /call`: parse the carrier payload (missing call identifier answered with spoken-failure TwiML and a named log line); create the room through `pipecat.runner.daily.configure` with `sip_provider="daily"`, the caller as display name, dial-out enabled only when the package declares outbound or a cold transfer, a short room expiry, and the optional geography when set; start the agent at `POST https://api.pipecat.daily.co/v1/public/{agent}/start` with `createDailyRoom: false` and the body from `data-model.md` section 4; answer with TwiML that loops audio or speech per the hold-audio rule above, never a bare pause (research R3); on any failure before the answer, speak a short failure line and hang up rather than parking the caller. `POST /outbound` renders only with outbound declared, requires the bearer token, validates `{"to": "+E164"}` by field name, creates a dial-out room, and starts the agent with the composed `sip:{to}@{sip_address}`. `GET /healthz` returns 200. Log env names, never values. Only what T003 proved missing is added to `internal/generate/templates/pipecat_v1/pyproject.toml.tmpl`, gated on the carrier so a Daily-only project's dependencies do not move
- [ ] T021 [US1] In `internal/generate/pipecat_v1.go` `renderPipecatFiles` around lines 425 to 472, emit `telephony_helper.py` for a carrier build. Depends on T011a: the branch that emits the carrier-websocket artifact set must already be keyed on the transport rather than on `data.Telephony != nil`, or a carrier Daily build gains `telephony.py`, `telephony_shared.py`, `telephony_state.py`, and `compose.telephony.yaml` and loses `pcc-deploy.toml`. Gate the helper on the daily-sip carrier form specifically, and keep `pcc-deploy.toml` emitted for both Daily forms, because a carrier build still deploys to Pipecat Cloud. Carry the helper's env names and the T019 split into `pipecatData`, and render the optional-versus-required marking in `internal/generate/templates/pipecat_v1/env.example.tmpl`, which is the only place that marking can live
- [ ] T022 [US1] In `internal/generate/templates/pipecat_v1/bot.py.tmpl`, add the inbound carrier block: read `direction`, `call_sid`, and `sip_uri` from the validated body; register the ready-event handler; forward the live call **exactly once** behind a module-global guard, however many times the event fires (research R11), by updating the carrier's call with `<Response><Dial><Sip>{sip_uri}</Sip></Dial></Response>` over the HTTP client already in the dependency tree, authenticated with the Connection's REST credential names. No carrier SDK. A body carrying no `direction` is not a phone session and must reach today's code path untouched
- [ ] T023 [US1] In `internal/generate/templates/pipecat_v1/README.md.tmpl`, write the `## Telephony setup` section per `contracts/runbook.md`: rendered only for a carrier build, opening with the counts computed from what the package declares (at most six carrier actions, at most two commands here), then `### At your carrier (Twilio)` keyed on the carrier with the four dictated actions (number, point it at the helper, termination address, allow-list Daily's `sip.hosts` from `https://ip-info.daily.co/ips/ip-info.json` with its three-day change lead), then a carrier-neutral `### On this side` with the two commands. Carry the four notes the contract requires: one number serves one target at a time and how to move it both ways, the transfer cost (Daily stays anchored and both legs keep billing, research F4), caller identity is carrier-governed and unverified, and the troubleshooting map from caller experience to cause. Never a credential list for termination, never a SIP REFER or PSTN transfer toggle: this route sends no REFER to the carrier
- [ ] T024 [P] [US1] Add the runbook contract test to `internal/generate/pipecat_carrier_telephony_test.go` per `contracts/runbook.md`: the section appears exactly once for a carrier build and zero times for a no-carrier build; the stated counts match the rendered actions; the platform part contains no carrier name; the forbidden content is absent; every environment reference is a name and never a value; and the one-number-one-target note names both directions
- [ ] T025 [P] [US1] Add the forward-once test: render the carrier bot and assert the guard is claimed before the forwarding request is issued and that the handler returns early on a second event, so a double ready signal cannot forward twice (spec US1 scenario 3). Assert against the rendered text the same way `TestUS2_DailyTransferAttemptsOnce` does for the transfer guard
- [ ] T026 [US1] Split `TestUS2_DailyProjectDeclaresNoServiceOrEndpoint` in `internal/generate/pipecat_v1_test.go` deliberately: the no-carrier assertion stays exactly as it is; a new carrier case asserts the helper's process and endpoints are present on the plan while the **deployed agent** still declares no public endpoint of its own, and that no `REDIS_URL`, `UNMUTE_PUBLIC_URL`, or carrier-websocket credential appears anywhere in the carrier build. Update the doc comment above `TestUS5_OutboundInstructionsNameTheIdentityAndThePermission` (lines 2026 to 2032), which currently states the now-superseded FR-002 rationale, to point at N37
- [ ] T027 [US1] In `internal/cli/dev.go` around lines 110 to 125, keep the `--telephony` refusal for every daily-sip target and make its message true for both forms. Narrow the current branch to the no-carrier case, keeping its wording; add a carrier case that names the helper, says the local test path is running the helper beside a tunnel as the README dictates, and does not claim there is no local telephony topology. Neither message may claim telephony is unsupported on the route, which would be false. Reached before the generic `has no executable telephony topology` path, because a carrier target now has a non-nil plan
- [ ] T028 [P] [US1] Add to `internal/cli/dev_test.go`: `unmute dev --telephony` on a carrier Daily target exits 1 with a message naming the helper and the README, and on a no-carrier Daily target exits 1 with today's message. Assert message content, not only the exit code
- [ ] T029 [US1] Create the example `examples/human-transfer-daily-twilio/` per research D9: `targets.yaml` with one pipecat target on `transport: daily-sip`, `carrier: twilio`, `connection:`, a `deployment_region`, and `destinations` using environment-variable names only; `connections/twilio_sip_daily.yaml` with the four keys mapped to the names in `contracts/environment.md`, reusing the specs/005 example's names where the meaning is identical; `agent.yaml` with `channels.phone` inbound and outbound, one cold transfer, and `capacity` with a positive `peak_starts_per_second`; plus `instructions.md` and a `README.md` modelled on `examples/human-transfer-daily/README.md`. Leave `examples/human-transfer-daily` untouched
- [ ] T030 [US1] Register the new example everywhere the suite pins the example set: the directory list in `internal/generate/examples_test.go` around lines 229 to 232, and the daily-route marker list in `TestDailyRouteWorkDoesNotReachOtherTargets` around lines 163 to 168, which gains the new carrier markers (the helper filename, the forward-once guard name, the runbook heading) so carrier work cannot leak onto a non-carrier target
- [ ] T031 [US1] Regenerate goldens deliberately with `go test ./internal/generate -run TestPipecat -update-pipecat`, then read every changed line before accepting. The no-carrier sections listed under T002 must not have moved. Run `make fmt && make lint && make build && make test`
- [ ] T032 [US1] LIVE: quickstart live steps 1 to 3. Drill the helper's refusal on an incomplete `.env` on purpose, then start it, tunnel it, complete the runbook's carrier part, and place one inbound call. Record the answer delay against SC-004, confirm from the logs that exactly one forward happened, and write the dated outcome into the Live Run Record below

**Checkpoint**: an operator can go from a clean compile to an answered call using the generated README alone. This is the MVP and it is independently shippable, with the route's capabilities still marked provisional.

---

## Phase 4: User Story 2 - The agent dials out through the operator's carrier (Priority: P2)

**Goal**: the deployed agent places a call that rings a real phone through the operator's own trunk.

**Independent Test**: declare outbound with the carrier connection, deploy, trigger a call following the emitted instructions, answer it.

- [ ] T033 [P] [US2] Add to `internal/generate/pipecat_carrier_telephony_test.go`: the helper's `POST /outbound` endpoint renders only when the package declares outbound, the bearer token is required, the destination is validated by field name, and the composed dial target is `sip:{destination}@{sip_address}` built from the Connection's names rather than any literal
- [ ] T034 [P] [US2] Add the validate-report test: a carrier package declaring outbound reports the `daily_dialout` prerequisite with its SIP dial-out wording from T006, and a carrier package declaring neither outbound nor a transfer reports no prerequisite at all, which is the anti-banner rule specs/004 established
- [ ] T035 [US2] In `internal/generate/templates/pipecat_v1/bot.py.tmpl`, add the outbound carrier block: on the outbound direction, call `start_dialout` with the composed SIP URI and `provider: "daily"` (research R6), and register the connected, stopped, and warning handlers so a rejected leg surfaces with its cause named rather than as a dead call (spec US2 scenario 4)
- [ ] T036 [US2] In `internal/generate/templates/pipecat_v1/README.md.tmpl`, extend the runbook and the phone-calls section for outbound on a carrier target: the exact trigger command against the helper, that the recipient's caller identity is governed at the carrier and what this project does and does not control, and that the account prerequisite is the same one the prerequisites section already names. State plainly that no Daily number purchase is needed on this route
- [ ] T037 [US2] Regenerate goldens with `-update-pipecat`, read the diff, and confirm the no-carrier sections still have not moved. Run `make test`
- [ ] T038 [US2] LIVE: quickstart live step 4. Trigger one outbound call through the helper, confirm the target phone rings, and record what caller identity the recipient actually saw. This is the fact research F2 left provisional; write the finding and its date into the Live Run Record, and correct the emitted wording in T036 if the observation contradicts it

**Checkpoint**: inbound and outbound both work on the operator's own carrier, independently.

---

## Phase 5: User Story 3 - Cold transfer is proven on a carrier call (Priority: P2)

**Goal**: on a live carrier call, the caller is handed to a person and the agent leaves, with every failure path keeping the caller connected and informed.

**Independent Test**: run the transfer recipe end to end on a carrier-carried call, including the failure drill, and record the dated result.

- [ ] T039 [P] [US3] Add to `internal/generate/pipecat_carrier_telephony_test.go`: on a carrier target the transfer's `toEndPoint` is the composed SIP URI built from the Connection's `sip_address` and the resolved destination, while on a Daily-only target it stays byte-identical to today. Assert the at-most-one-attempt guard and the caller-stays-connected failure branches are unchanged in shape on both, since specs/004 already proved them
- [ ] T040 [P] [US3] Add the startup-check test: the transfer destination and the carrier credentials the transfer path needs all appear in the emitted startup check for the carrier example, so a missing value fails by name rather than as a failed transfer on a paid call (specs/004 FR-011)
- [ ] T041 [US3] In `internal/generate/templates/pipecat_v1/bot.py.tmpl`, confirm the transfer primitive needs no change for the carrier leg: it stays `sip_call_transfer({"toEndPoint": ...})` behind the transport narrow (research F4, R8, R9). Record in the comment above it that `sip_refer` was considered and rejected because it depends on the originating carrier honouring REFER, which neither platform documents, and that the chosen primitive keeps Daily anchored with both legs billing. Do not add a second primitive
- [ ] T042 [US3] In `internal/generate/templates/pipecat_v1/README.md.tmpl`, state the transfer facts for a carrier target where the operator will look for them: the destination is dialled through their own trunk, the account prerequisite is the same dial-out approval, and the billing consequence of an anchored transfer. Do not restate the prerequisites section; point at it
- [ ] T043 [US3] Regenerate goldens with `-update-pipecat`, read the diff, run `make test`
- [ ] T044 [US3] LIVE: quickstart live step 5. Run the transfer to the spec's counts on carrier-carried calls: completion attempts against SC-005, then the failure drill with the destination declining, confirming the caller stays connected and is told. Confirm a second request in one call never produces a second attempt. Record every count and date in the Live Run Record
- [ ] T045 [US3] LIVE: quickstart live step 6, the failure mapping drill. Break one thing at a time (helper stopped, an allow-list entry removed, a platform secret removed) and confirm the runbook's troubleshooting map is honest. Correct the map in `README.md.tmpl` for anything the drill proved wrong, then regenerate goldens and record the outcome

**Checkpoint**: the three flows the requester asked to test all work on their own carrier, with dated evidence.

---

## Phase 6: User Story 4 - A second carrier changes words, not shapes (Priority: P3)

**Goal**: adding Telnyx or Plivo later is writing one forwarding action and one block of instruction text, nothing else.

**Independent Test**: inspect where the Twilio-specific content lives; the platform part and the agent contain no carrier name.

- [ ] T046 [P] [US4] Add the carrier-seam test to `internal/generate/pipecat_carrier_telephony_test.go`, mirroring the specs/005 seam test: render a fixture on a second carrier and assert the `### On this side` part is byte-identical to the Twilio fixture's, the carrier part falls back to generic prose pointing at the route's docs URL, the emitted artifact set is the same shape, and the helper's rendered text names no carrier at all
- [ ] T047 [US4] Make the seam real where the test finds it dishonest: the carrier-specific forwarding action in `bot.py.tmpl` keyed off the carrier the target names, and the carrier part of the runbook keyed the same way, with everything else carrier-neutral. Do **not** add a second carrier's route row, instruction text, or forwarding action in this feature; the structure is what ships here
- [ ] T048 [US4] Run `make test` and confirm the seam test passes without a second carrier existing, which is what proves the structure rather than the content

**Checkpoint**: the architecture scales by writing text, as the requester asked.

---

## Phase 7: Polish, documents, and the gate

**Purpose**: make every document tell the same story the artifacts tell, and close the gate.

- [ ] T049 [P] Update `docs/TRANSFERS.md`: the Pipecat cold transfer row gains the carrier route with its dated evidence from T044, the anchored-transfer billing fact, and the rejected-`sip_refer` reasoning. The warm cell keeps saying what it means (Daily documents the pattern, this project does not emit it yet) and gains the note that the carrier leg will carry warm unchanged when that feature lands
- [ ] T050 [P] Update `docs/TELEPHONY.md`: the Pipecat telephony section gains the carrier leg beside the Daily-provisioned form, with the helper named as an operator-run artifact and the local-versus-deployed distinction specs/004 established left intact
- [ ] T051 [P] Update `docs/user/targets/pipecat.md` and `docs/user/learn/07-phone-calls.md`: the two Daily forms, when to choose each, the four Connection keys, why no SIP credentials exist on this route, and the helper's role. Keep the two-way sync test in `internal/target/user_docs_test.go` green
- [ ] T052 [P] Update `docs/user/reference/controls.md` and `docs/user/reference/targets-yaml.md` for the new route triple and the pairing rule, and audit every "not supported" sentence these edits touch so each says whether it means the platform cannot do it or this project does not emit it yet, with a date (specs/004 SC-013)
- [ ] T053 [P] Correct the stale daily-sip claims the route change invalidates: `docs/SCHEMA.md` N34's channel clause is superseded by N37 rather than edited in place (T004 did this; verify no other N34 sentence now reads false), and any `docs/` sentence claiming `daily-sip` is Daily-provisioned only. Search for `daily-sip` across `docs/` and fix each hit or record why it is still correct
- [ ] T054 Update `docs/REPO_MAP.md` if the helper template changes where a reader should look for telephony emission on the Pipecat driver
- [ ] T055 [P] Fill in the outstanding live-run record in `specs/005-telephony-setup/tasks.md` (T010, T011, and the T021 live half), which the requester confirmed on 2026-08-12 as working on real calls for inbound, cold transfer, and warm transfer. This maps to no requirement of this feature and edits another feature's artifacts; it is carried here because the evidence rule is repository-wide and specs/005's file still shows those tasks open while the feature is shipped and proven. Drop it if the requester would rather close it separately
- [ ] T056 In the new route row in `internal/target/telephony.go`, lift `Provisional` to the proven tag only for the features T032, T038, and T044 actually exercised, and leave the rest provisional with a one-line note saying what is still unproven. A capability may not be presented as more proven than its evidence
- [ ] T057 [P] Run `go test ./internal/generate -run TestPublicExamplesEmitLintCleanPython` and confirm the helper is inside the linted set and clean. Any hand-edited Python in this change is checked with `ty` and `ruff` per CLAUDE.md
- [ ] T057a Perform the plain-language review that spec SC-009 names and CLAUDE.md requires, on the compiled carrier example's README rather than on the template: can a reader who has never used Daily or Pipecat Cloud say which steps happen at the carrier, which happen here, and what the helper is for? Fix what the review finds, in `README.md.tmpl`, then regenerate goldens and record what it changed. A review that changes nothing is recorded as such, with what was checked
- [ ] T058 Run the whole offline half of [quickstart.md](quickstart.md) and confirm every listed signal, including the byte-identity of the no-carrier build against the T001 baseline
- [ ] T059 Run `make fmt`, `make lint`, `make build`, `make test` and confirm zero failures with zero Python. Then run `make smoke` and confirm it passes or skips cleanly when `uv` is absent, and that it never entered the pull request gate
- [ ] T060 Diff the final emitted output of every pre-existing example against the T001 baseline and confirm byte-identity for all of them, with the new example the only addition. Enumerate every accepted golden change in the pull request text, read rather than regenerated blind

---

## Live Run Record

Fill in as the live tasks complete. A capability row may not lose `provisional` without a dated line here.

| Task | Flow | Date | Outcome | Notes |
|---|---|---|---|---|
| T032 | Inbound answered on a Twilio number | | | answer delay against SC-004; forward-once confirmed from logs |
| T038 | Outbound rings through the carrier trunk | | | what caller identity the recipient saw (research F2) |
| T044 | Cold transfer completes, and the failure drill | | | attempt counts against SC-005; second-request guard |
| T045 | Failure mapping drill | | | which troubleshooting lines the drill corrected |

**Baseline notes (T001, T002)**: record the pre-feature file list and the no-carrier golden sections that must not move.

---

## Dependencies and Execution Order

### Phase dependencies

- **Phase 1 Setup**: no dependencies. T001 must happen before any code change, or the baseline is unrecoverable
- **Phase 2 Foundational**: depends on Phase 1 and blocks every story. The amendment (T004) comes first because it authorises T008 and T017, and it is also what FR-002a's mandated surfaces (T005a, T005b) are checked against
- **Phase 3 US1**: depends on Phase 2 complete. Delivers the MVP
- **Phase 4 US2**: depends on Phase 2. Independently testable, but its live step is easier once US1 has proven the helper runs
- **Phase 5 US3**: depends on Phase 2 for the code and on US1's live path for its live proof, because a cold transfer needs an inbound call to exist
- **Phase 6 US4**: depends on the runbook and helper existing (US1). Adds no carrier
- **Phase 7 Polish**: depends on the stories whose facts the documents state, and on the live runs for T056

### Inside Phase 2, the order that matters

T004 (amendment) → T005 (route row) → T007 (agreement map, or the suite goes red) → T008, T009, T010 (build and validate guards) → **T011a (the gating-site audit) → T011** (the audit must land first, because T011 is what makes the field non-nil) → T012 → the tests T013 to T017. T006, T005a, and T005b are independent of that chain.

### Parallel opportunities

- T002 and T003 run together in Phase 1
- T005a, T005b, and T006 run alongside the guard work once T005 lands; T013 to T017 all touch different test files and run together, T016a and T016b included
- T018 and T019 run together before T020; T024, T025, and T028 run together after their implementation lands
- T033 and T034 run together; T039 and T040 run together
- Every task in Phase 7 marked [P] runs together, since each owns different files. T057a is not among them: it changes the README template and so must precede the final golden read

### The two sequencing traps

Every task that regenerates goldens (T031, T037, T043, T045, T057a) touches the same golden files, so they never run in parallel with each other. Each one reads its own diff before accepting.

T011a before T011, as above. It is the one ordering in this list whose violation fails quietly rather than loudly: the suite would go red only where T018 happens to look.

---

## Implementation Strategy

### MVP first (User Story 1 only)

1. Phase 1 baseline, Phase 2 foundation
2. Phase 3 to the checkpoint
3. **Stop and validate**: one live inbound call, recorded
4. Shippable here: the carrier route answers calls, capabilities still provisional, every other route untouched

### Incremental delivery

Phase 3 (inbound) → Phase 4 (outbound) → Phase 5 (cold transfer, the outcome the work exists for) → Phase 6 (the seam) → Phase 7 (documents and the gate). Each phase adds a testable flow without touching the previous one's artifacts.

### Rollback confidence

The whole feature is additive behind the carrier declaration. If a live phase fails structurally, the no-carrier Daily route and every other example are byte-identical (T060), so the compiler change ships safely with `provisional` intact.

## Notes

- [P] means different files and no dependency on an incomplete task
- Tests name their assertion, not just their file, so a task cannot be closed by an empty test
- Every platform claim written into an emitted file or a document carries its source and verification date from [research.md](research.md)
- No em or en dashes in emitted text or documents; plain wording throughout
- Commit after each task or logical group; stop at any checkpoint to validate a story on its own
