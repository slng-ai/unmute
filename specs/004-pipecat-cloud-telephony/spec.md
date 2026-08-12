# Feature Specification: Pipecat Cloud telephony on the Daily route

**Feature Branch**: `worktree-pipecat-verifications-trasfer-human-v2` (based on `feature/warm-cold-human-transfer`)

**Created**: 2026-08-12

**Status**: Draft

**Input**: User description: "Pipecat Cloud telephony: the yaml authoring surface and the setup nuances for Daily dial-in, Daily dial-out, regional websocket endpoints, and websocket authentication. There are many nuances involved in the telephony set up that need careful handling, especially because all this ties to the yaml definition, that should be clearly defined." Followed by: "The only way this is supported for now is either Pipecat Cloud or LiveKit Cloud. The only thing that can change is Pipecat support for SIP, that now will be only Daily."

## Decisions That Shape This Feature

Settled with the requester on 2026-08-12.

1. **The managed clouds are the supported deployment.** Pipecat Cloud for the Pipecat driver, LiveKit Cloud for the LiveKit driver. There is no second hosting model to choose between.
2. **Pipecat telephony is the Daily route, and only Daily.** That is the one dimension expected to change, and it has now changed to Daily.
3. **The yaml gains no field for either of the above.** Because there is one hosting model per driver and one Pipecat telephony route, there is nothing for an author to select. The facts belong in the compiler, the validate report, and the emitted instructions, not in new authoring surface.
4. **Warm transfer on Daily is real and will be emitted, in feature 005.** Added 2026-08-12, see Clarifications. Daily documents cold and warm; the earlier claim that Pipecat has no warm primitive was wrong. Warm is admitted as a Daily-only dated exception to the rule that generated code never owns the call's audio path, and N30's outbound requirement is scoped to routes that have a phone channel. This feature corrects the false claim (FR-032) and emits nothing new.

Decision 3 is the answer to the two questions this spec originally opened: no `hosting:` field, and no new telephony channel on the Daily route. The gaps those questions were trying to close are real, but they close by making the compiler carry the facts rather than by asking the author to restate them.

## This Reverses A Recorded Stance

Decision 1 contradicts two documents in the repository, both of which say the opposite in plain terms:

- `docs/DEPLOYMENT.md` opens with "**Status: Adopted stance, July 23, 2026. For now, Unmute deployments do not use LiveKit Cloud or Pipecat Cloud.** Everything below runs on infrastructure you control," and states "the supported path is self-hosted."
- `docs/TELEPHONY.md` states the design "supports local and self-hosted deployments for Pipecat and LiveKit **without requiring Pipecat Cloud or LiveKit Cloud**."

The code has already drifted the other way: every emitted Pipecat project carries a "Deploy to Pipecat Cloud" section, and `specs/001-livekit-cloud-deploy` exists. So decision 1 largely ratifies what the artifacts already do. It is still a reversal of a dated, adopted stance, and the repository rule is that the document wins until the document is changed. Correcting both documents is therefore part of this feature, not a follow-up. The constitution says nothing about hosting, so no amendment is needed there.

Both corrections were confirmed on 2026-08-12, with one distinction the requester drew that the documents must keep: **running locally still requires no cloud account, and remote deployment is where the managed clouds are the supported path.** So `docs/TELEPHONY.md` is not simply wrong. Its cloud-free claim was about local runs, and it reads as a claim about deployment. The correction is to separate the two rather than to delete the sentence, so `unmute dev` keeps its documented cloud-free local story while deployment points at the managed clouds.

One consequence worth stating before it surprises anyone: the self-hosted carrier websocket routes for Twilio, Telnyx, and Plivo exist to serve the goal `docs/TELEPHONY.md` just lost. They generate a web application with its own inbound webhook, its own media endpoint, and Redis. That shape has no home on Pipecat Cloud, which starts agents through its own entry point rather than by exposing arbitrary HTTP endpoints. This feature does not delete those routes, and says so as an explicit assumption, because deleting working routes is a separate decision the requester has not made.

## Where We Stand Today

Read from the code on the `feature/warm-cold-human-transfer` base, and checked against the Pipecat Cloud documentation on 2026-08-12.

