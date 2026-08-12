# Research: Telephony setup

All platform claims verified live on 2026-08-12 (LiveKit docs MCP and local `lk` 2.18.2). Spec's V1 to V8 table holds the sources; this file holds the decisions.

## D1. Twilio path: Elastic SIP Trunking, not the voice webhook

- **Decision**: the runbook dictates Elastic SIP Trunking only. One trunk carries inbound (origination URI), outbound (termination URI plus credential list), and cold transfer (SIP REFER through the same trunk).
- **Rationale**: the webhook path (TwiML for Programmable Voice) is documented by LiveKit as an inbound-only alternative. It serves neither dial-out nor REFER, so choosing it would double the configuration surface without removing one step. The requester's own trunk is already the elastic kind with all three capabilities.
- **Alternatives considered**: TwiML webhook (rejected above); supporting both (rejected: two runbooks to keep true, and nothing asked for the second).

## D2. LiveKit-side provisioning: one emitted script

- **Decision**: emit `telephony-setup.sh` into the build when the route accepts inbound calls. The README's LiveKit part becomes one command: `bash telephony-setup.sh`. The script consumes the two JSON inputs the build already emits.
- **Rationale**: FR-004 forbids transcribing an ID between commands. Something must list trunks, match by number, and feed the ID into the dispatch rule. A script is testable (its bytes are pinned like any template output) and keeps the README honest; a README paste-block is the same logic where no test can see it. Compile-time provisioning (the CLI calling `lk` or the admin API itself) is rejected by principle I (compile works offline) and by the telephony boundary (Unmute automates only its own local dev state).
- **Alternatives considered**: README command blocks (rejected: untestable, quoting-fragile); `lk sip dispatch create --trunks <id>` flag (rejected: still needs the resolved ID, and a forgotten flag creates the forbidden wildcard rule); compile-time provisioning (rejected: constitution).

## D3. Trunk resolution inside the script: list first, by number

- **Decision**: the script is idempotent by content, mirroring the dev flow's `ensureRecord` semantics. Sequence: `lk sip inbound list --json`, match the entry whose `numbers` contains the package's number; if absent, create from `sip-inbound-trunk.json` (script substitutes the number itself), then list and match again; if the ID is still empty, stop with a message naming the number; then create the dispatch rule from `sip-dispatch-rule.json` with the resolved ID substituted, skipping creation when a rule for that trunk and agent already exists.
- **Rationale**: re-running after a partial setup must be safe (spec edge case). Matching by number is the one key the operator already owns. Parsing `lk`'s create output was rejected because its shape is not a documented contract; the list JSON is.
- **Parsing**: `jq`. Dictated as a prerequisite line in the runbook next to `lk`. grep/sed on nested JSON rejected as wrong on edge cases; python3 rejected as a heavier prerequisite than jq on a deploy-only machine. `envsubst` is no longer needed by the runbook: the script does its own `${NAME}` substitution.

## D4. The wildcard ban, structurally

- **Decision**: `sip-dispatch-rule.json` keeps `trunk_ids` always present, with the placeholder renamed from `${LIVEKIT_SIP_INBOUND_TRUNK}` to `${UNMUTE_SIP_TRUNK_ID}`. That name is internal to the script; it is never an environment variable the operator sets, never in `.env.example`, never in the required-env list. The script refuses to create the rule while its resolved ID is empty.
- **Rationale**: FR-005. The failure direction matters: if substitution somehow did not happen, the rule would be scoped to the literal placeholder string, a nonexistent trunk ID, which matches nothing. Fail closed, never open. The dispatch-rule docs' wildcard note (empty `trunk_ids` matches every trunk in the project, read 2026-08-12) is why the key may never be emitted empty or omitted.
- **Test**: a structural test asserts the emitted JSON contains a non-empty `trunk_ids` array, and the script contains the empty-ID guard.

## D5. Dev flow re-key, and the death of DevSuppliedEnvironment

- **Decision**: `ensureLiveKitSIPRecords` gates on the route's inbound feature instead of `needs("LIVEKIT_SIP_INBOUND_TRUNK")`. Confirmed by reading a real compile report on 2026-08-12: the plan's `Evidence` carries `['cold_transfer', 'inbound', 'outbound', 'route', 'warm_transfer']`, and `TelephonyInbound` is the string `"inbound"` (`internal/target/telephony.go:18`). Evidence reflects what the package asked for, which is the right question here. The `injected` env map disappears: the local agent never read the ID (specs/003 R8), so there is nothing left to inject. With that, `DevSuppliedEnvironment` has zero members anywhere and the chain is deleted: `internal/target/telephony.go` field and row, `internal/ir/build.go` copy, `internal/generate/telephony.go` `DevSuppliedEnv` field, and the two uses in `internal/cli/dev_compose.go` (the `supplied` seeding in `externalTelephonyEnv`, and the stale-value check in `rejectLocalTopologyConflicts`).
- **One site is not a deletion, and mis-reading it is the biggest risk in this feature.** `internal/cli/dev_telephony.go:160`, `if len(plan.DevSuppliedEnv) > 0`, is the gate for the **two-phase local startup**: it sets `run.infraServices` and installs the `beforeApp` hook that creates the records. An earlier draft of this research and of the data model called it a display block, which was wrong. Its condition moves to the same inbound-feature check; the hook and the infra-first ordering stay. Only the env-injection loop inside the hook goes. A test covers the gate (T020) so that a future deletion fails loudly instead of quietly switching local telephony off.
- **Second consequence, accepted**: `rejectLocalTopologyConflicts` currently errors when an operator sets a dev-supplied name locally. After the retirement, a stale `LIVEKIT_SIP_INBOUND_TRUNK` in a local `.env` is simply ignored rather than flagged. That is the correct end state for a name nothing reads, and the README retirement sentence tells the operator to delete it.
- **Rationale**: FR-009 plus principle III: a field that names nothing is a second, false description of the environment. Deleting it is smaller than keeping it accurate.
- **Alternatives considered**: adding an `Inbound bool` to `TelephonyRuntimePlan` (rejected: `Evidence` already states the features; a second boolean is a copy); keeping `DevSuppliedEnv` empty (rejected: dead plumbing that reads as meaning something).

