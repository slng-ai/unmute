# Feature Specification: Dial out with the carrier's own SIP credentials

**Feature Branch**: `feature/warm-cold-human-transfer` (feature dir `002-inline-sip-trunk`)

**Created**: 2026-08-12

**Status**: Draft

**Input**: User description: "Let a LiveKit package dial out through its own carrier SIP credentials, with no LiveKit stored outbound trunk. The emitted warm transfer and outbound call both depend on a trunk the operator registers by hand, whose ID only the platform can assign. The documented alternative passes the trunk's hostname and credentials inline, and those are exactly the values a Connection already declares. Make inline the way a generated package dials out, decide deliberately whether the stored-trunk path stays, keep cold transfer out of scope, and update the documents where the trunk step stops being required."

## Why this exists

A compiled package cannot dial anybody until a human has run a command that this repository never mentions in the same breath as the package.

Today both outbound paths in the emitted LiveKit project reach the carrier through a **stored outbound trunk**: an object the operator creates with the platform CLI, whose ID the platform assigns. The warm transfer picks that ID up from `LIVEKIT_SIP_OUTBOUND_TRUNK` through the prebuilt's documented environment fallback, and the outbound-call path reads the same name explicitly. Neither value can come from the carrier, so neither can come from the package. The result is a build directory that looks complete, deploys cleanly, and then fails on the first transfer because a separate registration step was never run.

The platform documents an alternative that removes the step: pass the trunk's hostname and credentials **inline** with the call. What makes this worth doing rather than merely possible is that a Connection already declares every value the inline form needs, for whichever carrier it names. The package is not missing information. It is throwing information away and then asking the operator to re-supply it in a shape only the platform can mint.

Two consequences follow. First, a fresh compile becomes dialable with nothing but the carrier credentials the operator already holds. Second, the same emitted code works for any carrier the Connection vocabulary covers, instead of only for a trunk somebody registered, which is what makes the transfer story portable rather than Twilio-shaped by accident.

## Verified platform contract *(source of truth for this feature)*

Verified 2026-08-12 against live documentation. Every row carries its source, per Constitution principle IV.