**What already works.** `unmute validate examples/human-transfer-daily` reports `✓ pipecat`, no errors, no warnings. `unmute compile` emits a complete project. The cold transfer code announces to the caller, calls the Daily transfer primitive, reads the error that primitive returns rather than assuming it raises, and hands the model a failure result telling it to say so and keep helping the caller. The emitted deploy instructions get the ordering right: credentials before deploy, because the manifest already names them. The declared region reaches the deploy manifest.

**What is missing or wrong.**

| # | Problem | Why it bites |
|---|---|---|
| 1 | **An inbound Daily call fails before the agent answers.** The compiled project builds its audio path with a parameter object that cannot accept inbound call details. The framework fills those details in for a phone call, and the object rejects them. | This stops the transfer recipe at the step where a person calls the number. Every existing test stays green, because the framework's fill-in step is documented as doing nothing when there are no call details, and no test runs an inbound call. |
| 2 | **The account permission that cold transfer depends on is never mentioned.** Cold transfer dials the destination, which needs outbound calling enabled on the phone account. The provider grants that on request, as a paid feature, per domain. Nothing in the package or the report says so. | The first a author learns of it is a transfer that does not connect on a live call, with per-minute charges already running. |
| 3 | **Region is declared but disconnected.** The region reaches the deploy manifest, and nothing else. Credential stores are per-region with names unique across the whole platform, and an agent cannot read a store in another region. | A mismatch is a silent failure, not an error. The platform's own guidance is that the agent and the endpoint it serves must be in the same region. |
| 4 | **Two documents state a deployment stance the project no longer holds.** | An author reading `docs/DEPLOYMENT.md` sets up the wrong infrastructure and does not find out until nothing answers. |

Problem 1 is a defect with a known cause. Problems 2 to 4 are the compiler and the documents failing to carry facts the author needs.

## Clarifications

### Session 2026-08-12

- Q: Daily warm transfer needs the generated bot to own audio control (a state machine, a hold-music mixer, two audio gates), which contradicts the rule that generated code makes one platform call and never owns the audio path. How should we admit it? → A: As a Daily-only dated exception. The rule still holds everywhere else, so the carrier websocket routes stay transfer-free. Justification: Daily's room is the audio bridge and the processors only gate inside a pipeline we already own, which is a different risk class from bridging two carrier sockets.
- Q: SCHEMA N30 requires `channels.phone.outbound: true` for any warm transfer, but the Daily route has no phone channel. How should that be resolved? → A: Amend N30's scope so its requirement applies only to routes that have a phone channel. On Daily the compiler derives the outbound need and names Daily's dial-out prerequisite instead. FR-002 stands and `capacity` stays out.
- Q: Where should the Daily warm transfer work live? → A: Its own feature, `specs/004`. This feature keeps its scope, and warm cannot be tested until a Daily call connects at all, which is what this feature fixes.

**What this corrected.** An earlier version of this spec asserted that Pipecat has no warm-transfer primitive on any route. That is **wrong**. `docs.pipecat.ai/pipecat/telephony/daily-pstn`, verified 2026-08-12, states "Daily supports two primary transfer patterns", cold and warm. The base branch's engineering judgement was sound (the pattern does require owning audio control) but its factual claim was not. Correcting the repository's own statement of that fact is now in scope here as FR-032, because a false capability claim outranks code under Principle IV. Emitting warm is not in scope here; it is feature 005.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A Daily phone agent answers a real call (Priority: P1)

An author declares a Daily phone agent, compiles it, deploys it to the managed cloud, attaches a number, and a caller gets through and holds a conversation.

**Why this priority**: Problem 1 stops this today, and every other story sits on it. It is also the smallest slice that proves the managed path works.

**Independent Test**: Author a Daily package, compile, deploy, attach a number, call it, talk. Delivers a working hosted phone agent on its own.

**Acceptance Scenarios**:

