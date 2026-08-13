# Tasks: Telephony setup, one YAML block and a dictated runbook

**Input**: Design documents from `specs/005-telephony-setup/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/ (all present)

**Tests**: included. The constitution requires a runnable check behind non-trivial logic, and the contracts are written to be pinned by tests.

**Organization**: grouped by user story. Stories share three files (`telephony.go`, `README.md.tmpl`, test files), so the story phases run sequentially in the order below; [P] applies only within a phase.

## Format: `[ID] [P?] [Story] Description`

## Phase 1: Setup

**Purpose**: a known-green baseline, so every later diff is attributable.

- [X] T001 Baseline green at commit `011c2a4` (`make fmt && make lint && make test`, 2026-08-12) before any change
- [X] T002 [P] `livekit_v1_remy.txt` pins **no** telephony surface: its fixture has no telephony channel, so its sections are `.dockerignore`, `.env.example`, `Dockerfile`, `README.md`, `agent.py`, `compile-report.json`, `compose.dev.yaml`, `pyproject.toml`, `tracing.py`. The telephony surfaces live in three separate goldens: `livekit_v1_sip-inbound-trunk.json`, `livekit_v1_sip-dispatch-rule.json`, `livekit_v1_telephony_compose.yaml`. Only the last two changed in this feature (placeholder rename, retired name leaving the Compose env list)

## Phase 2: Foundational

No foundational tasks. The compiler pipeline, the capability table, and the emitted SIP JSON inputs already exist; every story edits existing surfaces. Story phases begin directly.

---

## Phase 3: User Story 1 - Set up telephony from the README alone (US1, Priority: P1) 🎯 MVP

**Goal**: the generated README carries the two-part runbook, and the build carries `telephony-setup.sh` so the LiveKit side is one command with no ID transcription.

**Independent Test**: quickstart Runs 2, 4, and 5. From a fresh compile, an operator with only the Twilio account and `.env` completes setup from the README and the agent answers a phone call.

### Implementation for User Story 1

- [X] T003 [US1] Create `internal/generate/templates/livekit_v1/telephony-setup.sh.tmpl` implementing `contracts/provisioning-script.md`: `set -euo pipefail`, never `set -x`; preflight checks for `lk`, `jq`, and a non-empty from-number env (each missing item named, exit non-zero); **never `source ./.env`** (it would read every secret in the file, and one non-identifier line would abort the script under `set -e`, which is a failure this project has already seen on LiveKit Cloud): when the from-number variable is unset, read that one assignment out of `./.env` textually, for example `sed -n 's/^{{.Telephony.FromNumberEnv}}=//p' ./.env | head -1`; resolve the trunk via `lk sip inbound list --json` matched by number with `jq`; create from `sip-inbound-trunk.json` if absent (script substitutes `${{{.Telephony.FromNumberEnv}}}` itself, no `envsubst`), then resolve again; hard guard exiting before any dispatch create while the trunk ID is empty; create the dispatch rule from `sip-dispatch-rule.json` with `${UNMUTE_SIP_TRUNK_ID}` substituted, skipping when a rule already targets that trunk; print one created-or-reused line per record and never a credential
- [X] T004 [US1] In `internal/generate/livekit_v1.go`: emit `telephony-setup.sh` from the new template when `telephony.HasInbound`, **inside the existing `if !connector` guard at line 638** where `livekitSIPFiles` is already called. The connector route also carries the inbound feature and has no SIP trunk at all, so gating on `HasInbound` alone would ship a SIP provisioning script into a route that cannot use one. Also rename the dispatch placeholder at line 688 from `LIVEKIT_SIP_INBOUND_TRUNK` to `UNMUTE_SIP_TRUNK_ID`, and refresh the stale `UNVERIFIED` comment at lines 656 to 659: the JSON shapes were re-verified against docs.livekit.io/telephony/start/sip-trunk-setup/ with the docs MCP on 2026-08-12
- [X] T005 [US1] In `internal/generate/templates/livekit_v1/README.md.tmpl`: write the `## Telephony setup` section per `contracts/runbook.md`, **inside the SIP branch** (the `{{- else}}` at line 264, which is the non-connector arm), placed where the current `## Configure self-hosted LiveKit SIP` heading sits at line 265. Rename that heading to `## Telephony setup`, because the section is what a LiveKit Cloud operator needs and its current name tells them to skip it. The section carries: the opening paragraph stating the step count, the prerequisites line naming `lk` and `jq`, an `### At your carrier (Twilio)` block gated on `eq .Telephony.Carrier "twilio"` with the six dictated steps (cold-transfer toggles gated on the package having a cold transfer), a generic `### At your carrier` fallback for other carriers pointing at `.Telephony.ProviderDocs`, a carrier-neutral `### At LiveKit` part whose one command is `bash telephony-setup.sh`, the transfer notes including the failure-line-to-toggle mapping, and the single retirement sentence for `LIVEKIT_SIP_INBOUND_TRUNK`. The runbook's origination step is the LiveKit Cloud form (project SIP URI); keep the existing self-hosted paragraph that points origination at the operator's own deployed LiveKit SIP endpoint, as a clearly labelled note for self-hosted deployments, so the two hosting models do not read as one. Delete the old inbound env bullet (line 285), the `Create the LiveKit SIP resources` envsubst block (lines 310 to 337), and every `envsubst` mention (3 sites)
- [X] T006 [US1] In `internal/target/telephony.go` lines 197 to 202: rewrite `ManualSteps` step 4 to say the operator runs `bash telephony-setup.sh` from the build directory for deployments, and that `unmute dev --telephony` creates local records itself; no step may mention copying a trunk ID. Also correct step 2, which currently states only the self-hosted origination target ("point the carrier's origination URI at that public SIP endpoint"): it must name both, the Cloud project SIP URI and, for a self-hosted deployment, the operator's own public LiveKit SIP endpoint
- [X] T007 [P] [US1] Create `internal/generate/livekit_telephony_setup_test.go`: script emitted for an inbound SIP route, absent for an outbound-only one, and absent for the connector route (which has the inbound feature but no SIP trunk); the connector README carries no `## Telephony setup` section and no SIP runbook; script contains the preflight names, the empty-ID guard, and no `set -x`; the script never contains `source` of the env file, and its only substitution tokens are the from-number env and `UNMUTE_SIP_TRUNK_ID`; `sip-dispatch-rule.json` carries a non-empty `trunk_ids` with the renamed token; README pins per `contracts/runbook.md` (section present, count paragraph, prerequisites line, Twilio block when carrier is twilio, `bash telephony-setup.sh` present, the cold-transfer failure line mapped to the carrier toggles per FR-007, the Agent Console caveat, and the retirement sentence appearing exactly once)
- [X] T008 [US1] Update stale expectations in `internal/generate/livekit_v1_test.go` and `internal/generate/livekit_inline_trunk_test.go` that pin the old README wording, the envsubst block, or the old placeholder
- [X] T009 [US1] Regenerate the livekit golden deliberately with the package's own flag, `go test ./internal/generate -run TestLiveKit -update-livekit` (declared at `internal/generate/livekit_v1_test.go:18`; a bare `-update` does not exist in this package), read every changed line of `internal/generate/testdata/golden/livekit_v1_remy.txt` before accepting, then `make fmt && make lint && make test`
- [X] T010 [US1] LIVE: quickstart Run 4 against the real LiveKit project (`bash telephony-setup.sh` twice; first run creates, second reports reused; `lk sip inbound list` and `lk sip dispatch list` confirm scope); record date and outcome in the Live Run Record below
- [X] T011 [US1] LIVE: quickstart Run 5 flows 1 to 3 after completing the Twilio part of the runbook (inbound answered, warm transfer on a real inbound call, cold transfer completes with the specs/003 log lines); record outcomes below

