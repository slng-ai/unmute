# Feature Specification: Spoken Agent Handoffs

**Feature Branch**: `[014-spoken-agent-handoffs]`

**Created**: 2026-08-15

**Status**: Draft

**Input**: User description: "Allow an agent handoff to optionally tell the caller that the handoff is happening. Confirm the behavior on LiveKit and Pipecat, and keep the conversation natural across the handoff."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Hear the handoff before it happens (Priority: P1)

A package author can give an agent handoff one short announcement sentence.
When the handoff runs, the active agent speaks that sentence once before the
next agent takes control.

**Why this priority**: A silent change between agents with the same voice feels
like a pause or a broken call. The caller needs a clear cue that another agent
is taking over.

**Independent Test**: Compile one two-agent package with an announcement, start
a conversation, trigger the handoff, and verify that the announcement is heard
once and finishes before the receiving agent responds.

**Acceptance Scenarios**:

1. **Given** a handoff with an announcement, **When** its transfer gate passes and the handoff runs, **Then** the caller hears that exact sentence once before control changes.
2. **Given** a handoff with an authored announcement, **When** the announcement is playing, **Then** the receiving agent does not speak over it or take a user turn until it finishes.
3. **Given** the same package compiled for each shipped target, **When** the same handoff runs, **Then** both targets preserve the same spoken order.

---

### User Story 2 - Continue naturally after a handoff (Priority: P2)

After the announcement, the receiving agent continues from the caller's latest
request. It does not repeat the package greeting or ask again for information
that crossed with the handoff.

**Why this priority**: An announcement helps only if the receiving agent then
acts like a colleague who has been briefed. A repeated greeting or question
makes the transfer feel fake.

**Independent Test**: In one conversation, identify a customer, hand off to a
second agent, then hand back. Verify that the entry greeting is spoken only at
call start, the known customer information is reused, and the latest request
drives the next action.

**Acceptance Scenarios**:

1. **Given** a handoff back to the entry agent, **When** that agent takes control again, **Then** the package's opening greeting is not repeated.
2. **Given** full history and known customer information cross the handoff, **When** the receiving agent continues, **Then** it does not ask for that information again.
3. **Given** the caller changed intent before the handoff, **When** the receiving agent starts, **Then** it follows the newest explicit intent rather than an older request.

---

### User Story 3 - Keep existing silent handoffs (Priority: P3)

A package author who does not configure an announcement keeps a silent agent
handoff. Existing packages remain valid and do not gain new speech.

**Why this priority**: The new behavior is an option. Existing packages may
deliberately use silent transitions and must not change without an author
choice.

**Independent Test**: Compile an existing handoff package with no announcement
and verify that no handoff line is added while the receiving agent still
continues without an opening re-greeting.

**Acceptance Scenarios**:

1. **Given** an existing handoff with no announcement, **When** it runs, **Then** no new handoff line is spoken.
2. **Given** an existing package with no announcement, **When** it is validated and compiled, **Then** it remains valid without edits.

### Edge Cases

- A handoff whose required variables are missing is refused before any announcement is spoken.
- An empty or whitespace-only configured announcement is rejected during package validation.
- A handoff announcement does not ask a question or start the receiving agent's work.
- A return handoff to the entry agent does not replay the call-start greeting.
- Two successful handoffs in one conversation each speak only their own announcement once.
- Caller speech during the short announcement does not cause the receiving agent to start before the announcement finishes.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A package author MUST be able to configure one optional plain-text announcement sentence on each agent handoff.
- **FR-002**: A configured announcement MUST be non-empty.
- **FR-003**: The active agent MUST speak the configured announcement exactly once and unchanged for each successful handoff.
- **FR-004**: The announcement MUST finish before control passes to the receiving agent.
- **FR-005**: The runtime MUST add no question or receiving-agent work to the authored announcement.
- **FR-006**: Transfer requirements and other refusal gates MUST run before the announcement, so a refused handoff produces no false transfer message.
- **FR-007**: Omitting the announcement MUST preserve silent handoff behavior and MUST NOT invalidate an existing package.
- **FR-008**: A handoff into the entry agent after call start MUST NOT replay the package's opening greeting.
- **FR-009**: The receiving agent MUST continue from the context selected by the handoff and MUST treat the caller's latest explicit intent as current.
- **FR-010**: Information carried across the handoff MUST remain available so the receiving agent does not ask for known values again.
- **FR-011**: Both shipped code targets MUST honor the same ordering: passed gate, completed announcement if configured, control change, receiving-agent response.
- **FR-012**: The generated runbook and source example MUST explain both announced and silent handoffs and how to test their spoken order.

### Key Entities

- **Agent handoff**: A one-way change from the active agent to another declared agent, with its existing trigger, requirements, and context policy.
- **Handoff announcement**: One exact author-owned sentence spoken by the active agent before control changes.
- **Call-start greeting**: The package's opening speech, which belongs only to the start of a call and never to a later return handoff.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In five consecutive live handoffs on each shipped target, the configured announcement is heard unchanged and exactly once before the receiving agent speaks.
- **SC-002**: In five consecutive round-trip handoff calls on each shipped target, the opening greeting is heard exactly once per call.
- **SC-003**: In the round-trip customer workflow, both targets reuse the known customer and complete the caller's latest request without asking for the phone number again.
- **SC-004**: After a handoff is selected, the caller hears either the configured announcement or the receiving agent within two seconds, with no unexplained silent gap longer than two seconds.
- **SC-005**: All existing packages without announcements validate and compile without edits and gain no new handoff speech.
- **SC-006**: Live handoff runs on both targets complete with zero provider errors, runtime exceptions, duplicate transfers, or actions based on invented tool results.

## Assumptions

- The first version uses one fixed author-written sentence per handoff. Variable-templated announcements are outside this feature.
- Announcements are short voice sentences and are allowed to finish without interruption so the caller always knows the handoff happened.
- This feature covers handoffs between voice agents in one session. Phone or SIP transfers to people are separate features.
- Existing context selection and transfer requirement rules stay authoritative.

## Backpropagated Defects

| ID | Date | Root cause | Invariant |
|---|---|---|---|
| B1 | 2026-08-15 | Pipecat invoked a generated direct tool with an undeclared keyword, raised before the handler could answer, and left the model with an in-progress result | V1 |
| B2 | 2026-08-15 | Pipecat rendered `announce` through the shared LLM context and treated a source-pipeline flush as a bus-wide playout barrier; target activation overtook the source turn, so both agents generated and one transfer produced four cues | V2 |

## Verification Invariants

- **V1**: Every generated Pipecat direct-tool invocation MUST produce one terminal result when provider arguments are unexpected or the handler fails before producing a result; it MUST NOT leave the call in progress or let the agent claim success from an invented result.
- **V2**: For every successful agent handoff with decoded announcement `A`, after refusal gates exactly one speech operation MUST receive `A` unchanged, no LLM call may render `A`, completed source playout MUST be observed before target activation, and only the receiving agent may start the continuation inference. Omission MUST schedule no announcement speech.
