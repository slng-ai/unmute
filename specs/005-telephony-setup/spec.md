# Feature Specification: Telephony setup, one YAML block and a dictated runbook

**Feature Branch**: `feature/warm-cold-human-transfer`

**Created**: 2026-08-12

**Status**: Draft

**Input**: User description: "Architect the full Twilio + LiveKit telephony setup for a generated package: inbound, outbound, warm and cold human transfer, all driven from the YAML definition, with the generated README dictating the exact carrier-side steps we cannot do for the operator. Must comply with LiveKit and Twilio docs (not our own), stay as simple as possible, minimize manual steps, and be structured so other SIP carriers can be added later. This supersedes and absorbs the retire-inbound-trunk-id feature (drop LIVEKIT_SIP_INBOUND_TRUNK; resolve the trunk by phone number instead)."

## Where We Stand Today

Read from the working tree of `feature/warm-cold-human-transfer` and verified live on 2026-08-12.

**What already works.** The YAML declares one telephony connection holding four environment variable names (`sip_address`, `sip_username`, `sip_password`, `from_number`; the example uses `SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD`, `SIP_FROM_NUMBER`). Dial-out passes those four values inline with each call, so no LiveKit outbound trunk is ever registered (specs/002, SCHEMA N33). A live warm transfer succeeded on 2026-08-12 (specs/003, run A1). Cold transfer code is emitted and correct: it acts on the caller's existing phone leg and logs a skipped line when there is no phone caller in the room.

**What does not work yet.** Nobody can call the deployed example agent. Receiving a call needs three things that exist only as partial instructions today: the carrier must forward calls to LiveKit (origination URI), LiveKit must accept the number (inbound trunk record), and LiveKit must route the call to this agent (dispatch rule). The current instructions for this are split across the self-hosted README section, docs/TRANSFERS.md, and tribal knowledge, and they require the operator to copy a LiveKit record ID into an environment variable (`LIVEKIT_SIP_INBOUND_TRUNK`) whose only job is to glue two setup commands together. Cold transfer therefore cannot be exercised live, because it needs an inbound call to exist first.

**This feature emits no new agent code.** The call handling (answer, dial out, warm, cold) is finished. This feature is about the setup surfaces: the provisioning artifacts, the generated README runbook, the retirement of the copied ID, and the docs that describe them.

## Decisions That Shape This Feature

Settled with the requester on 2026-08-12.

1. **Elastic SIP trunking is the Twilio path, not the voice webhook.** One Twilio trunk carries inbound calls, outbound calls, and the transfer request (SIP REFER) for cold transfer, and the same four values the YAML already declares drive all of it. LiveKit documents a webhook alternative (TwiML for Programmable Voice) for inbound only; choosing it would add a second configuration surface without removing a single step, and it serves neither outbound nor transfers. (Source: LiveKit Twilio quickstart, read 2026-08-12.)
2. **Dial-out stays inline.** No stored LiveKit outbound trunk, ever. This shipped in specs/002 and is a live-proven path.
3. **Inbound keeps the two LiveKit records, because they are the documented minimum.** An inbound trunk (this number belongs to this project) and a dispatch rule (calls on it go to this agent). What goes away is the copied record ID: the setup resolves the trunk by the phone number the operator already has in their environment file. This absorbs the retire-inbound-trunk-id feature drafted earlier on 2026-08-12; that draft never reached disk, and its requirements live here now.
4. **Cold transfer configures nothing.** It rides the inbound leg. Its only settings are carrier-side toggles (Call Transfer via SIP REFER, plus PSTN transfer on Twilio), and the runbook dictates them.
5. **Carrier instructions are content, not structure.** The LiveKit-side records and commands are the same for every carrier. The carrier-side part of the runbook is keyed off the carrier the connection names, so adding Telnyx or Plivo later means writing their instruction text, not changing any artifact shape.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Set up telephony from the README alone (Priority: P1)

An operator compiles the human-transfer example, opens the generated README, and finds one ordered "Telephony setup" section with two clearly labeled parts: what to do at the carrier (which we cannot do for them) and what to do at LiveKit (copy-paste commands). Following it top to bottom, with nothing else open, they end with a working phone agent: calls to their number are answered by the agent, the agent can dial out, a warm transfer brings a colleague into the call, and a cold transfer hands the caller over and hangs up the agent.

**Why this priority**: This is the product promise. Every other story is a refinement of this one, and today the inbound half of it is not achievable from our instructions at all.

**Independent Test**: From a clean compile, an operator with only a Twilio account and the four carrier values follows the README section and places one call of each kind. No other document is consulted.

**Acceptance Scenarios**:

1. **Given** a fresh compile and a filled `.env`, **When** the operator completes the carrier part of the runbook, **Then** every carrier action was dictated as an exact console path or command, with nothing left to guess and no step that says only "see the docs".
2. **Given** the carrier part is done, **When** the operator runs the LiveKit part, **Then** it completes with at most two commands and at no point asks them to read an ID from one command's output and type it into anything.
3. **Given** the runbook is complete, **When** someone calls the package's number, **Then** the agent answers.
4. **Given** an answered inbound call, **When** the caller asks for a manager, **Then** the warm transfer briefs the colleague and merges the call, and a caller asking for billing is cold transferred out and the session ends.
5. **Given** the operator skipped the carrier transfer toggles, **When** a cold transfer fires, **Then** the transfer fails, the failure log line appears, and the README maps that line to the missing toggle.

---

### User Story 2 - No copied record IDs anywhere (Priority: P2)

The operator never reads a LiveKit-assigned record ID and never pastes one. `LIVEKIT_SIP_INBOUND_TRUNK` disappears from every emitted surface: the environment example, the README, the compile report's required names, and the dev flow's classification. The dispatch rule step finds the trunk by the phone number already present in the operator's environment file, and it fails loudly if that number has no trunk yet.

**Why this priority**: The variable is the single most confusing object in the current setup. It looks like a runtime secret, it is not one (the deployed agent never reads it), and it exists only to carry an ID between two commands. Removing it removes a whole class of operator questions, but the runbook (story 1) is worth shipping even if this lags.

**Independent Test**: Compile a package with an inbound route and search the build directory for the retired name: zero occurrences. Follow the runbook: zero manual ID handling.

**Acceptance Scenarios**:

1. **Given** a fresh compile with an inbound route, **When** the emitted files are searched for `LIVEKIT_SIP_INBOUND_TRUNK`, **Then** there are no occurrences, except that one sentence in the README may tell operators of earlier builds the variable is retired and can be deleted.
2. **Given** the phone number has no inbound trunk yet, **When** the dispatch rule step runs, **Then** it stops with a message naming the number and the missing trunk, and no dispatch rule is created.
3. **Given** any path through the runbook or the emitted artifacts, **When** a dispatch rule is created, **Then** it is scoped to this package's trunk. A rule that matches every trunk in the project must be impossible to produce by following our instructions or artifacts.

---

### User Story 3 - A second carrier changes words, not shapes (Priority: P3)

When a Telnyx or Plivo connection is added to the vocabulary later, the LiveKit-side records, commands, and emitted artifacts stay byte-identical in shape. Only the carrier part of the runbook differs, and it is sourced from the carrier entry the connection names.

**Why this priority**: This is what "scalable" means here. It costs nothing extra if stories 1 and 2 are built with the seam in the right place, and it protects the setup from being rewritten per carrier.

**Independent Test**: Inspect where the Twilio instruction text lives: it is keyed off the connection's carrier, and the LiveKit-side instructions contain no carrier name.

**Acceptance Scenarios**:

1. **Given** the generated README, **When** the LiveKit part of the runbook is read, **Then** it names no carrier and would be correct verbatim for any SIP provider.
2. **Given** the compiler sources, **When** a new carrier's instructions are added, **Then** no emitted artifact changes shape and no LiveKit-side instruction changes.

---

### User Story 4 - Local dev telephony stays zero-step (Priority: P3)

`unmute dev --telephony` keeps provisioning its local trunk and dispatch rule automatically. Retiring the environment variable must not silently disable that flow: the dev flow keys off the route's inbound capability, not off the presence of the retired name.

**Why this priority**: Regression guard. Nobody asked for new dev behavior; the risk is breaking existing behavior while removing the variable.

**Independent Test**: The dev telephony flow's provisioning trigger is exercised by the existing test suite against a route with inbound, with the retired name absent everywhere.

**Acceptance Scenarios**:

1. **Given** a package whose route accepts inbound calls, **When** `unmute dev --telephony` runs, **Then** local trunk and dispatch records are still created with no manual step.

---

### Edge Cases