## D5b. Two hosting models, one runbook, one labelled exception

- **Decision**: the runbook is written for **LiveKit Cloud**, which the constitution's Targets And Providers section and specs/004 both settle as the supported deployment. Its origination step names the project SIP URI. The self-hosted case differs in exactly one step, because a self-hosted deployment has no project SIP URI: origination points at the operator's own public LiveKit SIP endpoint. That stays in the README as a short labelled note next to the origination step, not as a second runbook.
- **Rationale**: the emitted section is currently titled `## Configure self-hosted LiveKit SIP` and reads as self-hosted only, so a Cloud operator is told by the heading to skip the one section they need. Meanwhile the capability table's `ManualSteps` step 2 names only the self-hosted origination target. Both were describing a single hosting model while the artifact serves two. The heading becomes `## Telephony setup`, and step 2 names both targets.
- **Alternatives considered**: two full runbooks (rejected: FR-002 asks for one ordered section, and the two differ by one line); Cloud only with self-hosted origination left unstated (rejected: it would point a real carrier at the wrong host, and the spec's own assumption promises the self-hosted path keeps working).
- Everything else in the runbook is hosting-neutral: the same two LiveKit records, the same script, because `lk` carries the difference.

## D6. Where the carrier instruction text lives

- **Decision**: in `README.md.tmpl`, as a conditional block on `.Telephony.Carrier`: a full dictated section for `twilio`, and a generic fallback for other carriers that names the same four values, the origination URI requirement, and the transfer enablement requirement, pointing at `.Telephony.ProviderDocs`. The LiveKit part is one carrier-neutral block.
- **Rationale**: FR-008 and story 3: adding Telnyx later is adding a template block (content), while artifacts and LiveKit-side text stay untouched. A catalog struct field for instruction prose was rejected: no second carrier's prose exists yet, so the abstraction would have one user.
- Also: `internal/target/telephony.go` `ManualSteps` (the compile report's summary) is rewritten to match: the trunk-ID copy step becomes "run `bash telephony-setup.sh` from the build directory (or let `unmute dev --telephony` create local records itself)".

## D7. SCHEMA amendment N36

- **Decision**: append N36 (2026-08-12): inbound provisioning resolves the trunk by phone number; `LIVEKIT_SIP_INBOUND_TRUNK` leaves every emitted surface; `telephony-setup.sh` is the new provisioning artifact; the authoring surface is unchanged and no existing package fails strict decode; a deployment that still sets the old variable is unaffected because nothing reads it (it never did at runtime).
- Latest existing amendment is N35, so N36 is next (checked 2026-08-12).
- **Stale forward reference**: `docs/SCHEMA.md` (support tables, lines around 732 and 795) says Daily warm transfer is "feature 005". This directory took number 005; the Daily warm feature will be numbered when it is specified. Those two mentions change to "a planned follow-up feature" as part of the doc sweep, so 005 does not appear to promise Daily warm.

## D8. The environment surface after this feature

Required names for a LiveKit SIP package with inbound, outbound, warm, and cold:

- Carrier: the four the Connection declares (example: `SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD`, `SIP_FROM_NUMBER`).
- Platform: `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` (Cloud injects them on deploy; local dev supplies them).
- Self-hosted only: `REDIS_URL`.
- Destinations: whatever env-form destinations the package declares (example: `SUPERVISOR_PHONE_NUMBER`, `BILLING_PHONE_NUMBER`).

Removed: `LIVEKIT_SIP_INBOUND_TRUNK`. Added: nothing. `${UNMUTE_SIP_TRUNK_ID}` is a substitution token inside one JSON file, not an environment name (D4).

## D9. Housekeeping verified alongside

- `internal/generate/livekit_v1.go` carries an `UNVERIFIED` comment dated 2026-07-20 about the SIP JSON shapes (MCP was unavailable then). The shapes match the SIP trunk setup page read 2026-08-12 with MCP (`trunk.numbers`, `dispatch_rule.trunk_ids`, `dispatchRuleIndividual.roomPrefix`, `roomConfig.agents[].agentName`); the comment is refreshed with that date.
- The script must never echo a credential: it reads only the number and non-secret names, `set -euo pipefail`, no `set -x`, and `lk` handles its own auth (Cloud login or `LIVEKIT_URL` plus key pair for self-hosted), so the same script serves both hosting models.
- Full occurrence map of the retired name (grep, 2026-08-12): `internal/target/telephony.go:191,196`, `internal/cli/dev_livekit_sip.go:69,83`, `internal/generate/livekit_v1.go:688`, `internal/generate/livekit_v1_build.go:554`, `README.md.tmpl:285,327`, `docs/TRANSFERS.md:281`, `docs/TELEPHONY.md:90,310,765`, `docs/user/targets/livekit.md:547,579`, `docs/user/learn/07-phone-calls.md:394,434,538,704,734`, `docs/user/reference/cli.md:137`, `examples/human-transfer/README.md:106`, plus eight test files (`dev_livekit_sip_test.go`, `dev_telephony_test.go`, `dev_test.go`, `livekit_inline_trunk_test.go`, `livekit_v1_test.go`, `build_test.go`, `telephony_test.go`, `user_docs_test.go`). No golden file contains the name.