**Checkpoint**: an operator can set up everything from the README. The retired name still lingers in `.env.example` until US2.

---

## Phase 4: User Story 2 - No copied record IDs anywhere (US2, Priority: P2)

**Goal**: `LIVEKIT_SIP_INBOUND_TRUNK` leaves every emitted surface; zero matches in a fresh build beyond the retirement sentence.

**Independent Test**: quickstart Run 1.

### Implementation for User Story 2

- [X] T012 [US2] In `internal/target/telephony.go`: delete the `{Name: "LIVEKIT_SIP_INBOUND_TRUNK", AnyFeatures: ...}` row from `RuntimeEnvironment` (line 191) and its explanatory comment's inbound-ID clause
- [X] T013 [US2] In `internal/generate/livekit_v1_build.go` lines 553 to 555: delete `env.add("LIVEKIT_SIP_INBOUND_TRUNK")` and rewrite the trailing comment: inbound still needs its two platform records, but their ID is resolved by `telephony-setup.sh` at provisioning time and no environment name carries it
- [X] T014 [P] [US2] In `internal/generate/livekit_telephony_setup_test.go`: add the zero-occurrence test: compile the inbound fixture, walk every emitted file, assert the retired name appears only in `README.md` and exactly once; assert `.env.example` and `compile-report.json` contain zero occurrences
- [X] T015 [US2] Update `internal/target/telephony_test.go` and `internal/ir/build_test.go` expectations for the shrunken `RuntimeEnvironment`
- [X] T016 [US2] Regenerate the affected goldens deliberately (`-update-livekit` for `internal/generate`; `internal/cli` compose goldens use their own flag, check the test file before running), read the diff, `make test`

