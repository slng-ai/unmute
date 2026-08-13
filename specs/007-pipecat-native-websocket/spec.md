# Feature Specification: Pipecat native WebSocket telephony (zero hosted infrastructure)

**Feature Branch**: `007-pipecat-native-websocket`

**Created**: 2026-08-13

**Status**: Draft

**Input**: User description: "Native Twilio WebSocket route for Pipecat Cloud: an operator's own Twilio number reaches a deployed Pipecat Cloud agent with zero operator-hosted infrastructure, via a TwiML Bin pointing at the platform's wss endpoint. Inbound, outbound, and cold transfer, no helper artifact."

## Why this feature exists

Feature 006 gave the Pipecat Daily route a carrier leg, and it works, but it has a
standing cost: the operator must host a small webhook server (the helper) wherever
calls should land, forever. That cost was accepted in 006's spec and assigned to
the operator. During the first live run the operator asked the fair question: if
the agent lives on Pipecat Cloud, why is anything hosted at all?

Pipecat Cloud answers it. The platform natively terminates a carrier's media
stream itself: the carrier's number is pointed at a small piece of static call
markup, hosted in the carrier's own console, which streams the call's audio
straight to the platform, and the platform starts the deployed agent. Nothing runs
on the operator's side in production. Not a server, not a helper; a tunnel
exists only in the optional local development flow, where everything runs on
the author's own machine.

This feature adds that route. It does not replace 006's route; the two serve
different needs, and the documentation must say which to pick when.

## Clarifications

### Session 2026-08-13

- Q: Should local phone testing be automated by `unmute dev --telephony` or
  documented as a manual README section? → A: Automated (end to end: local run,
  cloudflared tunnel, the number pointed at it, undone on exit). The tunnel is
  cloudflared, never ngrok, and it exists in local development only; the
  production path has no tunnel.
- Q: Does `examples/human-transfer-daily-twilio` stay beside the new example? →
  A: No. The telephony example set is reorganized by use case: warm transfer +
  inbound stays `examples/human-transfer` (LiveKit; no Pipecat route allows warm
  today, and the docs say so); cold transfer + inbound on Pipecat over Twilio is
  a new example on this route that **replaces** `human-transfer-daily-twilio`;
  inbound + outbound is `examples/telephony-hello`, audited as part of this
  feature.
- Q: Should `telephony-hello`'s Pipecat target stay on the self-hosted
  `carrier-websocket` route or move to this route? → A: Move to
  `cloud-websocket`.
- Q: What are the connection environment names? → A: Reuse `telephony-hello`'s
  existing `twilio_voice` mapping verbatim (`TWILIO_ACCOUNT_SID`,
  `TWILIO_AUTH_TOKEN`, `TWILIO_PHONE_NUMBER`), so one `.env` drives every Twilio
  example; future carriers follow the same `<CARRIER>_*` pattern, platform
  values are `PIPECAT_CLOUD_*`, destinations stay generic
  (`BILLING_PHONE_NUMBER`).
- Q: Which upstream sources ground the route? → A: The Pipecat `twilio-chatbot`
  example, Twilio's Media Streams WebSocket protocol page, and Twilio's TwiML
  Bins guide; all three are linked from the emitted README and the docs, with
  the step-by-step for obtaining every value written out rather than assumed.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A caller reaches the deployed agent with nothing hosted (Priority: P1)

An operator has deployed their compiled agent to Pipecat Cloud and owns a Twilio
number. They follow the generated README: a fixed number of actions in the Twilio
console (create the static call markup with the values the README dictates, point
the number at it) plus one lookup command to fetch the one account value the
compiler cannot know. Then they dial the number from a phone and the agent
answers. Nothing is running on the operator's machine, and nothing they own is
hosted anywhere.

**Why this priority**: This is the whole reason the feature exists. The zero
hosted infrastructure property is the deliverable, and inbound is where the
current alternative (006) pays its hosting cost.

**Independent Test**: Compile the example, deploy, do the dictated console
actions, call the number. Also testable offline: the build contains no process for
the operator to run, no server artifact, and a README whose production setup
names no tunnel and no hosting; the tunnel lives only in the local development
section (FR-015).

**Acceptance Scenarios**:

1. **Given** a deployed agent and a voice-capable Twilio number configured per the
   README, **When** a caller dials the number, **Then** the caller hears a
   response promptly and the agent converses, with zero operator-run processes
   involved.
