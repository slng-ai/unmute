# Feature Specification: Brief the manager, then hand the call over

**Feature Branch**: `feature/warm-cold-human-transfer` (feature dir `003-warm-transfer-briefing`)

**Created**: 2026-08-12

**Status**: Draft

**Input**: User description: "After the cold and warm transfer work we just committed, I am receiving the call as supervisor. The problem is that the agent is not saying anything and looks like it's a simple agent that is speaking, saying 'hello how can I help'. So I am not briefed on what happened with the user and I am not put through with the user. Good note: music is played correctly."

## Why this exists

Feature 002 got the manager's phone to ring. This feature is about what the manager hears when they pick it up.

On a live LiveKit Cloud call on 2026-08-12, from a package compiled on this branch, the warm transfer dialled the supervisor and the supervisor answered. The caller was held with music, which worked. The supervisor then heard nothing, said hello, and got a generic assistant reply, something like "hello, how can I help". They were never told who was calling or why, and they were never connected to the caller. The caller stayed on hold.

So the plumbing works and the conversation does not. That is a different kind of defect from the last two, and it needs a different kind of fix: the last two were a missing argument and a stray return value, both provable by reading the emitted file. This one lives in a prompt, in a second model turn, in a room the operator cannot see.

Which brings up the second half of this feature. **We do not actually know why the briefing did not happen.** The prebuilt logs every internal step of the transfer at debug level: connecting to the consultation room, connecting to the caller, moving the participant across. A deployed agent shows info and above. So `lk agent logs` for a failed warm transfer and a successful one look identical, and both look like nothing happened at all. Three live tests on this branch have each produced a different theory about what went wrong, and each time the theory came from reading framework source rather than from the deployment. That is the actual bottleneck, and it is a handful of lines to fix.

There is a third thing the live call exposed, which nobody has hit yet only because the tests keep failing earlier. The transfer ends when the manager-facing model decides to connect the two calls. If it never decides, nothing ends it. The answer timeout covers ringing only. So a manager who answers, chats, and never confirms leaves the caller on hold with no bound at all. Planning found that the platform gives no way to bound it from outside without leaving the caller muted with music playing, so what this feature can do is give the briefing model a reason to end the consultation itself. That is a better outcome than today and it is still a model reaching a conclusion, which is stated here rather than sold as a fix.

## Verified platform contract *(source of truth for this feature)*

Verified 2026-08-12 against `livekit-agents` **1.6.9**, the version the deployment runs, read out of the image built from a compiled `examples/human-transfer` and exercised inside it. The rows were first read from a 1.6.4 checkout at `/Users/nicoferdi/Documents/GitHub/livekit_agent`, and the third column records what the 1.6.9 check then covered, per Constitution principle IV.

