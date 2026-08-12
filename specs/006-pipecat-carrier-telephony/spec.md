# Feature Specification: Pipecat carrier telephony, bring your own number to the Daily route

**Feature Branch**: `feature/warm-cold-human-transfer`

**Created**: 2026-08-12

**Status**: Draft

**Input**: User description: "Multi-carrier telephony for Pipecat Cloud: add Twilio as a carrier with the same zero-friction setup experience as specs/005-telephony-setup gives LiveKit. Must support testing inbound calls, outbound calls, cold transfer, and warm transfer. Architecture should scale to more carriers (Telnyx, Plivo) without changing bot code; consider Daily SIP (provider=daily) vs provider WebSocket endpoints."

## Where We Stand Today

Read from the working tree of `feature/warm-cold-human-transfer` and from the Pipecat documentation the requester supplied, both on 2026-08-12.

**What already works.** specs/004 shipped. The Daily route `(pipecat, daily-sip)` compiles, deploys to Pipecat Cloud, and answers an inbound call on a Daily-provisioned number. Cold transfer is emitted with the at-most-one-attempt guard and the caller-stays-connected failure paths. The validate report names the Daily dial-out account prerequisite when a package needs it. Region reaches the deploy manifest and the credential-store instructions, and the docs no longer claim a stance the project dropped.

**What is missing for the requester's goal.** The goal is a Pipecat Cloud telephony surface the requester can actually test: inbound, outbound, cold transfer, and eventually warm. Today that is blocked four ways:

| # | Gap | Why it bites |
|---|---|---|
| 1 | **The only phone leg is a Daily-provisioned number.** The requester cannot provision Daily numbers or Daily dial-out yet, and an operator holding a working carrier number should not need to buy a second number from a second vendor to test. | Every live proof from specs/004 stays out of reach. The LiveKit half of this repository is testable with a plain Twilio account; the Pipecat half is not. |
| 2 | **Outbound is not declarable on the Daily route.** `channels.phone` on it fails at build time because the route has no connection to dial with (specs/004 T046, verified by trying it). | The outbound flow cannot even be authored, let alone tested. |
| 3 | **There is no runbook.** specs/005 gives the LiveKit target one ordered telephony setup section with dictated carrier steps and a provisioning script. The Pipecat README has deploy instructions and a number-attachment pointer, not an end-to-end setup an operator can follow with nothing else open. | The zero-friction promise exists on one driver only. |
| 4 | **Warm transfer is not emitted on any Pipecat route.** Daily documents the pattern; this project does not build it yet (specs/004 FR-032, N34). | Warm stays a future feature. This spec must not make it harder. |

**This feature adds a carrier leg to the route that already works.** The agent code, the room, and the transfer machinery from specs/004 stay where they are. What this feature adds is the way in and the way out through the operator's own carrier, and the runbook that makes it settable from the README alone.

## The Choice: SIP Interconnect, Not Carrier Websockets

The requester asked for this comparison explicitly. Pipecat Cloud offers two ways to attach a carrier like Twilio, and they lead to different architectures.

**Carrier websocket endpoints.** Pipecat Cloud ships one websocket endpoint per carrier (`wss://{region}.api.pipecat.daily.co/ws/twilio`, and siblings for Telnyx, Plivo, Exotel). The carrier streams call audio straight to the agent. Inbound setup is very light: point the number at a TwiML Bin, no server anywhere.

**Daily SIP interconnect (`provider="daily"`).** The carrier forwards the call over SIP into the same Daily room the route already uses. Daily publishes one SIP configuration that works across carriers, per-room SIP addresses, static egress IPs for carrier allow-lists, and DTMF both ways. The room stays the audio bridge, exactly as it is for Daily-provisioned numbers.

This feature chooses the SIP interconnect. The reasons, in order of weight:

1. **The bot must not change per carrier.** On the websocket path each carrier is its own protocol in the agent (its own serializer, its own call-control API for anything beyond audio). On the SIP path the agent sees a Daily room participant whichever carrier carried the call, and Daily's own documentation states the bot code does not change when the provider is swapped. That is the requester's scalability requirement, verbatim.
2. **Transfers live in the room.** The cold transfer specs/004 shipped, and the warm transfer that is coming, both work on Daily room participants. A websocket call is not in a room, so choosing websockets would mean rebuilding both transfers per carrier against carrier APIs, and specs/004 already recorded that the websocket transports have no transfer control.
3. **It keeps a settled decision.** specs/004 decision 2 says Pipecat telephony is the Daily route, and only Daily. A carrier joining over SIP keeps that true: the carrier is where the number lives, not a new route shape. Managed carrier websockets would reopen everything specs/004 put out of scope with them (regional endpoints, websocket authentication).
4. **One mental model across both drivers.** The operator who followed specs/005 already owns a Twilio trunk with termination and credentials. The same account, trunk, and number serve the Pipecat target; what changes is where the number points.