2. **Given** the same setup, **When** the operator inspects the build directory,
   **Then** there is no helper file, no server to run, and the README's
   production setup contains no tunnel command and no hosting instruction (the
   tunnel appears only in the local development section, per FR-015).
3. **Given** the agent is deployed to a named region, **When** the README dictates
   the carrier configuration, **Then** the platform address it dictates is the
   regional one, without the operator having to know regions exist.
4. **Given** the agent is not yet deployed or not ready, **When** a caller dials,
   **Then** the failure is diagnosable: the README's troubleshooting section maps
   what the caller heard to where to look, by name.

---

### User Story 2 - The agent calls out through the operator's number (Priority: P2)

The operator declares an outbound phone channel. The README gives one command,
runnable from any machine with the operator's carrier credentials, that places a
call to a stated number. The target phone rings showing the operator's own number
as the caller, and the agent converses when answered. No hosted trigger service
exists.

**Why this priority**: Outbound is declared by the same channel the schema already
has, and it is the way an operator tests a call when they cannot receive one. It
also feeds the transfer story.

**Independent Test**: Run the dictated command with a reachable phone as the
target. Offline: the build's outbound documentation names no endpoint of the
operator's own, and a package without outbound gets no outbound command.

**Acceptance Scenarios**:

1. **Given** a deployed agent and the carrier credentials in the environment,
   **When** the operator runs the dictated outbound command with a target number,
   **Then** the target rings, the caller identity shown is the operator's own
   number, and the agent speaks on answer.
2. **Given** a package that declares no outbound, **When** it is compiled,
   **Then** no outbound command appears in the README and no outbound code path is
   emitted.
3. **Given** the outbound command fails (bad credentials, unreachable number),
   **Then** the failure is printed in the carrier's own words, never a silent
   success.

---

### User Story 3 - The agent hands the caller to a human (Priority: P2)

A caller asks for something the agent cannot do. The agent says it is
transferring, and the caller's live call is redirected to the declared human
destination through the operator's own carrier account. The agent's part ends. If
the destination declines or does not answer, the caller is still connected to the
agent, and the agent says so.

**Why this priority**: The transfer is the outcome the operator's project needs on
every route; the route is not complete without a way out to a person. It rides
the same carrier credentials outbound uses.

**Independent Test**: On a live inbound call, ask for a person; observe the
handoff and the destination ringing. Then repeat with the destination declining
and observe the caller still connected. Offline: the transfer tool is emitted
exactly when declared, the destination is an environment name (never a literal),
and the handback path speaks the same line whichever way the dial ended.

**Acceptance Scenarios**:

1. **Given** a live call and a declared cold transfer, **When** the caller asks
   for a person, **Then** the agent announces the handoff and the destination's
   phone rings, dialled through the operator's carrier account.
2. **Given** the destination answers, **Then** the caller and the human are
   connected and the agent is gone from the call.
3. **Given** the destination declines or times out, **Then** the caller is still
   connected to the agent, the agent says the transfer failed, and the failure is
   recorded in the session log by cause.
4. **Given** a second transfer request after a completed transfer attempt,
   **Then** it is not silently re-fired; the result of the first attempt is what
   the agent knows.

---

### User Story 4 - An author picks the right Twilio route, or moves between them (Priority: P3)

An author with a Twilio number and a Pipecat target now has more than one way to
connect them. The documentation states, in one place, when to choose this route
and when to choose the 006 carrier route, in terms of what each costs and what
each can do. An author who built the 006 form can switch to this route by changing
the declared route in the target and recompiling; the compiler tells them, by
name, anything in their declaration that no longer applies.

**Why this priority**: Two routes to the same carrier with different shapes is a
real choice, and an undocumented choice becomes a support burden. But the choice
only exists once stories 1 to 3 do.

**Independent Test**: Offline entirely. The comparison exists in the docs; a 006
example switched to this route recompiles or fails with named fields; declaring a
value this route cannot use fails naming the route.

**Acceptance Scenarios**:

1. **Given** the user documentation, **When** an author asks "which Twilio route",
   **Then** one section answers it with the concrete differences: what is hosted,
   what transfers can do, and what each requires from the carrier account.
2. **Given** a target declaring this route with a credential the route cannot use,
   **When** it is compiled, **Then** the compile fails naming the field, the
   route, and what the route actually needs.
3. **Given** a pure-inbound package on this route, **When** it is compiled,
   **Then** no carrier credential is required at all, because receiving a call
   through the platform needs none.