**All rows were confirmed unchanged except one: the instructions hook class was renamed to `WorkflowInstructions`, with no alias and no deprecation shim.** A template written from the 1.6.4 reading alone would have emitted a project that failed at import, and neither the offline suite nor the published documentation would have caught it. See [research R1](./research.md#r1-the-version-gap). The two documentation pages are the live ones the author supplied on 2026-08-12.

| Fact | Source | What the 1.6.9 check covered |
|---|---|---|
| The task dials the manager into a **separate room**, named the caller's room plus `-human-agent`, and starts a second session there. That session reuses the caller session's speech, language and turn-detection models. | `beta/workflows/warm_transfer.py`, `_dial_human_agent` | Room naming and model reuse |
| After the dial, the task **deliberately does not speak**. The code's own comment is `# let the human speak first`. So the manager hears silence until they say something, and the briefing can only be the model's reply to that. | `warm_transfer.py`, `on_enter` | Whether 1.6.9 still waits |
| The manager-facing agent's conversation history is **empty by design**: the task passes an empty context to its base with the comment "don't pass the chat_ctx", and builds the manager-facing agent with that empty context. The caller's transcript reaches the model **only** as text interpolated into the instructions, once, at construction time. | `warm_transfer.py`, `__init__` and `_dial_human_agent` | That the transcript is still instruction-only |
| Only messages with a caller or agent role **and** text content survive into that interpolated transcript. Tool calls, tool results and anything the agent captured as structured state are dropped. | `warm_transfer.py`, `_format_conversation_history` | Filter unchanged |
| The default instructions tell the model to summarise the conversation and then to call the confirm tool once the manager agrees. The authored extra text is appended **last**. | `warm_transfer.py`, `INSTRUCTIONS_TEMPLATE` | Template wording |
| `extra_instructions` is **deprecated**. The supported hook is `WorkflowInstructions`, carrying a persona and an extra section, exported from the same module as the task itself. Passing both logs a warning and ignores the deprecated one. **The class was called `InstructionParts` in 1.6.4 and that name exists nowhere in 1.6.9.** | `warm_transfer.py` signature, `beta/workflows/__init__.py`, `beta/workflows/utils.py` | Deprecation still in force, export path, and the rename |
| The calls merge **if and only if** the manager-facing model calls the confirm tool. Decline and voicemail are the model's calls too. There is **no timeout after the answer**: the answer timeout covers ringing only. | `warm_transfer.py`, `connect_to_caller` / `decline_transfer` / `voicemail_detected` | That no post-answer bound was added |
| Every unavailable outcome arrives at the caller side as one error, with the caller's audio restored and the hold music stopped first. | `warm_transfer.py`, `_set_result` | Restore behaviour |
| The manager can only accept by talking to a model. There is **no keypad accept**. | `warm_transfer.py` tool list | Tool list unchanged |
| Every internal step is logged at **debug**: connecting to the consultation room, connecting to the caller, moving the manager across. Only a failed dial and a caller disconnect are info or above. | `warm_transfer.py` | Levels unchanged |
| The documented flow says the agent gives the manager context (step 3) and then the manager is connected to the caller (step 4). So the briefing is part of the contract, not a nicety. | [Agent-assisted warm transfer](https://docs.livekit.io/telephony/features/transfers/warm.md) | n/a, doc |
| A caller who hangs up mid-transfer can be handled with a dedicated instruction **in Node only**. Python does not expose it. | [WarmTransferTask](https://docs.livekit.io/agents/prebuilt/tasks/warm-transfer.md) | n/a, doc |

One fact about our own emitted code, read on 2026-08-12 from `examples/human-transfer/build/livekit/agent.py`, the artifact that was actually deployed:

| Fact | Where |
|---|---|
| The emitted package sets the `livekit` logger to info. The prebuilt's debug lines are children of it. Whether that line or the runtime's own default is what filters them out has to be established before anything is changed about it. | `agent.py`, logging setup |
| The emitted warm transfer passes the deprecated extra-instructions parameter, and passes the agent's live conversation as the transcript source. | `agent.py`, the warm transfer tool |

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The manager hears why they were called (Priority: P1)

A supervisor picks up, says hello, and the first thing the agent says is who is on hold, what they are unhappy about, what has already been offered, and one question the supervisor can answer with yes or no. They never have to ask what this call is about.

**Why this priority**: It is the reported defect and the whole point of a warm transfer. A warm transfer whose briefing does not arrive is a cold transfer with extra steps and worse hold music.

**Independent Test**: Deploy the compiled `examples/human-transfer`, call it, give a name, a stylist and a complaint, ask for a manager, answer the supervisor's phone, say hello, and listen to the first sentence.

**Acceptance Scenarios**:

1. **Given** a caller who gave their name, a stylist and a complaint, **When** the manager answers and says hello, **Then** the agent's first turn contains all three and ends with a question the manager can answer, and it is not a greeting or an offer to help the manager.
2. **Given** the package's authored briefing text, **When** the emitted project is read, **Then** that text reaches the manager-facing prompt through the platform's supported instruction hook, and the deprecated parameter appears nowhere.
3. **Given** a transfer that fires after a caller said almost nothing, **When** the manager answers, **Then** they are told a caller is waiting and that the details are missing, rather than being greeted as if they were the caller.

---

### User Story 2 - A live transfer can be read from the logs (Priority: P2)

Someone runs `lk agent logs` after a transfer and can say, without reproducing the call, which phase it reached, how much of the conversation the briefing received, and how it ended.

**Why this priority**: Second in value, but it has to be built **first**. It is the smallest change in this feature and it is the only reason the other two are currently guesswork: on the evidence so far, a transfer that dialled and failed to brief produces the same log output as one that worked, which is to say none. The tasks phase should order it ahead of User Story 1 even though it ranks below it here.

**Independent Test**: Force each outcome once (manager answers and accepts, manager declines, nobody answers) and read the log for each without watching the call.

**Acceptance Scenarios**:

1. **Given** a warm transfer that completes, **When** the log is read, **Then** one line per **observable** phase is present at the level a deployed agent shows by default: the control fired, the dial went out along with how much conversation the briefing received, and the calls merged along with how long it took. The moment the manager answered is deliberately not among them: the platform awaits it internally and exposes nothing for it, and it is inferable from the outcome plus the duration. Narrowed during planning on 2026-08-12 rather than quietly dropped, with the rejected alternative on the record in [plan D3](./plan.md#d3-three-lines-per-transfer-no-destination-values).
2. **Given** a warm transfer that ends without a merge, **When** the log is read, **Then** a distinct line names the reason, and it cannot be mistaken for a completed transfer.
3. **Given** any warm transfer, **When** the log is read, **Then** it says how many turns of the caller's conversation the briefing was built from, so a briefing with nothing in it is visible without reproducing the call.
4. **Given** any emitted log line, **When** it is inspected, **Then** it carries no credential and no full destination number: a destination is identified by the environment variable name that holds it.

---

### User Story 3 - The caller is not left on hold when the manager does not decide (Priority: P3)

A caller who asked for a manager comes back to the agent when the manager does not take the call, without the manager having to say no out loud.

**Why this priority**: It is a real hole and it is nobody's fault yet, because the earlier defects always ended the transfer before this one could bite. Lowest because it needs the other two to be observable before it can be trusted, and because a manager who answers usually decides.

**Not independently deliverable, stated rather than assumed**: this story ships as one paragraph of the manager-facing prompt that User Story 1 introduces, plus its own test and its own documents. It is independently *testable*, which is what accepting it needs, but it cannot be built before User Story 1's prompt exists.

**Independent Test**: Answer the supervisor's phone, say hello, then talk about anything except taking the call. Never say yes and never say no.

**Acceptance Scenarios**:

1. **Given** a manager who answers and never confirms, **When** they let the conversation move on without answering, **Then** the briefing model declines on their behalf with a reason, and the package's configured unavailable behaviour runs, the same one that already handles nobody answering.
2. **Given** that decline, **When** the caller's side is examined, **Then** the hold music has stopped and the caller's audio is restored, exactly as on a failed dial, because it is the platform's own unavailable path and not something this feature arranges.
3. **Given** a normal consultation of ordinary length, **When** it completes, **Then** nothing cuts it short, because there is no timeout to fire.
4. **Given** a manager who answers and says nothing at all, **When** the consultation is examined, **Then** this story does **not** cover it: with no turn to act in, the model cannot decline and the caller keeps holding. Recorded here so the limit is a known one rather than a surprise.

### Edge Cases

- **The manager answers and says nothing at all.** Nobody speaks, so the model never gets a turn and can never brief, confirm or decline. This is the one case the best-effort exit cannot reach, because it needs a turn to act in, and it is the strongest argument for the hard bound that is now out of scope. User Story 3 covers the cases where the manager does speak.
- **The caller hangs up while on hold.** The manager is then talking to a briefing agent about a call that no longer exists. The platform exposes an instruction for this in Node and not in Python, so it is out of scope here and named as such rather than guessed at.
- **Voicemail answers the manager's phone.** The model has a tool for it and must choose to use it. Machine detection is not applied to the manager's leg. Out of scope, named.
- **An empty or near-empty transcript.** Covered by FR-004. It is also the most likely explanation of the reported call, which is exactly why FR-006 exists: the log has to make it visible instead of leaving it to be inferred.
- **The briefing model is the call's model.** The manager-facing session reuses the models the package already configures, and the shipped example configures a small one. This feature must not add a model knob for the briefing; if a briefing needs a stronger model, the package changes the agent's model. Named in Assumptions.
- **A second phone participant in the room after a merge.** The cold transfer picks the first phone participant in the room. After a warm merge the session ends, so no cold transfer can follow it, and on the return-to-caller path the manager's leg is already gone. Nothing to change, but no change here may break that.
- **The reference checkout is a different version from the deployment.** Every claim read from source carries that gap, and FR-013 owns closing it.

## Requirements *(mandatory)*

Identifiers are stable references and are never renumbered.

### The briefing

- **FR-001**: The manager's **first** agent turn MUST be the briefing: who is on hold, why they called, what has already been offered or attempted, and one question the manager can answer. It MUST NOT be a greeting, and MUST NOT offer to help the manager.
- **FR-002**: The package's authored briefing text MUST reach the manager-facing prompt through the platform's supported, non-deprecated instruction hook. The deprecated parameter MUST NOT appear in any emitted file.
- **FR-003**: The briefing MUST be built from the conversation the caller actually had, taken at the moment the transfer fires. The emitted code MUST hand over the live conversation, not an initial or empty one, and a test MUST pin which one it hands over.
- **FR-004**: When the transcript carries no caller words, the manager MUST still hear a usable opening that says a caller is waiting and that the details are not known, rather than a generic greeting. A thin briefing is a degraded transfer; a greeting is a broken one.
- **FR-005**: The manager MUST be told, in that first turn, what to say to take the call. Whether the calls merge is the manager-facing model's decision alone, so the manager has to be given the words that lead to it.

### Observability

- **FR-006**: Every **observable** phase of a human transfer MUST emit exactly one line, at the level a deployed agent shows by default: the control fired, the dial or referral went out along with the number of conversation **messages** the briefing was built from, and the outcome along with how long it took. The moment the manager answered is deliberately not among them: the platform awaits it internally and exposes nothing for it, and turning on framework debug to get it was rejected in [plan D3](./plan.md#d3-three-lines-per-transfer-no-destination-values). Messages rather than turns, because a message is the unit the conversation is stored in and therefore the only count the emitted code can take honestly. The count is not decoration: it is the difference between a briefing that said nothing and a model that ignored its instructions, and no other signal separates them.
- **FR-007**: A transfer that ends without a merge MUST log a distinct line naming the reason, distinguishable at a glance from a completed one.
- **FR-008**: An emitted log line MUST NOT contain a destination in **any** form: no phone number, no SIP URI, and no environment variable value either. It does not need one: the control's own name on the first line already says which destination fired. Credentials are forbidden everywhere by FR-015, which covers log lines too, so this requirement owns destinations only.
- **FR-009**: The cold transfer MUST gain the same three-line shape, minus the briefing message count, which it has no equivalent of. It MUST gain nothing else: its behaviour MUST NOT change.

### The bounded hold

- **FR-010**: **Rewritten 2026-08-12, after planning, and the original wording is quoted in FR-011 so the change is auditable.** A caller on hold MUST have an exit that does not depend on the manager saying yes. The manager-facing prompt MUST instruct the model to decline the transfer, with the manager's reason, when the manager says they cannot take it, goes quiet, or lets the conversation move on without answering. Declining runs the package's configured unavailable behaviour, the same one that already handles nobody answering. This is a **best-effort exit, not a bound**: the platform offers no timeout once the call is answered, and [research R5](./research.md#r5-why-there-is-no-hard-bound) shows that imposing one from outside leaves the consultation running while the caller sits muted with hold music playing, which is worse than the problem it was meant to fix. The hard bound is out of scope, with both of its possible mechanisms named under Assumptions.
- **FR-011**: ~~When that bound fires, the caller's audio MUST be restored and the hold music stopped, on the same terms as every other unavailable outcome. A bound that returns the caller to silence is worse than no bound.~~ **Withdrawn 2026-08-12 with FR-010's rewrite.** There is no bound to fire. What it asked for is exactly what the platform's own decline path already does, so nothing in this feature has to arrange it. Kept as history rather than deleted, because its last sentence is the reasoning that rules out the naive timeout.
- **FR-012**: The exit MUST NOT add an authoring field. ~~It comes from what the package already declares or from a constant stated in the emitted code with its reasoning next to it.~~ The constant half is withdrawn with FR-011: with no timeout there is no number to state, and **no timeout constant may be emitted**, because a constant that nothing enforces reads as a guarantee. The no-new-field half stands and is covered by FR-016.

### Discipline

- **FR-013**: Every platform claim MUST cite its source and its verification date, and MUST be verified against the version the deployment actually runs before it is relied on. The rows above were read from a 1.6.4 checkout while the deployment reports 1.6.9; closing that gap is part of this feature, not a footnote to it.
- **FR-014**: All emitted output MUST come from Go templates. No post-generation string surgery.
- **FR-015**: No emitted file, package file or report may contain a secret value. Environment variable names only, `UPPER_SNAKE`.
- **FR-016**: The authoring surface MUST NOT widen and MUST NOT break. A package written before this change MUST keep loading and compiling with no edit.
- **FR-017**: Goldens MUST be read, not regenerated blind.
- **FR-018**: The Pipecat driver and its Daily warm transfer MUST NOT change.
- **FR-019**: The offline test layer MUST pin the **shape of the emitted code** that implements each requirement above, because the end-to-end proof is a live call that cannot run in CI. It cannot pin an outcome that depends on a model's words, only the instruction that asks for them, and that instruction is what it MUST reach in every case.

## Key Entities

- **Warm transfer prebuilt**: the platform task that holds the caller, dials the manager, runs the consultation and merges the calls. This feature configures and observes it. Whether it stays the mechanism is the plan's decision, and FR-001 is written as an outcome so that either answer satisfies it.
- **Consultation**: the second room and second session where the manager is briefed. Invisible to the operator today, which is the substance of User Story 2.
- **Briefing**: the authored text plus the caller's transcript, interpolated into the manager-facing prompt once, at construction time.
- **Manager decision**: accept, decline, voicemail, or no decision. The first three are the model's tool calls. The fourth is what User Story 3 bounds.
- **Transfer log record**: the sequence of lines a single transfer leaves behind. Currently near-empty; after this feature it is the primary evidence for every other requirement.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On **three consecutive** live warm transfers, the manager's first agent turn names the caller and the reason for the call, and never opens with a greeting or an offer to help the manager.
- **SC-002**: **Zero** emitted files use the deprecated instruction parameter.
- **SC-003**: For **every** outcome the platform reports (merged, declined, no answer, voicemail), a reader of `lk agent logs` alone can name the phase reached and the reason it ended, without reproducing the call. A consultation that never ends produces no outcome line at all, and that absence is itself the signal; SC-005 owns that case.
- **SC-004**: The number of conversation messages the briefing was built from appears in the log for **every** warm transfer.
- **SC-005**: On a live call where the manager answers and never decides, the briefing model declines on their behalf and the caller returns to the agent. Measured over **three** attempts with the result of each written down, because this is a best-effort exit and its reliability is a number nobody has yet. An attempt that produces no outcome line is a failure and MUST be recorded as one, not retried quietly.
- **SC-006**: Hold music stops and caller audio returns on **every** unavailable path the platform reports, including a decline the briefing model makes on the manager's behalf.
- **SC-007**: **Zero** emitted files or log lines contain a credential value or a full destination number.
- **SC-008**: Cold transfer behaviour is unchanged, verifiable against its golden; only its log lines move.
- **SC-009**: A package written before this change compiles with **zero** edits, and **zero** authoring fields are added.
- **SC-010**: **Every** platform claim in this feature's documents cites a source and a date, and **every** one is verified against the 1.6.9 package the deployment runs. Met on 2026-08-12, and it earned its place: the check found one rename and corrected it before any code was written.

## Assumptions

- **The manager speaking first is kept, not fought.** The platform waits for the human on purpose, and a person answering a phone says hello. The defect is not the silence before hello, it is what comes after it. Any design that makes the agent speak into a ringing-then-answered line before the human has said anything is a change to how the call feels, and it is not what this feature is for.
- **The briefing uses the same models as the call.** The manager-facing session reuses the package's speech and language models. No per-transfer model knob is added. A package that needs a stronger model for the briefing changes the agent's model, which affects the caller too, and that tradeoff belongs to the author.
- **There is no bound, and that was decided rather than overlooked.** Accepted 2026-08-12, after planning found two things. The platform has no timeout once the manager answers. And imposing one from outside leaves the consultation running while the caller sits muted with hold music playing, because the awaited result is shielded from cancellation and the one path that stops the music and restores the caller is private. So the exit is a best-effort one through the platform's own decline tool. Two mechanisms would give a real bound and neither is taken here:
  - Call the platform's private cleanup method on a timeout. Deterministic, and it breaks on a rename with no deprecation, which is exactly what happened to the instructions hook between 1.6.4 and 1.6.9.
  - Own the hold music so it can be stopped, and leave the platform's task orphaned. That leaves a live manager-facing session and room behind, which is the class of bug `docs/SCHEMA.md` N31 records this repository already learning once.

  A live call where the best-effort exit does not hold is what reopens the choice, and it should reopen it with that observation attached.
- **The version gap is closed, and it was not a formality.** The contract table was first read from a 1.6.4 checkout while the deployment runs 1.6.9. Checking it against the real 1.6.9 package found the instructions hook renamed with no alias, which would have shipped a generated project that failed at import, and which neither the offline suite nor the published documentation would have caught. The cheapest source of truth for anything the emitted project depends on is the image built from a compiled example, and the recipe is now in [quickstart.md](./quickstart.md).
- **A live cloud call is the only end-to-end proof.** Carried unchanged from feature 002: both platforms' transfer features exist only where telephony runs in the cloud, the laptop routes carry plain audio with no transfer control, and this repository will not substitute its own machinery there. So the offline layer has to carry the shapes a live call will not reach.
- **The caller side already works.** Hold music, the spoken handoff line, the restore on failure and the silence after a merge are all confirmed live on 2026-08-12 and are not reopened here.
- Out of scope, each named on purpose rather than left to be discovered:
  - **A hard bound on the consultation.** Two mechanisms could give one and neither is taken here; both are named with their costs in the Assumptions bullet above.
  - The caller hanging up mid-consultation. The platform exposes the instruction for it in Node only.
  - A keypad accept for the manager. The platform's task offers no such tool.
  - Machine detection on the manager's leg.
  - The Pipecat driver, the connector route, and every route with no transfer primitive.
  - The cold transfer's behaviour. Its logging is in scope, its behaviour is not.
  - The `11LABS_API_KEY` line in the operator's own environment file, which no shell can export because the name starts with a digit. It is unrelated to transfers and it costs one line in the deploy log, but a secret name that cannot be exported is a fail-loud case this repository does not currently catch, and it deserves its own change rather than a corner of this one.