1. **Given** a Daily package, **When** the author compiles and deploys it following only the emitted instructions, **Then** the deployment reports itself ready.
2. **Given** the deployed agent with a number attached, **When** a person calls, **Then** the agent answers and holds a conversation.
3. **Given** an inbound phone call, **When** the agent builds its audio path, **Then** it accepts the call's details and does not fail before answering.
4. **Given** a session that is not a phone call, **When** the agent starts, **Then** it behaves exactly as it does today, because such a session carries no call details.
5. **Given** a package declaring a capability the Daily route does not have, **When** the author validates, **Then** the report fails and names both the capability and the route.

---

### User Story 2 - Cold transfer completes on the managed Daily route (Priority: P1)

The agent hands a caller to a person and leaves. The caller and the person keep talking. On every way that can fail, the caller stays connected and is told what happened.

**Why this priority**: This is the outcome the whole line of work exists for, and the transfer reference already records it as unproven until its recipe has been run. Story 1 is only the precondition.

**Independent Test**: Run the recipe in the transfer reference end to end, including the failure drill. Delivers the proven cold transfer on its own.

**Acceptance Scenarios**:

1. **Given** a live call where the caller asks for a person, **When** the agent transfers, **Then** the caller hears a spoken handoff line before the connection changes.
2. **Given** the destination answers, **When** the transfer completes, **Then** the caller and the person are connected and the agent is gone, and the call does not drop.
3. **Given** the destination does not answer, rejects, or is undialable, **When** the transfer fails, **Then** the caller is still connected and the agent says so and keeps helping, or ends with a goodbye, according to what the package declared.
4. **Given** the caller asks twice, **When** the agent responds, **Then** at most one transfer is attempted.
5. **Given** the run is complete, **When** the transfer reference is read, **Then** it records the dated result and any step the run proved wrong.

---

### User Story 3 - Prerequisites are named before a call is placed (Priority: P2)

Before spending money on a live call, the author is told every account feature the route needs that the provider does not grant by default.

**Why this priority**: Problem 2 turns a known, documented prerequisite into a mystery failure on a paid call. Naming it costs one line in a report and saves a debugging session. It is P2 only because story 1 has to work first for the prerequisite to matter.

**Independent Test**: Validate a Daily package with a cold transfer and confirm the report names the outbound permission, without any account existing. Delivers the warning on its own.

**Acceptance Scenarios**:

1. **Given** a package with a cold transfer on the Daily route, **When** the author validates, **Then** the report names the outbound calling permission as a prerequisite and states that the provider grants it on request.
2. **Given** a package with no transfer and no outbound calling, **When** the author validates, **Then** the report does not name a permission the package does not need.
3. **Given** a missing credential or a missing permission at runtime, **When** the agent starts or attempts the action, **Then** it fails with the missing item named, never as a silently dropped or unanswered call.
4. **Given** any refusal from validate across this surface, **When** the author reads it, **Then** it names the fix, either the routes that offer the capability or the field to change.

---

### User Story 4 - Region is declared once and everything follows it (Priority: P2)

The author names a region once. The deployment, the credential store, and the emitted instructions all agree, and a disagreement is impossible to ship.

**Why this priority**: A region mismatch produces no error at all, just an agent that cannot read its own credentials. It is the most likely silent misconfiguration in the setup and it is entirely preventable before deployment. P2 because the default region is fine for the first proof.

**Independent Test**: Compile for a non-default region and confirm every emitted reference agrees. Delivers a correct multi-region setup on its own.

**Acceptance Scenarios**:

1. **Given** a package naming a non-default region, **When** it is compiled, **Then** the deploy manifest and the credential-store instructions both name that region.
2. **Given** a package naming no region, **When** it is compiled, **Then** the instructions state which region applies by default and that the credential store follows the same default.
3. **Given** any compiled project, **When** the instructions are read, **Then** they state that credential stores are per-region and that their names are unique across the whole platform.
4. **Given** a region value the platform may not offer, **When** the author validates, **Then** the report states the value is forwarded as declared and names the command that lists the real ones.

---

### User Story 5 - The agent places outbound calls on Daily (Priority: P3)

The author declares that the agent may place calls, and the compiled project can dial a number.

**Why this priority**: Outbound shares its account prerequisite with cold transfer, so the plumbing overlaps. It is P3 because the transfer recipe never requires the agent to originate a call.