The websocket path wins on exactly one axis: inbound with no server at all. The price of that axis is per-carrier bot code and no transfer story, which are the two things this feature exists to avoid. The cost the SIP path pays instead is one small call-forwarding helper for inbound, dictated below, and this spec treats keeping that helper small as a requirement rather than an accident.

## Decisions That Shape This Feature

Proposed on 2026-08-12 from the platform documents the requester supplied. Each states its reason so it can be vetoed cheaply before planning.

1. **Carriers join the existing Daily route over SIP.** The route key gains a carrier: `(pipecat, daily-sip, twilio)` is a new row in the capability rulebook, and `(pipecat, daily-sip)` with no carrier keeps meaning Daily-provisioned numbers, unchanged. See the comparison above.
2. **The YAML gains no new field.** The vocabulary already says everything this feature needs: `transport: daily-sip`, `carrier: twilio`, `connection:` naming a telephony Connection, `channels.phone`, and `destinations`. Selecting the carrier leg is combining existing fields, not adding one.
3. **Inbound needs a call-forwarding helper, and it is an emitted artifact.** Daily's SIP addresses are per room and rooms are created per call, so a carrier cannot forward to a static address the way it can to LiveKit's project SIP URI. Something must answer the carrier's webhook, start the agent through the platform's start API, hold the caller audibly, and forward the call once the room's SIP leg is ready. That something is emitted with the build, runs anywhere the operator chooses, and is the only new moving part. The deployed agent itself keeps no public endpoint, which keeps specs/004's shape rule for the agent intact; the helper is a setup-time artifact beside it, present only when a carrier is declared.
4. **The carrier seam is one forwarding action plus instruction text.** Forwarding a live call is the one thing done in the carrier's own words (Twilio updates the call by its identifier). Adding Telnyx or Plivo later means writing that one action and the carrier part of the runbook, and MUST NOT touch the agent, the room shape, or the platform part of the runbook. This is specs/005 story 3, applied to this driver.
5. **The runbook experience mirrors specs/005.** One ordered "Telephony setup" section in the generated README, a carrier part we dictate because we cannot do it for the operator, a platform part that is copy-paste commands, no identifier ever transcribed between steps, and the step counts stated up front.
6. **Warm transfer stays its own feature, and this architecture must carry it for free.** The carrier leg joins the same room a Daily-provisioned call joins, so when the warm feature lands it inherits carrier calls without new work. Nothing in this feature may special-case the carrier leg in a way warm would trip over.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A Twilio number reaches the deployed agent, from the README alone (Priority: P1)

An operator compiles a Pipecat package that names their carrier, deploys it following the README, opens the "Telephony setup" section, and finds two labeled parts: what to do at Twilio and what to run on our side. Following it top to bottom with nothing else open, they end with a working phone agent: a call to their Twilio number is answered by the deployed agent and a conversation follows.

**Why this priority**: This is the product promise and the requester's blocker. Every other story sits on a call getting through, and today no carrier call can reach a Pipecat agent at all.

**Independent Test**: From a clean compile, an operator with only a Twilio account, their existing number, and a Pipecat Cloud account follows the README section and places a call. No other document is consulted.

**Acceptance Scenarios**:

1. **Given** a fresh compile with `carrier: twilio` on the Daily route, **When** the operator reads the runbook, **Then** every carrier action is an exact console path or command, every platform action is a copy-paste command, and no step transcribes an identifier from one step into another.
2. **Given** the runbook is complete and the helper is running, **When** someone calls the number, **Then** they hear ringing or hold audio with no dead silence, and the agent answers and holds a conversation.
3. **Given** an inbound carrier call, **When** the room's SIP leg becomes ready more than once, **Then** the call is forwarded exactly once.
4. **Given** the helper is not reachable from the carrier, **When** someone calls, **Then** the runbook's troubleshooting note maps what the caller hears to the unreachable helper, and the helper's own startup names any missing value instead of starting half-configured.
5. **Given** a session that is not a phone call (the browser or console mode), **When** the agent starts, **Then** it behaves exactly as it does today.

