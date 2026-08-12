# Feature Specification: Dial out with the carrier's own SIP credentials

**Feature Branch**: `feature/warm-cold-human-transfer` (feature dir `002-inline-sip-trunk`)

**Created**: 2026-08-12

**Status**: Draft

**Input**: User description: "Let a LiveKit package dial out through its own carrier SIP credentials, with no LiveKit stored outbound trunk. The emitted warm transfer and outbound call both depend on a trunk the operator registers by hand, whose ID only the platform can assign. The documented alternative passes the trunk's hostname and credentials inline, and those are exactly the values a Connection already declares. Make inline the way a generated package dials out, decide deliberately whether the stored-trunk path stays, keep cold transfer out of scope, and update the documents where the trunk step stops being required."

## Why this exists

A compiled package cannot dial anybody until a human has run a command that this repository never mentions in the same breath as the package.

Today both outbound paths in the emitted LiveKit project reach the carrier through a **stored outbound trunk**: an object the operator creates with the platform CLI, whose ID the platform assigns. The warm transfer picks that ID up from `LIVEKIT_SIP_OUTBOUND_TRUNK` through the prebuilt's documented environment fallback, and the outbound-call path reads the same name explicitly. Neither value can come from the carrier, so neither can come from the package. The result is a build directory that looks complete, deploys cleanly, and then fails on the first transfer because a separate registration step was never run.

That is not a prediction. On 2026-08-12 a compiled `examples/human-transfer` was deployed to LiveKit Cloud and asked for a manager over a live session. The agent registered, held a full conversation, fired the transfer, and then raised, from inside the prebuilt's constructor:

```
ValueError: `LIVEKIT_SIP_OUTBOUND_TRUNK` environment variable, `sip_trunk_id`, or `sip_connection` must be set
```

The caller heard that the manager was not available. Every other part of the package worked. The platform's own error message lists the three ways to satisfy it, and this feature takes the third.

The platform documents an alternative that removes the step: pass the trunk's hostname and credentials **inline** with the call. What makes this worth doing rather than merely possible is that a Connection already declares every value the inline form needs, for whichever carrier it names. The package is not missing information. It is throwing information away and then asking the operator to re-supply it in a shape only the platform can mint.

Two consequences follow. First, a fresh compile becomes dialable with nothing but the carrier credentials the operator already holds. Second, the same emitted code works for any carrier the Connection vocabulary covers, instead of only for a trunk somebody registered, which is what makes the transfer story portable rather than Twilio-shaped by accident.

## Verified platform contract *(source of truth for this feature)*

Verified 2026-08-12 against live documentation. Every row carries its source, per Constitution principle IV.