**Scope correction (2026-08-12).** This story originally also let the author choose the caller identity the recipient sees. That is a **new authoring field**, and the constitution requires an authoring-surface change to land in one change together with a numbered `docs/SCHEMA.md` amendment, updated derived schemas, a capability row, agreement tests, scaffold templates, the interactive console, in-repository examples, and `docs/user/`. That is a feature of its own, not a P3 add-on, so it moved to Out of Scope. Without it the recipient sees whatever the provider picks, and the emitted instructions have to say so.

**Independent Test**: Declare an outbound Daily agent, deploy, trigger a call, confirm it reaches the target. Delivers outbound calling on its own.

**Acceptance Scenarios**:

1. **Given** a package declaring outbound calling on Daily, **When** the author validates, **Then** the report accepts it and names the account permission it needs.
2. **Given** that package, **When** it is compiled, **Then** the instructions describe how a call is started and state what identity the recipient sees, given the package cannot choose one.
3. **Given** an account with domestic but not international permission, **When** an international call is attempted, **Then** the failure names the missing permission.

---

### Edge Cases

- An inbound session arrives with no call details, which is how every non-phone session arrives.
- The region in the package disagrees with the region of a credential store that already exists.
- A credential store name is already taken by another organisation, because names are globally unique.
- Two targets in one package name different regions.
- A number is attached for an agent that was never deployed, or was deleted.
- The account has outbound permission for domestic calls but not international ones.
- A caller identity is declared that the account does not own.
- The agent is at its declared session capacity when a call arrives.
- An existing self-hosted package is compiled unchanged after the stance reversal.
- The transfer destination is undialable.

## Requirements *(mandatory)*

### Functional Requirements

**Authoring surface**

- **FR-001**: This feature MUST NOT add a field for choosing a hosting model, because there is one supported hosting model per driver.
- **FR-002**: This feature MUST NOT add a telephony channel to the Daily route. Direction, controls, and prerequisites for that route MUST be derived by the compiler from what the package already declares.
- **FR-003**: This feature adds no authoring field at all. If that changes, the new field MUST land in one change together with everything the constitution's compliance review requires: a numbered dated `docs/SCHEMA.md` amendment, the derived schemas, the capability table, the agreement tests, the scaffold templates, the interactive console, the in-repository examples, and `docs/user/`.
- **FR-004**: A package that validates today MUST either keep its exact previous meaning or fail with a message naming what to change. It MUST NOT silently change region or deployment behaviour.

**Generated behaviour**

- **FR-005**: A compiled Daily project MUST build an audio path that accepts inbound phone call details, and MUST NOT fail before answering.
- **FR-006**: A compiled project MUST behave exactly as it does today on sessions that carry no call details.
- **FR-007**: The emitted instructions MUST tell the author where to attach a phone number and in what order to perform the deployment steps.
- **FR-008**: A compiled project MUST attempt at most one transfer per call, however many times the agent asks, and MUST leave the caller connected and informed on every transfer failure path. This MUST hold on the Daily route, where the shared control store the other routes use for it is unavailable by design (FR-027).
- **FR-027**: The two route shapes MUST stay distinct in the emitted project. A Daily project MUST declare no service and no public endpoint of its own. A carrier websocket project MUST keep its current services, endpoints, and credentials. Neither shape's credentials MUST appear in the other. No transfer tool MUST be emitted on any carrier websocket route.

**Prerequisites and refusals**

- **FR-009**: The validate report MUST name every account feature the selected route needs that the provider grants only on request, including the outbound calling permission that cold transfer depends on.
- **FR-010**: The report MUST NOT name a prerequisite the package does not need.
- **FR-011**: A missing credential or permission MUST surface as a named failure at startup or at the point of use, never as a dropped or unanswered call.
- **FR-012**: Every refusal across this surface MUST name the fix, not only the problem.
- **FR-028**: `unmute dev --telephony` MUST refuse on the Daily route, naming the route, naming the modes that do work on it, and pointing at the deploy path for a real phone call. It MUST NOT be accepted as a silent no-op, and it MUST NOT claim telephony is unsupported on the route, which would be false.

**Region**