**Checkpoint**: quickstart Run 1 passes.

---

## Phase 5: User Story 4 - Local dev telephony stays zero-step (US4, Priority: P3)

**Goal**: the dev flow keys off the route's inbound feature, and the dead `DevSuppliedEnvironment` plumbing is gone end to end.

**Independent Test**: quickstart Run 6 plus the existing dev test suite.

### Implementation for User Story 4

- [X] T017 [US4] In `internal/cli/dev_livekit_sip.go`: replace `needs("LIVEKIT_SIP_INBOUND_TRUNK")` (line 69) with a gate on the plan's inbound feature (scan `plan.Evidence` for the inbound feature name as `internal/target` spells it; confirm the exact string from the `TelephonyInbound` constant before writing it); delete the `injected` map and the env return (the local agent never read the ID, specs/003 R8); update callers for the changed signature
- [X] T018 [US4] **Read this task before touching `dev_telephony.go`.** The `if len(plan.DevSuppliedEnv) > 0` at `internal/cli/dev_telephony.go:160` is **not** a display block: it is the gate that switches on the whole two-phase local startup, setting `run.infraServices` and installing the `beforeApp` hook that creates the records. Replace that condition with the same inbound-feature check T017 introduces (extract it as one small helper both call), **keep** `run.infraServices` and `run.beforeApp`, and delete only the `for _, name := range plan.DevSuppliedEnv` injection loop inside the hook. Getting this wrong silently disables local telephony provisioning, which is the regression this story exists to prevent (FR-009, SC-006, constitution principle II)
- [X] T019 [US4] Delete the rest of the `DevSuppliedEnvironment` chain, now that nothing gates on it: the field and line 196 in `internal/target/telephony.go`, the copy at `internal/ir/build.go` lines 939 to 941 plus the IR plan field it fills, the `DevSuppliedEnv` field in `internal/generate/telephony.go` (lines 18 to 20, the clone at line 58, and the `dev_supplied_environment` report key), and both uses in `internal/cli/dev_compose.go`: the `supplied` seeding loop in `externalTelephonyEnv` (lines 57 to 62, no behavior change because the name also leaves `RequiredEnv`) and the loop in `rejectLocalTopologyConflicts` (lines 84 to 88). Note in the commit that the second deletion drops the friendly "supplied by dev itself" error for an operator who still has the retired name in `.env`; the value is now simply ignored, which the README retirement sentence already tells them
- [X] T020 [US4] Update the tests that referenced the chain: `internal/cli/dev_livekit_sip_test.go`, `internal/cli/dev_telephony_test.go`, `internal/cli/dev_test.go`, `internal/ir/build_test.go`, `internal/target/telephony_test.go`; the dev record-creation test must now exercise the feature-evidence gate (inbound route creates records, outbound-only route does not), and one test must cover the T018 gate specifically, so that deleting the two-phase startup fails the suite rather than passing quietly
- [ ] T021 [US4] `make test` including `internal/cli/dev_compose_smoke_test.go` is **green**; the LIVE half is pending: quickstart Run 6 (`unmute dev --telephony` still creates or reuses local records with no manual step and no mention of the retired name)

**Checkpoint**: dev flow proven unchanged in behavior.

---

## Phase 6: User Story 3 - A second carrier changes words, not shapes (US3, Priority: P3)