---

### User Story 2 - The agent dials out through the operator's carrier (Priority: P2)

The author declares outbound calling on the Daily route with their carrier connection, compiles, and the deployed agent can place a call that rings a real phone, presenting the from-number the connection names.

**Why this priority**: Outbound is half of the requester's test matrix and is currently not even authorable (specs/004 T046). It is P2 only because inbound proves the architecture first.

**Independent Test**: Declare `channels.phone.outbound` with the carrier connection, deploy, trigger a call following the emitted instructions, and answer it.

**Acceptance Scenarios**:

1. **Given** a package declaring outbound on `(pipecat, daily-sip, twilio)`, **When** the author validates, **Then** the report accepts it and names every account prerequisite the dial path needs, on either side, before money is spent.
2. **Given** the same declaration without a connection, **When** the author validates, **Then** it fails naming the connection it needs, exactly as today.
3. **Given** a deployed outbound package, **When** a call is triggered following the emitted instructions, **Then** the target phone rings and the recipient sees the from-number the connection names.
4. **Given** the carrier rejects the SIP leg (a missing allow-list entry or credential), **When** the call is attempted, **Then** the failure surfaces with the cause named, never as a silently dead call.

---

### User Story 3 - Cold transfer is proven on a carrier call (Priority: P2)

On a live call that arrived through the operator's Twilio number, the caller asks for a person, hears a spoken handoff, and is connected to the destination while the agent leaves. Every failure path keeps the caller connected and informed, exactly as specs/004 shipped it.

**Why this priority**: Cold transfer is the outcome this line of work exists for, and it is already emitted; what is missing is a phone leg the requester can test it on. This story is the proof run, not new transfer code.

**Independent Test**: Run the transfer recipe end to end on a carrier-carried call, including the failure drill, and record the dated result in the transfer reference.

**Acceptance Scenarios**:

1. **Given** a live carrier call, **When** the caller asks for a person, **Then** the caller hears the handoff line, the destination is dialed, and on answer the caller and the destination are connected with the agent gone.
2. **Given** the destination does not answer or rejects, **When** the transfer fails, **Then** the caller is still connected and the agent says so and continues, per the declared failure behavior.
3. **Given** the caller asks twice, **When** the agent responds, **Then** at most one transfer is attempted.
4. **Given** the account lacks a permission the transfer's dial leg needs, **When** the author validated earlier, **Then** that permission was already named in the report, so the live failure is never the first mention.

---

### User Story 4 - A second carrier changes words, not shapes (Priority: P3)

When Telnyx or Plivo joins later, the agent code, the room shape, the emitted helper's structure, and the platform part of the runbook stay identical. What is written per carrier is the carrier part of the runbook and the one forwarding action done in the carrier's own words.

**Why this priority**: This is what "scalable" means here, and it costs nothing extra if stories 1 to 3 are built with the seam in the right place. It protects the setup from being rewritten per carrier, which is exactly what happened to the websocket routes.

**Independent Test**: Inspect where the Twilio-specific content lives: the carrier part of the runbook and the forwarding action are keyed off the carrier the target names; the platform part and the agent contain no carrier name.

**Acceptance Scenarios**:

1. **Given** the generated README, **When** the platform part of the runbook is read, **Then** it names no carrier and would be correct verbatim for any SIP-capable carrier.
2. **Given** the compiler sources, **When** a new carrier's forwarding action and instruction text are added, **Then** no emitted agent code changes and no platform-side instruction changes.

---

### Edge Cases

- **A non-phone session.** Browser and console sessions carry no call details and must keep working byte-identically, as specs/004 already pins.
- **A Daily-provisioned package.** A `daily-sip` target with no carrier keeps its exact current meaning, artifacts, and instructions. No helper is emitted for it.
- **The number still points at LiveKit.** An operator who did the specs/005 setup has the number's voice configuration on their SIP trunk. The runbook must say plainly that a number serves one target at a time and that switching is one carrier-side change, in both directions.
- **The caller waits while the agent boots.** The wait must be audible hold, never silence, and never a bare pause with a fixed short cap, which the platform documents as too short for room setup.
- **The SIP leg ready signal fires more than once.** The call is forwarded exactly once (story 1, scenario 3).
- **The helper dies between webhook and forward.** The caller hears the carrier's failure treatment rather than an eternal hold; the troubleshooting note maps this.
- **The carrier rejects Daily's SIP traffic.** Termination auth is wrong or the allow-list is missing; the failure names the cause (story 2, scenario 4).
- **The dial leg's account permission is missing.** Named by validate before the call, named again by the runtime failure (stories 2 and 3).
- **Two targets, one connection.** One package declaring the LiveKit sip target and the Pipecat carrier target sharing one telephony Connection must validate and compile; only the number's carrier-side pointing differs.