- **FR-013**: The region MUST be declared once and MUST reach every place that depends on it: the deploy manifest and the credential-store instructions.
- **FR-014**: When no region is declared, the emitted instructions MUST state which region applies by default and that the credential store follows the same default.
- **FR-015**: The emitted instructions MUST state that credential stores are per-region and that their names are unique across the whole platform, because both facts break a deployment in ways that do not say which side is wrong.

**Documents**

- **FR-016**: `docs/DEPLOYMENT.md` MUST be corrected so its adopted stance matches the managed clouds being the supported deployment, with the reversal dated.
- **FR-017**: `docs/TELEPHONY.md` MUST be corrected so its cloud-free claim is scoped to local runs and no longer reads as a claim about deployment. It MUST NOT drop the local claim, which stays true.
- **FR-018**: The documents MUST keep local runs and remote deployment distinguishable: running locally requires no cloud account, and remote deployment uses the managed clouds. A reader MUST be able to tell which of the two any statement is about.
- **FR-019**: The transfer reference MUST be updated to match anything this feature changes, and MUST record the dated result of the run that proves the Daily cold transfer.
- **FR-020**: The authoring contract MUST state that Pipecat telephony is the Daily route, so a reader learns it without reading a generated project.
- **FR-029**: The transfer reference's Status section MUST NOT claim a proof state that has not happened. The LiveKit rows MUST either record their dated run results or stay explicitly provisional.
- **FR-032**: No document in the repository MUST claim a platform lacks a capability it documents. Specifically, the statement that Pipecat has no warm-transfer primitive on any route MUST be corrected to say that Daily documents warm transfer and this project does not emit it yet, with the reason (it requires the generated bot to own audio control) and the verification date. The correction MUST NOT change any route tag, any capability, or any authoring field, because emitting warm belongs to a separate feature.

**Proof**

- **FR-021**: The automated suite MUST prove a compiled Daily project accepts inbound phone call details, without needing any phone account.
- **FR-022**: The automated suite MUST prove every emitted reference to a region agrees.
- **FR-023**: The automated suite MUST prove the prerequisite appears when the package needs it and is absent when it does not.
- **FR-024**: The automated suite MUST fail if the generated projects, the capability report, and the authoring contract disagree about what a route supports.
- **FR-025**: An authored example package MUST exercise the managed Daily route with a cold transfer and MUST be covered by the example tests.
- **FR-026**: The full default test suite MUST pass.
- **FR-030**: The automated suite MUST prove the two "MUST NOT add" rules in FR-001 and FR-002 by asserting against the derived authoring schema, so a later change that adds a hosting field or a Daily telephony channel fails a test rather than arriving quietly.
- **FR-031**: The automated suite MUST prove FR-004 across every in-repository example, comparing emitted output before and after this feature.

### Key Entities

- **Route**: the exact combination of orchestrator, transport, and carrier a target selects. Capability is a property of the route, never of the orchestrator brand.
- **Region**: where the agent runs. It constrains which credential store the agent can read.
- **Credential store**: the named set of secrets the deployed agent reads. Per-region, with a name unique across the whole platform.
- **Account prerequisite**: a platform feature the provider grants on request rather than by default, which the route cannot work without.
- **Transfer attempt guard**: the per-call record that a transfer has already been attempted, so a second request is refused. On the carrier routes this lives in the shared control store; on the Daily route there is no such store, so it is in-process for the life of the call.
- **Route evidence**: the recorded proof behind each supported capability, including whether a live call has confirmed it.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An author can go from an empty directory to a deployed phone agent that answers a real call by following only the emitted instructions, with no reference to this repository's source.
- **SC-002**: A live cold transfer completes on the managed Daily route on at least 9 of 10 consecutive attempts.
- **SC-003**: On every transfer failure path, the caller is still connected and has been told what happened, in 10 of 10 attempts. No failure path drops a caller silently.
- **SC-004**: Every account prerequisite the route needs is named by validate before any money is spent on a live call, in 10 of 10 cases across the transfer surface.
- **SC-005**: A region change is a one-line edit and every emitted reference follows it, verified by compiling for each supported region.
- **SC-006**: Reading only the authoring contract, an author can tell which Pipecat telephony route exists and what it supports, without reading a generated project.
- **SC-007**: No document in the repository states a deployment stance the project does not hold, and every cloud-free claim that remains is explicitly scoped to local runs.
- **SC-008**: Every package that validated before this feature either behaves identically or fails with a message naming what to change, in 10 of 10 cases across the existing examples.
- **SC-009**: The default test suite passes with zero failures, and the new checks fail if the inbound audio path or the region agreement regresses.
- **SC-010**: Every capability this repository claims on a route has a dated live call record behind it.
- **SC-011**: The caller hears something within 2 seconds of asking for a person, so the handoff never feels like a dropped call.
- **SC-012**: A second transfer request in the same call never produces a second attempt, in 10 of 10 attempts.
- **SC-013**: No document in the repository claims a platform lacks a capability that platform documents. Every "not supported" statement about a provider says whether it means the platform cannot do it or this project does not emit it yet, and carries a verification date.