**Goal**: the carrier seam is proven by test, not by intention.

**Independent Test**: the two tests below pass with no production change beyond US1's template.

### Implementation for User Story 3

- [X] T022 [P] [US3] In `internal/generate/livekit_telephony_setup_test.go`: render or compile a telnyx-carrier fixture and assert the generic `### At your carrier` fallback appears with the provider docs link, the `### At LiveKit` part is byte-identical to the twilio fixture's, and the emitted artifact set (`telephony-setup.sh`, both SIP JSON files) is the same shape
- [X] T023 [US3] Add the neutrality pin: the rendered `### At LiveKit` section and `telephony-setup.sh` contain none of the carrier names the capability table declares (twilio, telnyx, plivo, exotel)

**Checkpoint**: all four stories functional.

---

## Phase 7: Polish and Cross-Cutting

- [X] T024 [P] Append SCHEMA amendment N36 (2026-08-12) to `docs/SCHEMA.md` per research D7 (trunk resolved by number, name retired, script is the provisioning artifact, runbook is Cloud-shaped with the self-hosted origination step unchanged, authoring surface unchanged, old deployments unaffected), and rewrite the two stale "feature 005" mentions of Daily warm transfer (near lines 732 and 795) to "a planned follow-up feature"
- [X] T025 [P] Update `docs/TRANSFERS.md`: the environment table row at line 281 and the cold-transfer setup wording, aligned with the runbook contract
- [X] T026 [P] Update `docs/TELEPHONY.md` lines 90, 310, and 765: the LiveKit SIP resource row now describes `telephony-setup.sh` and states no environment name carries the trunk ID
- [X] T027 [P] Update the user docs: `docs/user/targets/livekit.md` (lines 547, 579), `docs/user/learn/07-phone-calls.md` (lines 394, 434, 538, 704, 734), `docs/user/reference/cli.md` (line 137); run `go test ./internal/target -run UserDocs` (the sync test in `user_docs_test.go`) and fix both directions
- [X] T028 [P] Update `examples/human-transfer/README.md` line 106 (retirement wording) and simplify `preflight.sh` lines 38 to 47: the special case for the retired name becomes unnecessary and is deleted
- [X] T029 Plain-language review of the emitted runbook (SC-005), the review CLAUDE.md requires of every document: read the compiled `examples/human-transfer/build/livekit/README.md` telephony section as someone who has never used LiveKit, and confirm it says which steps happen at the carrier, which at LiveKit, and what each is for, in simple words with no em or en dashes. Record what changed as a result
- [X] T030 Run quickstart Runs 1, 2, 3, and 6 end to end; full gate `make fmt && make lint && make build && make test`
- [X] T032 Added on request after T005 shipped: the runbook's carrier half gained a runnable command block. One carrier-neutral `### Get your origination URI` section above both carrier branches lists every configured project with its SIP URI, plus a by-name variable form for scripting; the Twilio branch gained `#### The same steps as commands` (trunk, credential list, origination URI, number attach, transfer toggles, with no SID transcribed and the password read at a prompt) and `#### Check the carrier side` (the three states that break this side). Every command was `--help`-verified against the installed `twilio-cli` 6.2.4 and run read-only against a real account and project on 2026-08-12; `transfer-mode enable-all` and `transfer-caller-id from-transferee` were confirmed against live trunk records rather than recalled. The first draft derived the URI from `.env`'s `LIVEKIT_URL` and printed nothing on the shipped example, because a Cloud `.env` has no `LIVEKIT_URL`; that is why the shipped version lists projects instead. `contracts/runbook.md` gained the matching section
- [ ] T031 LIVE: complete the SC-001 ledger below, including the optional toggle-off cold transfer probe (quickstart Run 5 flow 4) if the trunk owner agrees to the temporary config change

---

## Live Run Record