- **The number is already claimed in a shared LiveKit project.** Creating a second inbound trunk for the same number fails at LiveKit. The runbook says what that failure means (someone else's trunk holds your number, or you ran the step twice) and that re-running after a partial setup is safe.
- **The dispatch step runs before the trunk exists.** It must stop loudly (story 2, scenario 2). The silent alternative, creating a rule with no trunk scope, would route every trunk in the project to this agent and is the one forbidden outcome.
- **Cold transfer without the carrier toggles.** The call fails after the "fired" and "referring" log lines; the README maps the failure line to the Twilio Call Transfer and PSTN transfer settings.
- **Cold transfer from the Agent Console.** There is no phone leg, so the agent logs the skipped line and tells the caller. Already shipped in specs/003; the runbook states that cold transfer can only be tested with a real phone call.
- **Twilio does not authenticate inbound calls with a username and password.** LiveKit inbound trunks support that auth, Twilio Elastic SIP Trunking does not send it (LiveKit inbound trunk docs, read 2026-08-12). The emitted inbound record must therefore not carry auth fields for Twilio; the number itself is the match.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The YAML MUST remain the single authoring input for telephony: one connection naming four environment variables, plus the target's routes and destinations. Setting up Twilio inbound, outbound, and both transfers MUST require no new authored field in the example.
- **FR-002**: The generated README for a package on a **SIP** route MUST contain one ordered "Telephony setup" runbook with two labeled parts: the carrier part ("you must do this at your carrier; we cannot do it for you") and the LiveKit part (commands to run). Every step MUST be an exact console path or a copy-paste command; a bare link with no stated action does not count as a step. The runbook replaces the section currently headed "Configure self-hosted LiveKit SIP", whose name tells a LiveKit Cloud operator to skip the one section they need. The connector route is excluded: it carries the inbound feature but has no SIP trunk, so it MUST NOT receive this runbook or the provisioning script.
- **FR-003**: For Twilio, the carrier part MUST dictate, in order: purchase a voice-capable number; create an Elastic SIP trunk; configure termination (the Termination SIP URI, one domain ending in `pstn.twilio.com`, which is the value of the hostname environment variable and MUST be presented as one value rather than two, plus a credential list holding the username and password the environment file names); configure origination (the origination URI set to the project SIP URI with `;transport=tcp`, and the exact way to obtain that URI: the LiveKit project settings page, or the project ID from `lk project list --json` with the `p_` prefix removed); associate the number with the trunk; and, when the package has a cold transfer, enable Call Transfer (SIP REFER) and PSTN transfer on the trunk.
- **FR-003a**: The runbook is written for LiveKit Cloud, the supported deployment. Exactly one step differs for a self-hosted deployment, because there is no project SIP URI there: origination points at the operator's own public LiveKit SIP endpoint. That difference MUST appear as a labeled note beside the origination step, and the capability table's manual-step text MUST name both targets rather than only the self-hosted one.
- **FR-004**: The LiveKit part MUST complete with at most two commands after the environment file is filled, MUST take every value it needs from that file or from the emitted artifacts, and MUST NOT require the operator to transcribe any identifier between steps. The trunk MUST be found by the phone number.
- **FR-005**: No path through the emitted artifacts or the runbook may create a dispatch rule that matches every trunk in the project. If the trunk cannot be resolved, the step MUST fail with a message naming the number, and create nothing.
- **FR-006**: `LIVEKIT_SIP_INBOUND_TRUNK` MUST leave every emitted surface: the environment example, the README's required names, the compile report, and the dev flow's environment classification. One retirement sentence in the README, telling operators of earlier builds to delete the variable, is the only permitted mention.
- **FR-007**: The runbook MUST state that cold transfer needs no LiveKit-side setup beyond inbound, that its carrier requirement is the transfer toggles, and that it cannot be tested from the Agent Console. It MUST map the transfer failure log line to the carrier toggles.
- **FR-008**: The carrier part's content MUST be keyed off the carrier the connection names, so that adding a carrier adds instruction content only. The LiveKit part MUST be carrier-neutral text with no carrier name in it.
- **FR-009**: `unmute dev --telephony` MUST keep provisioning local records automatically for routes that accept inbound calls, with its trigger derived from the route's declared capability rather than from any environment variable name. This includes the infrastructure-first startup order (infrastructure services, then records, then the application), which today is switched on by the same environment-name check being retired. A test MUST cover that trigger, so that removing it fails the suite instead of quietly switching local telephony off.
- **FR-010**: The repository docs that describe this flow (docs/TRANSFERS.md, docs/user/learn/07-phone-calls.md, docs/user/targets/livekit.md, and the reference pages that list required environment names) MUST tell the same runbook and MUST NOT mention the retired variable except as a dated retirement note.
- **FR-011**: No secret value may appear in any emitted file or document; environment variable names only. Every platform behavior claim in emitted text and repository docs MUST carry its source and verification date.

### Key Entities

- **Telephony connection**: the YAML object naming the four carrier environment variables. Already exists; this feature adds nothing to it.
- **Inbound trunk record**: the LiveKit-side record claiming the package's phone number for this project. Carrier-neutral: it holds a name and the number.
- **Dispatch rule record**: the LiveKit-side record routing calls accepted on that trunk to this package's agent. Always scoped to the package's trunk.
- **Runbook**: the README's telephony setup section. Two parts; the carrier part varies by carrier, the LiveKit part never does.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator holding only carrier credentials, with no LiveKit SIP records existing for their number, completes the whole setup using the generated README and nothing else, and all four call flows work live: an inbound call is answered, an outbound call rings, a warm transfer merges, a cold transfer connects the caller and ends the session. The live run is recorded with dates in the feature's task file.
- **SC-002**: A search of a fresh build directory for the retired variable name returns zero matches (the single permitted README retirement sentence aside), and the runbook contains zero steps that transcribe an identifier.
- **SC-003**: The runbook's carrier part for Twilio has at most six actions and the LiveKit part at most two commands, and the runbook states this count so an operator can see the end from the start.
- **SC-004**: The full test suite passes, and every golden file change is read and accepted deliberately rather than regenerated blind.
- **SC-005**: A reader who has never used LiveKit can say, from the README alone, which steps happen at the carrier, which at LiveKit, and what each is for. Checked by the plain-language review that CLAUDE.md already requires of all documents, performed on the compiled example's README and recorded with what it changed.
- **SC-006**: The local dev telephony flow provisions its records with zero manual steps, exactly as before the variable retirement.

## Platform Facts This Feature Depends On

Constitution principle IV: every claim carries its source and the date we read it.

| # | Fact | Source | Verified |
|---|------|--------|----------|
| V1 | Twilio setup order is: buy number, create Elastic SIP trunk, origination URI for inbound, termination URI plus credential list for outbound, associate number with trunk. | docs.livekit.io/telephony/start/providers/twilio/ | 2026-08-12 |
| V2 | The origination URI is the project SIP URI with `;transport=tcp`; the SIP URI is the project ID minus its `p_` prefix, from the project settings page or `lk project list --json`. | docs.livekit.io/telephony/start/sip-trunk-setup/ | 2026-08-12 |
| V3 | LiveKit inbound needs exactly two records: an inbound trunk (name plus numbers) and a dispatch rule; the documented minimal trunk record carries no auth fields. | docs.livekit.io/telephony/start/sip-trunk-setup/ | 2026-08-12 |
| V4 | Twilio Elastic SIP Trunking does not support username and password auth on inbound; the number is the match. | docs.livekit.io/telephony/accepting-calls/inbound-trunk/ | 2026-08-12 |
| V5 | Cold transfer is `TransferSIPParticipant`, a SIP REFER through the trunk the call arrived on; it takes no trunk argument, and on Twilio it requires Call Transfer (SIP REFER) enabled plus PSTN transfer; caller ID for the transfer target is a trunk setting, not per transfer; `ringing_timeout` caps the wait, default 30 seconds. | docs.livekit.io/telephony/features/transfers/cold.md | 2026-08-12 |
| V6 | Dial-out with inline carrier configuration (hostname, username, password, from number) is documented and needs no stored LiveKit outbound trunk. | specs/002 contract, SCHEMA N33; warm transfer proven live (specs/003 run A1) | 2026-08-12 |
| V7 | `lk sip inbound list --json` exists for resolving a trunk by its number, and `lk sip dispatch create` accepts a rule file. | lk CLI 2.18.2, checked locally | 2026-08-12 |
| V8 | A dispatch rule with no trunk scope matches every trunk in the project. | docs.livekit.io/telephony/accepting-calls/dispatch-rule/ (wildcard note) | 2026-08-12 |

## Assumptions

- One phone number per package, as today. Multiple numbers are a future feature.
- A shared LiveKit project (several agents, several trunks) is a supported deployment context. This is why FR-005 is absolute: in a shared project an unscoped rule captures everyone's calls.
- The operator has a Twilio account with Elastic SIP Trunking available, and permission to change trunk settings.
- The retire-inbound-trunk-id draft from earlier on 2026-08-12 is fully absorbed here (its stories are story 2 and story 4, its wildcard ban is FR-005). No directory for it exists on disk; the feature pointer now names this feature.
- The self-hosted LiveKit SIP path keeps working as documented. It loses the retired variable like every other surface, and it shares the one runbook rather than keeping a separate one; its compose, ports, and Redis instructions are untouched, and its single genuine difference is the origination target (FR-003a).
- Cold transfer needs no configuration of its own, so it appears in the runbook only as the carrier toggles plus notes. Its acceptance scenario about missing toggles (US1 scenario 5) is verified by inspecting the mapped log line and README text; deliberately breaking a live trunk's configuration is optional and needs the trunk owner's agreement.

## Out Of Scope

- Automating carrier configuration. The CLI never calls Twilio's API; carrier steps are dictated, not performed.
- The TwiML voice-webhook inbound path for the LiveKit target (decision 1).
- The LiveKit Twilio connector route. It has its own section, no SIP trunk, and no transfers; it loses the retired variable with everything else and gains nothing here.
- Writing the carrier instruction content for providers other than Twilio. The structure must be ready for them (story 3); the words come with their connections.
- Pipecat and Daily telephony. specs/004-pipecat-cloud-telephony owns that surface.
- Any change to the emitted agent code. Answering, dialing, warm, and cold are done and live-tested where a live test was possible.