## Out of Scope

- **Choosing the caller identity an outbound recipient sees.** Moved out of User Story 5 on 2026-08-12. It is a new authoring field, and the constitution requires such a change to land with a SCHEMA amendment, derived schemas, a capability row, agreement tests, scaffold templates, the interactive console, examples, and `docs/user/` in the same change. That is its own feature. Until it lands, the recipient sees whatever the provider picks and the instructions say so.
- **Emitting warm transfer on the Daily route.** Daily does support warm transfer, verified against the documentation on 2026-08-12, and this project will emit it. It is **feature 005**, not this one, because it needs four custom audio processors, a hold-music asset, a new numbered amendment superseding N31's warm clause, and a live run with hold-audio and private-briefing drills. It also cannot be tested until a Daily call connects at all, which is what this feature fixes. Correcting the repository's false claim that Pipecat has no warm primitive **is** in scope here, as FR-032.
- **Warm transfer on the carrier websocket routes.** Those transports carry media only and the framework documents no transfer control on them. The rule that generated code never owns the audio path continues to hold there, unchanged.
- **Regional websocket endpoints and websocket authentication.** Both are properties of the carrier websocket routes. With Pipecat telephony being Daily only, and Daily using neither, they have nothing to apply to. They return to scope only if a managed carrier websocket route is ever supported. Flagged explicitly because the requester linked both documents, and this is the one place where a linked source did not turn into a requirement.
- **Retiring the self-hosted carrier websocket routes.** They no longer serve a stated goal, and their shape does not fit the managed cloud, but deleting working routes is its own decision.
- **New carriers, and bringing an existing carrier number in over SIP** as the inbound leg of the Daily route.
- **Running the LiveKit transfer recipes.** Those runs are separate work. Correcting their Status rows so the document does not overclaim is in scope, as FR-029.
- **Making the public-example test git-based.** The constitution requires a repository hygiene rule to be written against `git` rather than the working tree, so that compiling an example locally cannot turn the suite red. `internal/generate/examples_test.go` enumerates directories on disk and violates this, which is what turned the suite red earlier in this work. It is a pre-existing defect unrelated to telephony, recorded here so it is tracked rather than rediscovered.

## Assumptions

- **Daily-provisioned numbers are the inbound leg** of the Daily route.
- **Cold transfer depends on the outbound calling permission**, because it dials the destination. It is a prerequisite of cold transfer in its own right, not only of declared outbound calling.
- **The self-hosted carrier websocket routes stay as they are** for now, neither promoted nor deleted. If the requester wants them retired, that is a separate decision and a breaking change.
- **The default region applies when none is declared**, and the credential store defaults the same way, so an author who names no region gets a consistent pair.
- **Region values stay a free provider vocabulary**, forwarded as declared, because the platform is the real validator and its region list changes outside our release cycle.
- **Live call proof stays manual.** Nothing automated places a phone call. Live results are recorded as route evidence.

## Dependencies

- A Pipecat Cloud account, a Daily account with outbound calling granted, and two reachable phone numbers for the caller and the transfer destination.
- The base branch's transfer work, which owns the `cold:` and `warm:` authoring shape this feature does not change.
- The transfer reference document, which owns the recipe and the Status record this feature updates.