| Run | Date | Outcome |
|---|---|---|
| Script first run (T010) | 2026-08-12 | **Pass.** Created the inbound trunk and the dispatch rule against slng-atlas from the emitted script |
| Script idempotence (T010) | 2026-08-12 | **Pass.** Second run created nothing |
| Inbound answered (T011) | 2026-08-12 | **Pass.** Call to `SIP_FROM_NUMBER` reached the deployed agent `livekit` (`CA_pxm6TiuPibJq`, eu-central) |
| Warm on inbound call (T011) | 2026-08-12 | **Pass.** Confirmed by the requester on a real inbound call, alongside inbound answering and cold transfer. Recorded here on 2026-08-13 while implementing specs/006, which mirrors this runbook on the Pipecat driver; the row had stayed open after the run. |
| Cold completes (T011) | 2026-08-12 | **Pass.** Caller asked for billing, was connected onward, agent left the call, three cold log lines clean |
| Dev flow regression (T021) | | **Still open.** The requester's 2026-08-12 confirmation covers inbound, cold, and warm on real calls, which is T010 and T011. It does not cover `unmute dev --telephony` creating or reusing the local records, so this row stays empty rather than borrowing that evidence. |
| Toggle-off cold probe (T031, optional) | | |

What the live pass needed that the plan had not anticipated, both now documented: the phone number had to be **attached to the trunk** (its `trunkSid` was empty, so origination was never consulted), and one secret name (`11LABS_API_KEY`) was not a valid shell identifier, which LiveKit Cloud reports as a single `/etc/run/env` line at the top of `lk agent logs` and otherwise swallows. The generated README, the example README and `docs/user` all carry both now.

Reference: warm transfer passed live on 2026-08-12 from the Agent Console (specs/003 run A1), and again on a real inbound leg the same day, which is what T011 needed.

**Verified read-only on 2026-08-12, ahead of the live runs.** The script's two risky halves were exercised against the real LiveKit project with `lk` 2.18.2 without creating anything: `lk sip inbound list --json` returns `{"items":[{"sipTrunkId","name","numbers",...}]}` and the resolver's `jq` filter correctly returned empty for the package's number (no trunk claims it yet, which is the pre-setup state); `lk sip dispatch list --json` returns `{"items":[{"sipDispatchRuleId","trunkIds",...}]}` and the existence check answered correctly for both a present and an absent trunk ID; both `sed` substitutions produced valid JSON with the expected values. What remains for the live runs is the mutating half: the two creates, idempotence on a second run, and the calls.

**Still pending, and why.** T021's live run and T031 need the Twilio console steps done on the trunk, the operator's credentials, and a phone. T010 and T011 are done: the runs happened on 2026-08-12 and the record above was completed on 2026-08-13. Everything else is green offline.

## Dependencies & Execution Order

- Phase 1 first. Phase 2 is empty by design.
- **US1 → US2 → US4 → US3**: US2 removes names US1's runbook replaced; US4 deletes plumbing that US2's surface removal makes dead; US3 only pins what US1 built and can run any time after Phase 3. The stories share `telephony.go`, `README.md.tmpl`, and the new test file, which is why they are sequenced rather than parallel.
- Polish (Phase 7) after all stories; T024 to T028 are parallel (different files), then T029, T030, T031 in order.
- Within US4 the order is fixed: T017 introduces the inbound-feature helper, T018 switches the startup gate onto it, and only then does T019 delete the old field. Deleting the field first would leave the gate uncompilable and invites replacing it with something weaker.
- LIVE tasks (T021's run, T031) need credentials and a phone; everything else is CI-green offline. T010 and T011 ran on 2026-08-12.

## Parallel Example: Phase 7

```bash
Task: "Append SCHEMA amendment N36 in docs/SCHEMA.md"
Task: "Update docs/TRANSFERS.md environment row"
Task: "Update docs/TELEPHONY.md lines 90, 310, 765"
Task: "Update user docs and run the sync test"
Task: "Update examples/human-transfer/README.md and preflight.sh"
```

## Implementation Strategy

MVP is Phase 1 plus Phase 3 (US1): after T011 an operator can set up the whole thing from the README, and the live acceptance is provable. US2 then deletes the retired name (quick, mostly subtraction), US4 deletes the plumbing, US3 pins the carrier seam, and Phase 7 makes the documents tell the same story. Stop and validate at every checkpoint; the golden regen tasks (T009, T016) are deliberate, read-the-diff gates, never a blind regeneration.

Ten findings from `/speckit-analyze` on 2026-08-12 are folded in above, one of them critical (the `dev_telephony.go:160` startup gate, T018). Two decisions came out of that review and are now recorded in the spec: the runbook is Cloud-shaped with a labelled self-hosted note for origination (FR-003a), and the connector route is out of scope.