## Requirements *(mandatory)*

### Functional Requirements

**Authoring surface**

- **FR-001**: This feature MUST add no new authoring field. The carrier leg is selected with the existing target fields (`transport`, `carrier`, `connection`) and the existing phone channel, and a package that validates today MUST keep its exact meaning.
- **FR-002**: The route's Connection keys MUST reuse the existing telephony vocabulary wherever the meaning is identical (the four SIP names of N33, the carrier REST names the websocket routes already use). Any genuinely new key MUST land in the same change as a numbered dated `docs/SCHEMA.md` amendment, with the derived schemas, capability table, agreement tests, scaffold, console, examples, and `docs/user/` updated together, per the constitution.
- **FR-003**: `(pipecat, daily-sip)` with no carrier MUST keep meaning Daily-provisioned numbers, byte-identical output for existing packages, no helper emitted.

**Route and capability**

- **FR-004**: `(pipecat, daily-sip, twilio)` MUST become a row in the single capability rulebook supporting inbound, outbound, cold transfer, hangup, and the call sources the Daily route exposes. Each capability stays provisional until its dated live run, and no second copy of these facts may appear anywhere.
- **FR-005**: Declaring `channels.phone.outbound` on the Daily route with a carrier connection MUST validate and compile. Without a connection it MUST keep failing by the connection's name, exactly as today.
- **FR-006**: Warm transfer MUST stay gated on this route with the message stating that Daily documents the pattern and this project does not emit it yet. Nothing in this feature may special-case the carrier leg in a way that blocks the warm feature from riding it unchanged: the carrier call joins the same room shape a Daily-provisioned call joins.

**Generated behaviour**

- **FR-007**: A build whose route declares a carrier MUST emit the call-forwarding helper as an artifact beside the agent. The helper answers the carrier's webhook, starts the agent through the platform's start API, keeps the caller on audible hold from answer until the agent is connected, forwards the live call to the per-call SIP address exactly once however many ready signals fire, and refuses to start with any needed value missing, naming it. The deployed agent itself keeps no public endpoint.
- **FR-008**: The carrier-specific forwarding action MUST be the only carrier-specific code, keyed off the carrier the target names. The agent, the room configuration, and the helper's structure MUST be carrier-neutral.
- **FR-009**: Cold transfer MUST work on a carrier-carried call with the authored shape unchanged (`cold:`, `destinations`, failure behavior), keeping the at-most-one-attempt guard and every caller-stays-connected failure path from specs/004. Which side carries the transfer's dial leg (the platform's dial-out or the carrier trunk) is settled in planning, recorded with its source, and its account prerequisite named either way.
- **FR-010**: The validate report MUST name every account feature this route needs that is granted on request rather than by default, on either side of the interconnect, and MUST NOT name one the package does not need. A missing credential or permission at runtime MUST fail with the item named, never as a dropped or unanswered call.
- **FR-011**: No secret value may appear in any emitted file or document: environment variable names only, with the helper reading values at start the same way the agent does.

**Runbook**

- **FR-012**: The generated README for a carrier-declaring build MUST contain one ordered "Telephony setup" section with two labeled parts: the carrier part ("do this at your carrier; we cannot do it for you") dictating exact console paths or commands, and the platform part as copy-paste commands. No step may transcribe an identifier between steps, and the section states its step counts up front.
- **FR-013**: For Twilio, the carrier part MUST dictate: a voice-capable number; pointing the number's voice configuration at the running helper; the trunk termination and credentials or allow-list the outbound leg needs, including the platform's published static addresses where an allow-list is the mechanism; and any toggle the transfer's dial leg turns out to need, once planning settles which side carries it. An operator who completed the specs/005 setup MUST be told which of these they already have.
- **FR-014**: The platform part MUST be carrier-neutral, MUST complete with at most two commands after the environment file is filled and the agent is deployed, and MUST include how to expose the helper for a test call without owning public infrastructure.
- **FR-015**: The runbook MUST state that a number serves one target at a time and what to change, on which side, to move it between the LiveKit target and this one.