---

### User Story 5 - An author hears the phone path before deploying (Priority: P3)

An author runs one command, `unmute dev --telephony`, from their package. The
compiled agent starts locally, a tunnel gives it a public address, and their
Twilio number is pointed at it for the length of the session. They call the
number from a phone and talk to the exact agent they are about to deploy. On
exit, however it exits, the number's configuration is restored to what it was.

**Why this priority**: catching a broken phone path before a deploy is the whole
value of dev mode, but the route is complete and shippable without it; browser
and console cover conversation testing meanwhile.

**Independent Test**: run the command with a declared number, call it, hang up,
stop the session, and read the number's configuration back: it must be what it
was before the session.

**Acceptance Scenarios**:

1. **Given** a package on this route and carrier credentials in the
   environment, **When** the author runs `unmute dev --telephony`, **Then** one
   command yields a phone number that reaches the locally running agent,
   through a cloudflared tunnel, with no manual carrier console work.
2. **Given** the session ends, by clean exit or interrupt, **Then** the
   number's previous voice configuration is restored, and the production Bin
   (if one exists) was never touched.
3. **Given** the production docs and README, **Then** the tunnel appears only
   in the local development section; the production path contains none.

---

### Edge Cases

- The carrier markup carries a wrong agent identity or organization value: the
  caller's call connects and then drops or stays silent. The troubleshooting map
  must name this cause and where to read the correct values.
- The agent is deployed but the deployment is not ready: same caller experience as
  above, different fix. The map must separate them.
- The agent is deployed to one region but the carrier markup points at another
  region's platform address: calls fail or degrade. The README must emit the
  matching address so this cannot happen by following it, and the map must cover
  the case where an old markup survives a region change.