| Fact | Source |
|---|---|
| The warm-transfer prebuilt takes **either** a stored trunk ID **or** inline trunk configuration, and one of the two is required. | [WarmTransferTask](https://docs.livekit.io/agents/prebuilt/tasks/warm-transfer/) |
| The outbound-participant API takes the same either/or, and with inline configuration a **from-number is required**, "because inline trunk configuration has no `numbers[]` field to pick a default from". The prebuilt's own page does not repeat that rule, but the prebuilt reaches the carrier through this API, so the rule reaches it too. | [CreateSIPParticipant](https://docs.livekit.io/reference/telephony/sip-api/#createsipparticipant), [Inline trunk configuration](https://docs.livekit.io/telephony/making-calls/outbound-calls/#inline-trunk) |
| Inline configuration carries a hostname, a destination country, a transport, an auth username, an auth password, two header maps, and an optional `From` host. Only the hostname is needed to dial. | [SIPOutboundConfig](https://docs.livekit.io/reference/telephony/sip-api/#sipoutboundconfig) |
| A stored outbound trunk is **not required**. Inline configuration is documented for quick setup, for trunk settings that vary per call, for one SIP provider per tenant, and for "routing to arbitrary SIP endpoints". | [Outbound trunk](https://docs.livekit.io/telephony/making-calls/outbound-trunk/), [Inline trunk configuration](https://docs.livekit.io/telephony/making-calls/outbound-calls/#inline-trunk) |
| **Region pinning is not a stored-trunk capability.** The destination-country parameter "works with both inline trunk configuration and stored outbound trunks", and if the code "doesn't match any supported region, the parameter has no effect and calls are routed using default behavior", so it is optional in either form. | [Region pinning](https://docs.livekit.io/telephony/features/region-pinning/#outbound-calls) |
| What a stored trunk holds that inline configuration does not: a **list** of caller-ID numbers the platform picks from per call, trunk **metadata** copied onto every SIP participant it creates, and one place to change the host or credentials without touching a deployment. | [CreateSIPOutboundTrunk](https://docs.livekit.io/reference/telephony/sip-api/#createsipoutboundtrunk) |
| The environment names are **conventions, not platform requirements**. The only two the platform reads by itself are the prebuilt task's own fallbacks for the stored trunk ID and the from-number. The outbound-participant API reads no environment at all. LiveKit's own inline examples use plain SIP-shaped names such as `SIP_TRUNK_HOSTNAME`, with no LiveKit prefix and no carrier prefix. | [WarmTransferTask](https://docs.livekit.io/agents/prebuilt/tasks/warm-transfer/), [Inline trunk configuration](https://docs.livekit.io/telephony/making-calls/outbound-calls/#inline-trunk) |
| Trunks are long-lived objects the platform caches and reuses, and creating **one per call** is documented as harmful at scale. The documentation does not say whether inline configuration takes part in that caching, so this feature must not claim that it does or that it does not. | [Outbound trunk](https://docs.livekit.io/telephony/making-calls/outbound-trunk/) |
| Inline configuration removes the stored trunk **object**, not the platform's SIP service, which still sends the INVITE to the carrier's host. So any standard SIP carrier is reachable, and the call still leaves through LiveKit SIP, which is managed on LiveKit Cloud. | [Outbound calls](https://docs.livekit.io/telephony/making-calls/outbound-calls/), [Region pinning](https://docs.livekit.io/telephony/features/region-pinning/) |
| Cold transfer acts on the caller's existing leg through SIP REFER and needs **no outbound trunk of any kind**. | [Call forwarding](https://docs.livekit.io/telephony/features/transfers/cold/) |

## Clarifications

### Session 2026-08-12

- Q: Now that region pinning works inline too, should a generated project still be able to fall back to a stored LiveKit trunk at all? → A: No. Drop the stored path entirely. Inline is the only way a generated project dials out, and the stored-trunk environment name leaves the emitted project. The cost is accepted: a rig built on the old instructions must set the carrier SIP credentials, and its trunk goes unused.
- Q: Which environment variable names should carry the inline trunk values? → A: Carrier-neutral SIP names, replacing the carrier-prefixed ones the shipped connection uses today. The values are standard SIP trunk settings and belong under standard SIP names, which also matches how the platform's own examples read.
- Q: What exactly should the four neutral names be spelled? → A: `SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD`, `SIP_FROM_NUMBER`, matching the platform's own inline examples and the wire field names. Accepted on the condition that every document showing the old names is updated in the same change.
- Q: Should the emitted inline configuration also carry the optional destination-country and transport settings? → A: No. Exactly the four values needed to dial. Outbound calls already originate from the region the agent runs in, which `deployment_region` already controls, and the transport is auto-detected. Region pinning by destination country is out of scope with that reason on the record.
- Volunteered by the author, same session: transfers use each platform's **own** transfer feature, which exists only where telephony runs in the cloud (LiveKit SIP, Pipecat Daily). The routes that run on a laptop carry plain audio with no transfer control, and this repository will not substitute its own machinery there. So a transfer can only be proved against a real cloud deployment. **Confirmed during implementation as already settled in the repository**: `docs/SCHEMA.md` N31 (2026-08-11) supersedes N28 on exactly this point, and records that the laptop-route transfers were built, live-tested and deleted because each custom design made the generated process own the call's audio path. The capability table has been right all along.
- Q: What happens to the outbound-trunk setup the generated project ships, now that nothing reads it? → A: Remove all three places. Stop emitting the outbound-trunk input file, delete the README steps that create the trunk, and switch the local development path to dial inline as well, so local and deployed dial by the same mechanism. Inbound is untouched.

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

### User Story 3 - An operator who already has a stored trunk is told what changed (Priority: P2)

Somebody who registered a trunk on the old instructions recompiles, reads one clear paragraph about what replaced it, sets the carrier SIP credentials, and dials. Their old trunk sits unused and can be deleted.

**Why this priority**: This is a deliberate break. The stored path is gone, not demoted, so the only thing standing between a working rig and a broken one is whether the change is stated plainly and in the place the operator is already reading. A silent break here is the worst outcome of the whole feature.

**Independent Test**: Take a build made before this change, recompile it, and follow only the generated README to get back to a working transfer.

**Acceptance Scenarios**:

1. **Given** a recompiled package, **When** its environment template and README are read, **Then** the stored-trunk name is absent from both, and the README says in plain words that the trunk is no longer used and which values replaced it.
2. **Given** a deployment that still has the old stored-trunk name set, **When** a transfer runs, **Then** it dials inline and the leftover name is ignored, with no half-configured state and no error that blames the wrong thing.
3. **Given** the same deployment with the carrier SIP credentials missing, **When** a transfer is attempted, **Then** it fails naming the missing values, not the missing trunk.
4. **Given** an operator adopting the updated connection example, **When** they read the migration note, **Then** it lists the old name and the new name for each of the four SIP values, so the `.env` edit is mechanical and needs no guessing.

---

### User Story 4 - The documents stop demanding a step that is no longer required (Priority: P3)

Every walkthrough that told the reader to register an outbound trunk before deploying now says what is actually required, and nothing more.

**Why this priority**: The instructions are the reason people ran the extra step. Fixing the emitter without fixing them leaves the same contradiction that started the previous feature. Lower because it delivers nothing on its own.

**Independent Test**: Follow the LiveKit rig walkthrough from step 1 in an account with no stored trunk and confirm no step fails.

**Acceptance Scenarios**:

1. **Given** the rig walkthrough, **When** a reader works top to bottom, **Then** the outbound-trunk registration is absent, not merely marked optional, because an optional step that nothing reads is still a step somebody will run.
2. **Given** every document that describes dialling out, **When** they are compared, **Then** none of them presents the stored trunk as a prerequisite for a warm transfer.
3. **Given** the generated README, **When** its telephony setup is read end to end, **Then** the only platform-side records it asks for are the inbound trunk and the dispatch rule, and it says in one line why those two cannot go the same way the outbound trunk did.

### Edge Cases

- **A Connection that declares no SIP credentials.** With the stored path gone this is fatal rather than a fallback, so it must fail at compile time. It already cannot happen on this route: the route requires the SIP address, username, password and number, and validation rejects a package that omits any of them. This feature therefore adds a test that pins that behaviour, not a new rule.
- **A route that does not use SIP at all.** The connector route carries media over a carrier WebSocket and has no trunk of either kind. Nothing here applies to it, and nothing here may change it.
- **A leftover stored-trunk name in a deployment.** Nothing reads it, so it has no effect. It must not be silently honoured, because a name that works in one build and is ignored in the next, with no message either way, is the worst possible shape for the operator.
- **A credential rotated on the carrier but not in the deployment.** Both dial-out paths fail the same way, in the platform's own words, because they read the same names. This is an improvement on the current split, where one path reads a trunk ID that keeps working while the other fails.
- **A carrier whose SIP host differs from its termination address.** The Connection declares one host, and it becomes the inline hostname, which is the single place the value is used once the stored trunk is gone. A carrier that needs those to differ is out of scope and must be named as such rather than guessed at.
- **Region pinning.** Both forms express it, through the same destination-country parameter, so it is no reason to prefer either and no repository document may say that it is. Out of scope here on purpose: outbound calls already originate from the region the agent runs in, which `deployment_region` already sets, and the parameter has no effect at all when the country matches no supported region. A package with a local telephony regulation to satisfy is the case that would reopen this.
- **A carrier that needs a transport other than the platform default.** The default detects the transport, so nothing is emitted for it. A carrier that must be pinned to TCP or TLS is out of scope and named as such rather than guessed at.
- **Cold transfer.** Unaffected in every case. It must keep working with no outbound trunk and no inline configuration, and no test may start passing only because a trunk exists.

## Requirements *(mandatory)*

Identifiers are stable references, so they are never renumbered. They are grouped
by topic rather than by number, which is why FR-017 sits with the dial-out
requirements and FR-018 with the correctness ones.

### Dial-out configuration

- **FR-001**: The emitted warm transfer MUST be able to dial using only the carrier configuration a Connection declares: the SIP host, the authentication credentials, and the number to call from. It MUST NOT require any platform-assigned trunk identity.
- **FR-002**: The emitted outbound-call path MUST use the same configuration as the warm transfer, read from the same environment names, so the two cannot drift apart or be fixed independently.
- **FR-003**: Where the inline form requires a from-number that a stored trunk would have defaulted, that number MUST come from the Connection rather than being invented, defaulted to a literal, or left to the platform.
- **FR-004**: The emitted project MUST have exactly one dial-out shape. It MUST NOT read or require any platform-assigned trunk identity, and the stored-trunk environment name MUST disappear from the emitted environment template and from the compile report. A deployment that still sets that name MUST be unaffected by it. What the *generated documents* may say about a stored trunk is FR-018's, not this requirement's.
- **FR-005**: A new authoring field MUST NOT be added. Every value the inline form needs is already declared on the Connection, so the authoring surface does not widen: this feature removes a step, it does not add a knob.
- **FR-017**: The emitted inline configuration MUST carry exactly the four values needed to dial: the trunk hostname, the authentication username, the authentication password, and the number to call from. It MUST NOT carry a destination country or a transport. Both are optional on the platform, the transport is auto-detected, and outbound calls already originate from the region the agent runs in, which `deployment_region` already decides.

### Correctness and scope

- **FR-006**: A package whose Connection declares no SIP credentials MUST fail at compile time with a message naming what is missing, rather than emitting a call that cannot authenticate.
- **FR-007**: Cold transfer behaviour MUST NOT change: not the primitive, not the destination shape, not whether it needs a trunk, which it does not.
- **FR-008**: The connector route MUST NOT change in any way. It has no SIP trunk of either kind.
- **FR-009**: An emitted file MUST NOT contain a credential value. Credentials MUST continue to be referenced by `UPPER_SNAKE` environment name only.
- **FR-010**: The authoring surface MUST NOT break: a package written before this change MUST keep loading and compiling with no edit. The **deployment** contract does break, deliberately: a rig that dialled through a stored trunk MUST be told, in the generated README and in `docs/TRANSFERS.md`, that the trunk is no longer used, which values replace it, and that the trunk can be deleted.
- **FR-018**: The repository MUST NOT create a stored **outbound** trunk, emit an input for one, or instruct anybody to create one. The emitted outbound-trunk input file goes, every generated document stops mentioning one, and the local development path dials inline instead of registering one, so local and deployed use the same mechanism. This requirement owns the local development path and the generated documents; FR-004 owns what the emitted project reads. **Inbound is untouched**: the inbound trunk and the dispatch rule are how an unsolicited call reaches a project and a room at all, they carry no dial-out configuration, and this feature MUST NOT disturb them.

### Documents and discipline

- **FR-011**: Every claim about the platform's trunk behaviour MUST cite its page and carry its verification date, in whichever repository document states it. For every claim this change lands, that date is 2026-08-12; a later re-verification carries its own.
- **FR-012**: `docs/TRANSFERS.md` and the generated README MUST stop presenting outbound-trunk registration as a prerequisite for a warm transfer, MUST say that a generated project no longer uses a stored outbound trunk at all, and MUST tell an operator who has one what to set instead.
- **FR-013**: Wherever the Connection's role is documented, it MUST say that its SIP values now reach the deployed agent's dial-out path directly, since that is a new consequence of declaring them. The pages that document that role today are `docs/user/reference/targets-yaml.md`, which describes the `connection` field and points at the Connection keys, `docs/TELEPHONY.md`, `docs/user/learn/07-phone-calls.md` and `docs/user/targets/livekit.md`. Naming them is deliberate: an unnamed "wherever" is how a page gets missed.
- **FR-014**: Anything in the repository that asserts the stored-trunk shape MUST be updated in the same change: the stored-trunk name leaves the emitted required-environment list, every test that asserts its presence follows, and the goldens for every fixture whose output moves MUST be read rather than regenerated blind. The local development path is FR-018's.
- **FR-015**: The SIP values a Connection declares MUST be named as standard SIP trunk settings rather than after one carrier, in the shipped connection examples, the scaffold template, and every document that shows them. The names are `SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD` and `SIP_FROM_NUMBER`, matching the platform's own inline examples and its wire field names. The hostname name is deliberate: the platform documents that the value "is not a SIP URI and shouldn't contain the `sip:` protocol", which a name ending in "address" invites. The compiler MUST NOT gain any knowledge of these names: it reads whatever the Connection declares, which is what makes the same emitted code work for any SIP carrier.
- **FR-016**: The rename MUST reach the environment **names** only. The Connection's own keys stay as they are, because renaming a key would break a written package, which FR-010 forbids. One authored name per key means one name reaches every use of that value, so no emitted file may carry a mixture of old and new names for the same value. Carrier-specific credentials on other routes, such as a carrier's REST account keys, MUST keep their carrier-shaped names, because those are genuinely one carrier's and not standard SIP.
- **FR-019**: ~~The `docs/SCHEMA.md` amendment must also record that N28's connector transfer path is rejected by choice.~~ **Withdrawn 2026-08-12, during implementation.** The premise was wrong: **N31 (2026-08-11) already supersedes N28**, says so in its first sentence, and agrees with the capability table. It also records that the connector and carrier-WebSocket transfers were *built, live-tested and then deleted*, because every custom design made the generated process own the call's audio path and each live test found a new lifecycle bug there. So there was no contradiction to settle and nothing was left unbuilt. What remains, and is covered by FR-011 and FR-014, is one sentence of N28 that this feature falsifies: `WarmTransferTask` no longer needs "a SIP participant reached through an outbound trunk", only the participant. Amendment N33 retires that half. See [research.md R8](./research.md#r8-a-finding-i-got-wrong-and-the-document-that-already-had-it-right).

## Key Entities

- **Connection**: the carrier account a target binds, declaring the SIP host, credentials and number by environment name. Already the single home for those facts; this feature makes them reach the dial-out path.
- **Inline trunk configuration**: the carrier's host and credentials passed with the call, needing nothing registered on the platform.
- **Stored outbound trunk**: a platform-registered object with a platform-assigned identity. After this feature, no generated project uses one. It holds a caller-ID number list and trunk metadata, and it can be changed without touching a deployment, which is what an operator gives up. It does not hold region pinning, which both forms express.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In an account where a stored outbound trunk has **never** been created, a freshly compiled package completes a warm transfer, with **zero** platform-side registration commands run.
- **SC-002**: The platform-side setup a warm transfer needs before it can be attempted drops from **two commands and one copied value** (`envsubst` on the emitted trunk input, `lk sip outbound create`, then pasting the returned trunk id into `.env`) to **nothing**.
- **SC-003**: **Both** dial-out paths in one emitted project read the same environment names, verifiable by reading the artifact.
- **SC-004**: **Zero** emitted files, environment names, or generated documents refer to a platform-assigned outbound trunk, and a deployment that still sets the old name behaves identically to one that does not.
- **SC-005**: **Zero** emitted files contain a credential value.
- **SC-006**: **Zero** repository documents present outbound-trunk registration as required for a warm transfer, and **every** one that still mentions a stored outbound trunk says that generated projects no longer use it.
- **SC-007**: Cold transfer keeps working with **no** trunk of either kind configured.
- **SC-008**: A package whose Connection lacks SIP credentials fails at compile time, with **zero** artifacts written.
- **SC-009**: **Zero** carrier-prefixed names remain on the SIP values in any shipped example, the scaffold template, or any document, and **zero** lines of compiler code mention any of the four names.
- **SC-010**: A Connection that still declares the old carrier-prefixed names compiles and dials exactly as before, proving the compiler stayed name-agnostic.
- **SC-011**: The emitted `sip-inbound-trunk.json` and `sip-dispatch-rule.json` are **byte for byte** unchanged, and **zero** local development steps create a stored outbound trunk. What no longer refers to one is SC-004's count, not this one's.
- **SC-012**: Local development and a deployed agent dial through the **same** mechanism, so a transfer that works in one cannot fail in the other for want of a trunk.
- **SC-013**: ~~Zero repository documents claim a transfer works on a route whose capability row denies it.~~ **Withdrawn with FR-019.** The one document that looked like it did is explicitly superseded by a later amendment, which is how an amendment list is supposed to work, so a text-matching test would have to special-case supersession to avoid failing on history it is meant to preserve.

## Assumptions

- **The stored trunk goes away entirely.** Decided 2026-08-12 after the region-pinning claim that justified keeping it turned out to be wrong: both forms express it. What remained was a caller-ID number list, trunk metadata, and changing the host without touching a deployment, none of which the emitted project uses today. Keeping a runtime override for those would have bought one branch in emitted code and two ways for the same package to dial, so it was rejected in favour of one shape. The accepted cost is a deployment-level break, covered by User Story 3.
- **A package that genuinely needs a stored trunk is out of scope, not impossible.** Nothing stops an operator writing that call by hand outside the generated project, and nothing here removes the platform feature. It simply stops being something `unmute compile` emits.
- Inline configuration is derived from the Connection, never authored twice. If a Connection declares the values, the emitted project uses them.
- **The environment names are already the author's to choose.** A Connection declares one name per key and the compiler carries it through verbatim, so nothing in the compiler derives, prefixes, or knows these names. Renaming them is therefore a change to the shipped connection examples, the scaffold template, and the documents, not to any naming logic, and the emitter needs no work to keep supporting a carrier-prefixed name somebody already authored.
- **Renaming costs an operator four lines in `.env`, once.** It lands in the same release as the trunk removal, so it is one migration rather than two, and User Story 3 covers stating it.
- The Connection's SIP address and the platform's inline host field are the same value. The repository already feeds that Connection value into the stored trunk's host field, so the two uses are consistent by existing practice rather than by assumption.
- Both dial-out paths keep their current failure handling. What a transfer does when nobody answers is settled by the authoring surface and does not change here.
- Out of scope: region pinning, transport pinning, per-call trunk variation, carriers whose SIP host and termination address genuinely differ, the connector route, cold transfer, the Pipecat driver, **the inbound trunk and dispatch rule**, and creating or managing platform-side trunks on the operator's behalf, which compile must never do because it is offline and credential-free.
- **A transfer can only be proved against a real cloud deployment, and that is accepted rather than worked around.** Both platforms' transfer features live on the routes whose telephony runs in the cloud: LiveKit's on SIP, Pipecat's on Daily. The routes that run on a laptop carry plain audio with no transfer control, and building our own transfer machinery there was considered and rejected, because a substitute that behaves almost like the real primitive is the kind of thing that passes locally and fails on a live call. The consequence is that this feature's acceptance test is a manual live call, which is why the offline layer has to carry the shapes a live call will not reach, such as the warm-only package.
- The operator holds carrier SIP credentials already. This feature removes a platform-side step, not a carrier-side one: the trunk still has to exist at the carrier, with transfers enabled where the carrier gates them.