**Documents**

- **FR-016**: The authoring contract gains a numbered dated amendment recording the new route row and any Connection vocabulary this route adds. `docs/TRANSFERS.md`, `docs/TELEPHONY.md`, and the Pipecat target page under `docs/user/` MUST tell the same story as the emitted runbook, and the agreement tests between them MUST stay green.
- **FR-017**: Every platform behavior claim in emitted text and repository docs MUST carry its source and verification date, and any claim this spec lists as open MUST be verified during planning before code depends on it.

**Proof**

- **FR-018**: The automated suite MUST prove, without any phone account: the helper and agent artifacts are emitted exactly when a carrier is declared, the forwarding fires once against a double ready signal, the capability row and the emitted project and the docs agree, and every existing example compiles byte-identical.
- **FR-019**: An in-repository example MUST exercise `(pipecat, daily-sip, twilio)` with inbound, outbound, and a cold transfer, and be covered by the example tests.
- **FR-020**: The live runs (inbound answered, outbound rings, cold transfer completes and fails safe) are recorded with dates in this feature's task file, and the capability rows stop being provisional only on that evidence.

### Key Entities

- **Carrier interconnect**: the SIP leg between the operator's carrier trunk and the per-call room. Carrier-neutral on the platform side, carrier-owned on the number side.
- **Call-forwarding helper**: the emitted artifact that turns a carrier webhook into a started agent and a forwarded call. Present only when a carrier is declared; runs where the operator chooses.
- **Telephony Connection**: the existing YAML object naming carrier environment variables. This feature reuses it; planning settles the exact key set for this route.
- **Per-call SIP address**: the room-scoped address the platform mints for each call, the reason a static origination pointer cannot work here and the helper exists.
- **Route row**: `(pipecat, daily-sip, twilio)` in the single capability rulebook, the one home for what this route supports.
- **Runbook**: the README's telephony setup section. The carrier part varies by carrier; the platform part never does.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator holding a Twilio account, one number, and a Pipecat Cloud account goes from a clean compile to a live answered call using the generated README and nothing else. The run is recorded with dates in this feature's task file.
- **SC-002**: An operator who completed the specs/005 LiveKit setup reuses the same carrier account, trunk, and number for this target by changing where the number points and adding at most three new environment values.
- **SC-003**: The carrier part of the runbook has at most six actions and the platform part at most two commands, both counts stated in the runbook, with zero identifiers transcribed between steps.
- **SC-004**: A caller never hears more than 2 seconds of silence between dialing and either ringing, hold audio, or the agent, measured on the recorded live runs.
- **SC-005**: Cold transfer on a carrier call completes on at least 9 of 10 consecutive attempts, and on every failure path the caller is still connected and informed, 10 of 10.
- **SC-006**: Every account prerequisite of the route is named by validate before any live call, 10 of 10 cases across the surface.
- **SC-007**: A search of a fresh carrier build shows the carrier's name only in the carrier part of the runbook and the one forwarding action; a fresh no-carrier Daily build is byte-identical to today's.
- **SC-008**: The full default test suite passes with zero Python, and every golden change is read and accepted deliberately.
- **SC-009**: A reader who has never used Daily or Pipecat Cloud can say from the README alone which steps happen at the carrier, which on the platform, and what the helper is for, checked by the plain-language review CLAUDE.md already requires.

## Platform Facts This Feature Depends On

Constitution principle IV: every claim carries its source and the date we read it. All sources below were supplied by the requester and read on 2026-08-12.