| Fact | Source |
|---|---|
| The warm-transfer prebuilt takes **either** a stored trunk ID **or** inline trunk configuration, and one of the two is required. Inline carries a hostname, an auth username, and an auth password. | [WarmTransferTask](https://docs.livekit.io/agents/prebuilt/tasks/warm-transfer/) |
| The stored-trunk argument has a documented **environment fallback**, which is the only reason the emitted warm transfer works today while passing no trunk at all. | [WarmTransferTask](https://docs.livekit.io/agents/prebuilt/tasks/warm-transfer/) |
| The outbound-participant API takes the same inline configuration, and with it a **from-number becomes required**, because there is no stored trunk whose number can be the default. That argument also has its own environment fallback. | [Agent-assisted transfer](https://docs.livekit.io/telephony/features/transfers/warm/), [WarmTransferTask](https://docs.livekit.io/agents/prebuilt/tasks/warm-transfer/) |
| A stored outbound trunk is **not required**: inline configuration is offered as the alternative for quick setup or when trunk settings vary per call. | [Outbound trunk](https://docs.livekit.io/telephony/making-calls/outbound-trunk/) |
| Trunks are long-lived objects the platform **caches and reuses**. Creating one per call is documented as harmful at scale. Inline configuration is not a per-call trunk object, but this is the reason to keep the stored path reachable rather than delete it. | [Outbound trunk](https://docs.livekit.io/telephony/making-calls/outbound-trunk/) |
| A stored trunk can pin outbound calls to a region by destination country. Inline configuration exposes hostname, transport and credentials. **Region pinning is the capability the stored path keeps.** | [Outbound trunk](https://docs.livekit.io/telephony/making-calls/outbound-trunk/) |
| Cold transfer acts on the caller's existing leg through SIP REFER and needs **no outbound trunk of any kind**. | [Call forwarding](https://docs.livekit.io/telephony/features/transfers/cold/) |

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A warm transfer dials with carrier credentials alone (Priority: P1)

Someone compiles a package whose Connection names a carrier and its SIP credentials, deploys it, asks the agent for a manager, and the manager's phone rings. They never registered anything on the platform side.

**Why this priority**: It is the whole feature. It is also the shortest path to a warm transfer anybody can reproduce, because the credentials come from the carrier account the package already needs.

**Independent Test**: In an account with no stored outbound trunk, compile `examples/human-transfer`, deploy, and request a manager in the Agent Console.

**Acceptance Scenarios**:

1. **Given** a package whose Connection declares a SIP address, username, password and number, **When** it is compiled, **Then** the emitted warm transfer carries the trunk's hostname and credentials by name, and requires no platform-assigned trunk identity.
2. **Given** that package deployed with only carrier credentials set, **When** the caller asks for a manager, **Then** the manager's phone rings and the transfer completes.
3. **Given** any emitted file, **When** it is inspected, **Then** it names the credential environment variables and contains no credential value.

---

### User Story 2 - An outbound call dials the same way (Priority: P2)

The same package places an outbound call without a stored trunk, because both dial-out paths read the same configuration.

**Why this priority**: Leaving the outbound path on the stored trunk would keep the registration step mandatory for any package with an outbound channel, which is most of the reason the step exists today. Second only because the warm transfer is what the current work is blocked on.

**Independent Test**: Dispatch the deployed agent for an outbound call with no stored trunk in the account, and confirm the destination rings.

**Acceptance Scenarios**:

1. **Given** a package with an outbound telephony channel, **When** it is compiled, **Then** the outbound dial carries the same inline configuration as the warm transfer, including the from-number the inline form requires.
2. **Given** two dial-out paths in one project, **When** they are compared, **Then** they read the same environment names, so a credential rotation cannot fix one and break the other.

---

### User Story 3 - An operator who already has a stored trunk keeps working (Priority: P2)

Somebody who registered a trunk before this change, or who wants a trunk-only platform capability such as region pinning, keeps using it without editing generated code.

**Why this priority**: The stored path buys something inline does not, and silently removing it would break setups that work today, including every rig built against the previous instructions.

**Independent Test**: Set the stored-trunk environment name on a deployed agent and confirm the transfer uses that trunk rather than the inline configuration.

**Acceptance Scenarios**:

1. **Given** a deployed agent whose stored-trunk environment name is set, **When** a transfer runs, **Then** the stored trunk is used.
2. **Given** the same agent with that name unset, **When** a transfer runs, **Then** the inline configuration is used, and nothing had to be recompiled to switch between them.
3. **Given** the generated README, **When** it is read, **Then** it states which path is in force by default and the one thing the stored path adds.

---

### User Story 4 - The documents stop demanding a step that is no longer required (Priority: P3)

Every walkthrough that told the reader to register an outbound trunk before deploying now says what is actually required, and says what the trunk is still for.

**Why this priority**: The instructions are the reason people ran the extra step. Fixing the emitter without fixing them leaves the same contradiction that started the previous feature. Lower because it delivers nothing on its own.

**Independent Test**: Follow the LiveKit rig walkthrough from step 1 in an account with no stored trunk and confirm no step fails.

**Acceptance Scenarios**:

1. **Given** the rig walkthrough, **When** a reader works top to bottom, **Then** the outbound-trunk registration is either absent or marked as optional with its reason.
2. **Given** every document that describes dialling out, **When** they are compared, **Then** none of them presents the stored trunk as a prerequisite for a warm transfer.

### Edge Cases

- **A Connection that declares no SIP credentials.** Inline configuration is then impossible and the package must say so at compile time rather than emitting a call that cannot authenticate. The stored-trunk path stays the answer for that shape.
- **A route that does not use SIP at all.** The connector route carries media over a carrier WebSocket and has no trunk of either kind. Nothing here applies to it, and nothing here may change it.
- **Both configurations present.** One must win by a stated rule, and the rule must be visible in the generated README, because a silent preference is a support case.
- **A credential rotated on the carrier but not in the deployment.** Both dial-out paths fail the same way, in the platform's own words, because they read the same names. This is an improvement on the current split, where one path reads a trunk ID that keeps working while the other fails.
- **A carrier whose SIP host differs from its termination address.** The Connection declares one address today, and the repository already feeds that same value to the stored trunk's host field, so the two uses agree. A carrier that needs them to differ is out of scope and must be named as such rather than guessed at.
- **Region pinning.** Inline configuration does not express it. A package that needs it uses the stored trunk, and the documents must say that rather than leaving the reader to discover it.
- **Cold transfer.** Unaffected in every case. It must keep working with no outbound trunk and no inline configuration, and no test may start passing only because a trunk exists.

## Requirements *(mandatory)*

### Dial-out configuration

- **FR-001**: The emitted warm transfer MUST be able to dial using only the carrier configuration a Connection declares: the SIP host, the authentication credentials, and the number to call from. It MUST NOT require any platform-assigned trunk identity.
- **FR-002**: The emitted outbound-call path MUST use the same configuration as the warm transfer, read from the same environment names, so the two cannot drift apart or be fixed independently.
- **FR-003**: Where the inline form requires a from-number that a stored trunk would have defaulted, that number MUST come from the Connection rather than being invented, defaulted to a literal, or left to the platform.
- **FR-004**: The stored-trunk path MUST stay reachable without editing generated code, because it carries at least one capability the inline form does not express. Which path is in force MUST be decided by one stated rule, and that rule MUST be printed in the generated README.
- **FR-005**: No new authoring field MUST be added for the choice. The values the inline form needs are already declared, and the stored trunk's identity is already an environment name, so both paths are expressible without widening the authoring surface.

### Correctness and scope

- **FR-006**: A package whose Connection declares no SIP credentials MUST fail at compile time with a message naming what is missing, rather than emitting a call that cannot authenticate.
- **FR-007**: Cold transfer behaviour MUST NOT change: not the primitive, not the destination shape, not whether it needs a trunk, which it does not.
- **FR-008**: The connector route MUST NOT change in any way. It has no SIP trunk of either kind.
- **FR-009**: No emitted file MUST contain a credential value. Credentials MUST continue to be referenced by `UPPER_SNAKE` environment name only.
- **FR-010**: The authoring surface MUST NOT break: a package written before this change MUST keep loading and compiling, and MUST keep working when deployed with a stored trunk already in place.

### Documents and discipline

- **FR-011**: Every claim about the platform's trunk behaviour MUST cite its page and carry the 2026-08-12 verification date, in whichever repository document states it.
- **FR-012**: `docs/TRANSFERS.md` and the generated README MUST stop presenting outbound-trunk registration as a prerequisite for a warm transfer, and MUST say what the stored trunk is still for.
- **FR-013**: Wherever the Connection's role is documented, it MUST say that its SIP values now reach the deployed agent's dial-out path directly, since that is a new consequence of declaring them.
- **FR-014**: Anything in the repository that asserts the stored-trunk-only shape MUST be updated in the same change, including the emitted required-environment list if the stored-trunk name stops being required, and the goldens for any fixture whose output moves.

## Key Entities

- **Connection**: the carrier account a target binds, declaring the SIP host, credentials and number by environment name. Already the single home for those facts; this feature makes them reach the dial-out path.
- **Inline trunk configuration**: the carrier's host and credentials passed with the call, needing nothing registered on the platform.
- **Stored outbound trunk**: a platform-registered object with a platform-assigned identity, reached by environment name, and the only path that expresses region pinning.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In an account where a stored outbound trunk has **never** been created, a freshly compiled package completes a warm transfer, with **zero** platform-side registration commands run.
- **SC-002**: The number of manual setup commands required before a warm transfer can be attempted drops from **one** to **zero**.
- **SC-003**: **Both** dial-out paths in one emitted project read the same environment names, verifiable by reading the artifact.
- **SC-004**: A deployment with the stored-trunk name set uses the stored trunk, and the same build with it unset dials inline, with **zero** recompiles between the two.
- **SC-005**: **Zero** emitted files contain a credential value.
- **SC-006**: **Zero** repository documents present outbound-trunk registration as required for a warm transfer, and **every** one that mentions it says what it is still for.
- **SC-007**: Cold transfer keeps working with **no** trunk of either kind configured.
- **SC-008**: A package whose Connection lacks SIP credentials fails at compile time, with **zero** artifacts written.

## Assumptions

- **The stored trunk stays, as a runtime override read from the environment name it already uses.** Recommended over the alternatives because it adds no authoring field (the platform already documents that name as a fallback), it keeps every existing rig working with no edit, and it leaves region pinning reachable. The cost is one branch in emitted code, which is honouring a documented platform contract rather than inventing a mechanism. A compile-time-only choice was considered and rejected: compile is offline and cannot know whether a trunk exists in the operator's account.
- Inline configuration is derived from the Connection, never authored twice. If a Connection declares the values, the emitted project uses them.
- The Connection's SIP address and the platform's inline host field are the same value. The repository already feeds that Connection value into the stored trunk's host field, so the two uses are consistent by existing practice rather than by assumption.
- Both dial-out paths keep their current failure handling. What a transfer does when nobody answers is settled by the authoring surface and does not change here.
- Out of scope: region pinning as an authoring field, per-call trunk variation, carriers whose SIP host and termination address genuinely differ, the connector route, cold transfer, the Pipecat driver, and creating or managing platform-side trunks on the operator's behalf, which compile must never do because it is offline and credential-free.
- The operator holds carrier SIP credentials already. This feature removes a platform-side step, not a carrier-side one: the trunk still has to exist at the carrier, with transfers enabled where the carrier gates them.