- The number is still attached to a SIP trunk or an old webhook: the markup is
  never consulted and the failure is silent. The 006 lesson ("one number serves
  one target at a time", read the number's configuration instead of listening)
  applies verbatim and must appear in this route's runbook too.
- A call arrives during agent cold start: the caller must hear something rather
  than dead air. The carrier markup can speak a short line before connecting the
  stream, and the README's dictated markup must include it.
- A phone call needs environment values (transfer destination, carrier
  credentials for outbound or transfer) that a browser or console session must
  never be asked for: the check runs only on phone calls, before the caller pays
  for the failure.
- The platform's stream address is reachable by anyone who knows the agent and
  organization names: what guards it, and what an operator should treat those
  names as, must be answered by research and stated honestly in the README rather
  than assumed.
- Transfer destination declared but carrier credentials absent: fails at startup
  of the phone session by name, not mid-call.
- A dev session and the production Bin want the same number: a number points at
  one thing at a time, so a dev session takes it and must give it back on every
  exit path, interrupt included. A dev session that dies without restoring the
  number has broken a real phone line; the restore is not optional.
- The tunnel drops mid-call during local development: the call fails as any
  network failure; the number restore still runs at session end.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The Pipecat target MUST offer a declarable route on which an
  operator's own Twilio number reaches the deployed agent with **zero
  operator-hosted infrastructure**: the build emits no process the operator
  runs, no endpoint the operator hosts, and the runbook's production path
  contains no tunnel and no hosting step. The only tunnel anywhere is the local
  development flow's (FR-015), which runs on the author's machine and touches
  nothing hosted.
- **FR-002**: The route MUST be distinguishable in the authoring surface from the
  existing self-hosted carrier WebSocket route and from the 006 Daily carrier
  route, so an author's declaration states which shape they get, and the compile
  report names it.
- **FR-003**: With an inbound phone channel declared, the generated README MUST
  dictate the complete carrier-side setup: the exact static call markup to create
  (including a spoken line before the stream connects, so cold start is never
  dead air), where each placeholder value comes from, and the console actions to
  point the number at it. The one account value the compiler cannot know is
  fetched with one dictated command. The README and the docs MUST link the three
  grounding sources: the Pipecat `twilio-chatbot` example, Twilio's Media
  Streams WebSocket protocol page, and Twilio's TwiML Bins guide.
- **FR-004**: When the target declares a deployment region, the dictated platform
  address MUST be the matching regional one.
- **FR-005**: A pure-inbound package on this route MUST compile and run with no
  carrier credentials declared, because the platform receives the call without
  any. One capability behaves differently in that state and the documentation
  MUST say so rather than let it be discovered: ending a call. With credentials
  present the agent ends it through the carrier's own call control; without them
  it ends by closing the stream, which the carrier treats as the end of the call
  because the markup has nothing after the stream. Both end the call; only the
  first can do anything else to it. Implementation note, added 2026-08-13: the
  second mechanism costs one compile-time branch in the emitted bot, because the
  framework's own transport factory refuses to build a carrier transport without
  credentials for the first (research F15). The promise is unchanged; what
  changed is that keeping it takes code rather than a default.
- **FR-006**: With an outbound phone channel declared, the README MUST give one
  command that places a call through the operator's carrier account from the
  operator's own number, with only the destination typed by the operator; every
  other value is read from the environment by name. A package without outbound
  gets neither the command nor the code path.
- **FR-006a**: "The operator's own number" MUST be defined wherever it is asked
  for, and it means: a **voice-capable phone number the operator owns in the
  Twilio account those credentials belong to**, used as the caller identity the
  recipient sees. Three things follow, and the documentation MUST state all
  three rather than leave them to be discovered:
  - It MAY be the same number that receives calls. One number serves both
    directions, which is why the examples share one environment value.
  - A number has no separate "outbound" capability to look for. Voice capability
    is the whole requirement; there is nothing else to enable on the number
    itself.
  - Things outside the number can still refuse a call, and the troubleshooting
    map MUST name them: the destination country's permissions on the account,
    and any account-level restriction on calling unverified destinations. The
    exact rules MUST be verified against the carrier's own documentation and
    dated before they are written into any emitted file.
- **FR-007**: With a cold transfer declared, the agent MUST redirect the live
  call to the declared destination through the operator's carrier account,
  announce before doing so, and on failure keep the caller connected and say so.
  The destination is always an environment name; no phone number literal is ever
  emitted.
- **FR-008**: Outbound and cold transfer MUST require the carrier credentials
  they use, checked at the start of a phone session by name, and never required
  or read by browser or console sessions of the same package.
- **FR-009**: Declaring on this route a connection value the route cannot use
  MUST fail at compile time naming the value, the route, and the set the route
  accepts.
- **FR-010**: The route MUST take its place in the single capability rulebook
  with per-capability evidence, entering as provisional and losing that tag only
  through dated live runs recorded in this feature's tasks file, matching the
  006 discipline.
- **FR-011**: No secret value may appear in any emitted file or document:
  environment variable names only.
- **FR-012**: The user documentation MUST contain one section comparing the
  Twilio routes on the Pipecat target: what each hosts, what each requires from
  the carrier account, what transfers can do on each, and a plain recommendation
  for the common case.
- **FR-013**: The route's troubleshooting section MUST map each named edge case
  above to what the caller hears and where to look, including the
  number-still-on-a-trunk silent failure and the wrong-identity-in-markup case.
- **FR-014**: Every existing **route** MUST compile byte-identical builds after
  this feature; the compiler change is additive behind the new route
  declaration. The example set changes deliberately per FR-016 and nothing
  else moves.
- **FR-015**: `unmute dev --telephony` MUST run the phone path locally, end to
  end, with one command: start the compiled agent locally, expose it through a
  cloudflared tunnel, point the declared number's voice configuration at it,
  and restore that configuration on every exit path, interrupt included. No
  carrier markup is created locally and the production markup is never touched;
  the tunnel exists in local development only and never appears in the
  production part of any document.
- **FR-016**: The telephony examples MUST cover the three use cases, one
  example each: warm transfer + inbound (`examples/human-transfer`, LiveKit;
  the docs state that no Pipecat route offers warm transfer today); cold
  transfer + inbound on Pipecat over Twilio (a new example on this route, which
  replaces `examples/human-transfer-daily-twilio`); inbound + outbound
  (`examples/telephony-hello`, whose Pipecat target moves to this route and
  which is audited as FR-016a defines). All three Twilio examples share one
  `.env`.
- **FR-016a**: "Audited" for `examples/telephony-hello` means both halves, and
  neither alone closes it. Offline: its route declaration, its README claims, its
  connection comment, and the accuracy of its LiveKit half are read and corrected
  where the route move made them stale. Live: the operator deploys it to Pipecat
  Cloud and confirms a real inbound call and a real outbound call work, and that
  confirmation is recorded dated in this feature's tasks file. The example is not
  audited until the operator says the deployed agent works.
- **FR-017**: The connection environment names MUST reuse `telephony-hello`'s
  existing mapping (`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`,
  `TWILIO_PHONE_NUMBER`), and the naming convention MUST scale to future
  carriers as `<CARRIER>_*`, with platform values named `PIPECAT_CLOUD_*` and
  transfer destinations staying generic.

### Key Entities

- **Route**: the new (provider, transport, carrier) row: Pipecat target, platform
  native carrier stream, Twilio. Owns capability evidence, required environment,
  and (unlike 006's row) declares no operator-run process and no public endpoint.
- **Carrier voice markup**: the static call markup hosted in the carrier's
  console that connects a call's audio stream to the platform and names the agent.
  Not an emitted file; the README dictates its exact content.
- **Connection**: the carrier credential set, needed only when the package places
  or redirects calls: account identifier, auth token, and the operator's own
  number for caller identity. No SIP address exists on this route.
- **Transfer destination**: a named environment variable holding where a cold
  transfer goes, read at call time.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator with a deployed agent and a voice-capable Twilio
  number completes inbound setup in at most four steps: three in the carrier's
  console plus one lookup command. The count of processes they must run or host
  in production is exactly zero.
- **SC-002**: A caller dialing the number hears a response within 2 seconds, and
  the agent's greeting within 10 seconds on a cold start.
- **SC-003**: An outbound call is placed with one command; the recipient sees the
  operator's own number (FR-006a) as the caller.
- **SC-004**: A cold transfer, attempted 5 times in a test session, connects the
  caller to the destination every time the destination answers, and every
  declined attempt leaves the caller still connected and told; zero silent
  failures.
- **SC-005**: The emitted build for this route contains every artifact the
  no-carrier Pipecat Cloud build contains and no additional process artifact;
  the README's production setup contains zero hosting or tunnel instructions.
- **SC-006**: Every route not touched by FR-016's example changes compiles
  byte-identical builds from before this feature, and the untouched examples
  (`human-transfer`, `human-transfer-daily`, and the non-telephony set) are
  unchanged files.
- **SC-007**: The route comparison section answers "which Twilio route should I
  use" in one read: a new author can state the deciding difference (hosting
  against transfer shape) after reading it once.
- **SC-008**: `unmute dev --telephony` makes the declared number reach a locally
  running agent with one command, and the number's previous voice configuration
  is restored on every exit path, interrupt included: read back after a session,
  it is byte-identical to what it was before, in 100% of runs including
  interrupted ones.

## Assumptions

- Twilio is the only carrier in scope. The platform natively terminates at least
  one other carrier's stream; that is future work and the route naming must not
  block it.
- Warm transfer is out of scope on this route, as it is on 006's, and therefore
  on every Pipecat route; the docs state that plainly wherever the example set
  is described, rather than leaving the gap implied.
- `examples/human-transfer-daily` (the Daily-provisioned number, no carrier)
  stays untouched: the reorganized example set in FR-016 covers the
  Twilio-carrier use cases, and that example belongs to the no-carrier route.
- Local development tunneling is cloudflared, matching the rest of this
  repository; ngrok appears nowhere, including where upstream docs use it.
- The 006 Daily carrier route keeps its capability rows and goldens but no
  longer ships an example; its live-run tasks stay open against a private
  fixture rather than a public example.
- Call audio is standard telephone quality; no requirement here exceeds what a
  phone call carries.
- The operator has an authenticated Pipecat Cloud CLI and can deploy; deployment
  itself is feature 001/004 ground and unchanged here.
- Live validation is gated exactly as 006's: capabilities stay provisional until
  dated live runs are recorded, and the offline half of validation runs with no
  accounts.
- The security model of the platform's public stream address (what limits who can
  connect a stream to a given agent) is a research question for the plan phase;
  the spec's requirement is only that the answer, whatever it is, is stated
  plainly in the emitted README rather than left implicit.

## Dependencies

- A Pipecat Cloud account with a deployed agent (for the live half only).
- A Twilio account with a voice-capable number the operator owns (for the live
  half only), whose account permits calls to the destinations the live runs use.
- Two reachable phones for the transfer drill (live half only).
- The platform's native carrier stream endpoint and its documented markup
  contract, verified against current platform documentation during planning, with
  the verification dated.
- Grounding sources, linked from the emitted README and the docs (FR-003):
  - <https://github.com/pipecat-ai/pipecat-examples/tree/main/twilio-chatbot>
  - <https://www.twilio.com/docs/voice/media-streams/websocket-messages>
  - <https://help.twilio.com/articles/360043489573-Getting-started-with-TwiML-Bins>