| # | Fact | Source | Verified |
|---|------|--------|----------|
| V1 | Daily SIP with `provider="daily"` connects Daily's SIP leg to any SIP-capable carrier; one SIP configuration works across carriers and the bot code does not change when the carrier is swapped; static egress addresses are published for carrier allow-lists, with signaling and media listed separately. | docs.pipecat.ai/pipecat/telephony/daily-sip | 2026-08-12 |
| V2 | Daily SIP addresses are per room, `sip:$roomName.$index@$domainName.sip-us.daily.co`, so inbound needs a webhook flow: create the room, start the bot, hold the caller, forward the live call when the SIP leg is ready. | docs.pipecat.ai/pipecat/telephony/daily-sip | 2026-08-12 |
| V3 | The Twilio inbound flow is: webhook to the operator's server, room created with SIP enabled, hold music in the webhook response (a bare pause is documented as too short), then on the ready event the live call is updated by its identifier to dial the SIP address. | docs.pipecat.ai/pipecat/telephony/twilio-daily-sip | 2026-08-12 |
| V4 | On Pipecat Cloud the webhook server calls the platform's public start endpoint with a room request and a body the agent receives as arguments, instead of spawning a local process. | docs.pipecat.ai/pipecat/telephony/twilio-daily-sip (deployment section) | 2026-08-12 |
| V5 | Dial-out: the room is created with dial-out enabled, and the bot starts the call with a SIP address pointing at the carrier, `provider="daily"`, with connected, stopped, and warning events to observe. | docs.pipecat.ai/pipecat/telephony/daily-sip | 2026-08-12 |
| V6 | The ready signal can fire more than once; the documented practice is a forward-once guard. | docs.pipecat.ai/pipecat/telephony/daily-sip | 2026-08-12 |
| V7 | DTMF receive and send are documented on the `provider="daily"` leg. | docs.pipecat.ai/pipecat/telephony/daily-sip | 2026-08-12 |
| V8 | Daily dial-out is a paid per-domain feature granted on request; international is enabled separately. | rulebook entry, docs.pipecat.ai/pipecat-cloud/guides/telephony/daily-dial-out | 2026-08-12 |

**Open facts planning must verify before code depends on them:**

- Whether the platform's start endpoint accepts the `provider="daily"` SIP room configuration directly (the deployment example shows a SIP room request without that field).
- Whether the transfer and outbound dial legs on a carrier interconnect need the Daily dial-out permission (V8), the carrier trunk, or either, and what the recipient sees as caller identity on each.
- Whether the outbound SIP leg authenticates to the carrier with credentials, with the published static addresses on an allow-list, or both, which decides this route's exact Connection key set (FR-002).
- Whether the existing cold transfer primitive behaves identically on a carrier-forwarded leg.

## Assumptions

- The operator has a Twilio account with a voice-capable number, and may already hold the trunk from the specs/005 setup. Nothing here buys numbers or creates carrier resources for them, per the telephony boundary.
- The helper runs where the operator chooses. Testing uses a local run exposed by a tunnel, dictated in the runbook; production hosting (public ingress, TLS) stays with the operator, per the constitution.
- One phone number serves one target at a time. Moving it is a carrier-side change the runbook dictates.
- Daily-provisioned numbers remain the zero-infrastructure inbound path, untouched.
- Warm transfer emission is its own feature, with the requester-supplied flows example as a starting point; this feature only guarantees the carrier leg will not block it (FR-006).
- Live call proof stays manual and is recorded as route evidence, as everywhere else in this repository.

## Out Of Scope

- **Emitting warm transfer.** Its own feature, as specs/004 already decided. This feature's obligation is FR-006: the carrier leg must carry it for free when it lands.
- **Telnyx, Plivo, or Exotel instruction content and forwarding actions.** The structure must be ready (story 4); the words and the one action per carrier come with their connections.
- **Local `unmute dev --telephony` on this route.** The refusal specs/004 shipped stays, and its message must stay accurate once this route exists. A local flow mirroring the LiveKit zero-step direction is its own feature.
- **Retiring the self-hosted carrier websocket routes.** Still a separate decision nobody has made.
- **Regional websocket endpoints and websocket authentication.** Properties of the websocket routes this feature deliberately did not choose; they stay out with them.
- **Automating carrier configuration.** The CLI never calls the carrier's API at compile or setup time; carrier steps are dictated, not performed. The helper's one forwarding action at call time is the recorded exception, and it acts only on a call the carrier just delivered to it.
- **Choosing the outbound caller identity beyond the connection's from-number.** Same boundary as specs/004 drew.

## Dependencies

- A Pipecat Cloud account and a deployed agent to receive the forwarded calls.
- A Twilio account with a voice-capable number, and whatever trunk pieces planning confirms the outbound leg needs.
- Two reachable phone numbers for the caller and the transfer destination drills.
- specs/004's shipped Daily route, whose agent, transfer machinery, and prerequisite reporting this feature extends rather than replaces.
- specs/005's runbook contract, whose shape this feature mirrors on the second driver.
