# Research: Brief the manager, then hand the call over

All source readings on 2026-08-12, from the local reference checkout at
`/Users/nicoferdi/Documents/GitHub/livekit_agent`, whose `version.py` reports
**1.6.4**. The deployment reports **1.6.9**. R1 owns that gap and everything below
inherits it.

---

## R1. The version gap

**Status: CLOSED 2026-08-12. It caught a break that would have shipped an unimportable
`agent.py`.**

**How it closed**: the locally built image `unmute-lk-fixed:latest`, built from a compiled
`examples/human-transfer`, carries `livekit-agents` **1.6.9** at
`/usr/local/lib/python3.12/site-packages/livekit/agents/`. Its
`beta/workflows/warm_transfer.py` was copied out and diffed against the 1.6.4 checkout,
its `utils.py` and `__init__.py` were read, and the resulting shape was exercised inside
the image against the real package rather than inferred.

**The finding, and it is the whole reason this item existed:**

> In 1.6.9 the instruction hook class is **`WorkflowInstructions`**, not `InstructionParts`.
> The old name does not exist anywhere in the installed package: a grep across the whole
> `livekit` tree returns nothing, so it is a rename with no alias and no deprecation
> shim.

A template written from the 1.6.4 reading would have emitted
`from livekit.agents.beta.workflows import InstructionParts`, and every generated project
would have died at import with `ImportError`. The offline suite would not have caught it,
because the emitter's tests read emitted text and the smoke layer that installs the real
package is environmentally broken on this machine. It would have been found on the next
live deploy, which is the fourth live call in a row spent on something an hour of reading
could have prevented. The published documentation would not have caught it either: the
page the author supplied on 2026-08-12 shows the Python example with no instruction class
at all, and names the parts object only for Node.

**What the rename costs**: nothing but the name. Verified by running it inside the image:

- `WorkflowInstructions(persona=..., extra=...)` takes the same two keyword arguments.
- `persona` still overrides the identity section only, `extra` is still appended, an empty
  string still removes a section, and an unset field still keeps the platform default.
  Its docstring says so and the resolve call proves it: with a template of
  `{persona} / {_conversation_history} / {extra}` and a persona supplied, the persona
  replaced the default and nothing else moved.
- It now subclasses the platform's own instructions type and takes an `audio` first
  parameter, but that is positional and optional, so keyword use is unchanged.
- The task now calls `instructions.resolve(template=..., default_persona=..., ...)` instead
  of formatting the template itself. Internal, and it produces the same result.

**Everything else is byte-identical between 1.6.4 and 1.6.9.** The diff over
`warm_transfer.py` is exactly the rename and its three call sites. So every other claim in
R2 through R5 stands as read:

| Claim | Requirement it carries | Verdict on 1.6.9 |
|---|---|---|
| The instruction hook exists with a persona and an extra section, and the extra-instructions parameter is still deprecated | FR-002, plan D2 | **Confirmed, with the class renamed to `WorkflowInstructions`** |
| The persona replaces only the identity section of the platform's own template | FR-001, FR-004, FR-005 | Confirmed, and exercised against the real package |
| The transcript is interpolated into the instructions once, at construction, and the manager-facing agent starts with an empty history | FR-003, FR-006 | Confirmed, unchanged |
| The task does not speak before the human does | the framing of US1 | Confirmed, unchanged, same comment in the same place |
| The awaited result is shielded, and the cleanup entry point is private | FR-010, the recorded exception in the plan | Confirmed, unchanged |
| The internal steps are logged at debug | FR-006, R7 | Confirmed, unchanged |

**Two extra shapes verified in the same pass**, both load bearing for FR-006: the agent's
read-only conversation view exposes `items`, so `len(chat_ctx.items)` is a valid count,
and it agrees with the platform's own `messages()` walk on a two-message context.

**The lesson worth keeping**: the version gap was not a formality. Reading a checkout
that is five patch releases behind the deployment produced one confident, wrong,
unimportable line. The image built from the compiled example is the cheapest source of
truth available in this repository, and it should be the first place any future claim
about the emitted project's dependencies is checked.

---

## R2. The instruction hook

**Decision**: pass `instructions=WorkflowInstructions(persona=..., extra=...)`. Stop passing
`extra_instructions`. The class name is the one thing R1 corrected, so it is written here
in its 1.6.9 spelling and nowhere in its old one.

**Rationale**: read from `beta/workflows/warm_transfer.py`. The constructor lists
`extra_instructions` under a `# deprecated` comment beside `target_phone_number`, which
is already documented as deprecated. When no instructions are given, the task builds
`WorkflowInstructions(persona=PERSONA, extra=extra_instructions)` itself, and when both are
given it logs a warning and ignores the deprecated one. So the two are the same lever
with one supported spelling and one on the way out.

`WorkflowInstructions` is exported from `livekit.agents.beta.workflows`, the same module the
emitted code already imports the task from, so this costs one more name on an existing
import line. Its docstring is explicit about the shape: each field overrides that section
when set, an unset field keeps the built-in default, and an empty string removes the
section. Persona is "who the agent is and how it behaves"; extra is "extra instructions
appended to the prompt".

The platform's template then reads, in order: the persona, a context paragraph that says
the user is the human agent and the caller is the transcript, the transcript itself, a
sentence naming the confirm tool, a sentence telling the model to start with a summary,
and finally the extra section. Two consequences the design leans on:

- Replacing the persona leaves the transcript wiring and the confirm-tool sentence alone.
  Both work. Only the identity was wrong.
- The extra section lands **last**, which is the strongest position in the prompt, and it
  is where the package's authored briefing already goes.

**Alternatives considered**: overriding the full instruction string, which takes ownership
of the transcript interpolation for no gain (rejected in plan D2); leaving the deprecated
parameter in place, which works today and produces a package that breaks on an upgrade
nobody will connect to this file.

---

## R3. What the transcript actually carries

**Decision**: keep handing over the agent's live conversation, and log how many messages
were handed over.

**Rationale**: `_format_conversation_history` walks the chat context's messages, keeps
only the caller and agent roles, skips any message with no text content, and builds a
plain "Caller:" and "Assistant:" transcript. Tool calls, tool results and captured
structured state are dropped. The result is interpolated into the instructions **once**,
in the constructor. The manager-facing agent is then started with an **empty** chat
context: the task passes an empty one to its base with the comment "don't pass the
chat_ctx", and hands that same empty one to the agent it builds. So the transcript exists
for the manager-facing model only as text inside its system prompt, and nothing added to
the caller's conversation after the transfer fires can reach it.

The emitted code passes the agent's `chat_ctx`, which is a live read-only view over the
agent's own items rather than a snapshot taken at construction. That is the right source
and the emitted code already uses it, so FR-003 is a test to write, not a line to change.

**Why the count is logged and not the transcript**: the transcript is the caller's words.
Putting them in an operator log would be a new place for a caller's personal details to
land, for no diagnostic gain that the count does not already give. Zero messages and
twelve messages have different fixes; the words themselves change nothing.

**Why the count is of what we hand over, not of what survives the filter**: re-implementing
the filter in the emitted code to log a "true" count would create a second copy of the
platform's rule, which principle III forbids and which would drift on the next upstream
change. The log line therefore says precisely what it counts.

---

## R4. What the emitted code can and cannot see

**Decision**: log the control firing, the dial request with the message count, and the
outcome with elapsed seconds. Do not claim to log the moment the manager answered.

**Rationale**: the answer is awaited inside the task, through the platform's
answer-blocking participant creation, and nothing is exposed for it. The task's own line
for the surrounding step is at debug. The awaited result arrives only on a merge, and
every other ending arrives as one error. So from outside there are exactly three
observable moments, and the design uses those three.

This narrows User Story 2's first acceptance scenario, which lists "the manager answered"
as a phase. What the log gives instead is enough to tell the four cases apart:

| What happened | What the log shows |
|---|---|
| Answered and accepted | dial line, then merged with an identity and a duration |
| Never answered | dial line, then unavailable with the platform's timeout reason, duration near the ring timeout |
| Answered and declined | dial line, then unavailable with the decline reason |
| Answered and never decided | dial line, then unavailable with the decline reason and a duration well past the ring timeout |

**Alternative considered and rejected**: raising the framework logger to debug in the
emitted package. It would produce the answer line, along with every other debug line the
whole framework emits for the whole session, on every call. That trades one wanted line
for an unreadable log, which is a worse version of the problem this story exists to fix.

---

## R5. Why there is no hard bound

**Decision**: no timeout is introduced. The persona is told to use the platform's decline
tool when the manager goes quiet or never answers the question, which routes the stuck
case into the package's declared unavailable behaviour.

**Rationale**, all read from source:

- The answer timeout covers **ringing only**. Its own docstring says the task completes
  with an error when the timeout elapses waiting for the human agent to answer. Nothing
  bounds the conversation after that.
- The merge happens if and only if the manager-facing model calls the confirm tool.
  Decline and voicemail are that model's calls too. There is no fourth exit.
- The awaited value comes back through `await asyncio.shield(self.__fut)`. A shield means
  cancelling the awaiting side does **not** cancel the inner future. So wrapping the await
  in a timeout would raise on our side while the consultation carried on: the cleanup path
  never runs, so the hold music keeps playing and the caller's audio stays disabled, and
  the manager stays connected to a briefing agent. That is worse than the unbounded hold
  it was meant to fix, and it is precisely what FR-011 forbids.
- The cleanup path that stops the music and restores the caller is private, and it is the
  only thing that does both. The public completion call skips it.

So the exit has to come from inside the consultation, and the platform already provides
one: the decline tool, which runs the full cleanup and surfaces as the same error every
other unavailable outcome uses. Telling the model to use it is the only lever that is
both public and correct.

**Residual risk, stated rather than hidden**: it is a prompt, so it is probabilistic. A
model that neither confirms nor declines still leaves the caller on hold. The mitigation
makes that less likely and the R4 logging makes it visible when it happens. A real bound
needs either the private cleanup call or an upstream change, both listed in the plan's
Complexity Tracking, and the choice is the user's.

**Action for the record**: submit documentation feedback upstream asking for a
post-answer bound, since the flow's own documentation promises the caller comes back when
the transfer does not happen, and today that promise holds for ringing only.

---

## R6. Why the briefing failed on the live call

**Decision**: do not pick one cause. Fix the two that are provably wrong, and make the
log discriminate the rest on the next call.

The reported symptom: the manager answered, heard nothing, said hello, and got a generic
assistant reply instead of a briefing. Three causes fit the same symptom, and the
deployment's logs cannot tell them apart today, which is the whole reason User Story 2
exists.

| Hypothesis | Standing | What the new log line settles it with |
|---|---|---|
| The transcript handed over was empty or nearly so, so the model was asked to summarise nothing and improvised a greeting | **Most likely.** It explains the exact wording and it explains "I am not briefed on what happened", because there would have been nothing to brief. | The handed-over message count. Zero or one settles it immediately. |
| The instructions arrived but a small model did not follow them. The package configures a small language model, and the manager-facing session reuses it. | **Plausible.** The instruction to summarise sits mid-prompt while a conversational hello arrives as the only user turn. | A healthy message count plus a greeting means this one, and the persona change is the fix. |
| The manager was hearing something other than the briefing agent | **Unlikely, kept on the list.** The generated greeting for this package is "Sage and Stone Salon, this is the front desk. How can I help?", which is close enough to the report to be worth ruling out rather than assuming. The deployed worker registers under an agent name, so a room the prebuilt creates should not dispatch a second job, but that was not verified against the deployment. | The absence of a second "session started" line in the same window rules it out. Confirming it needs the deployment, not source. |

Both fixes land regardless: the persona addresses the second hypothesis and instructs the
model what to say under the first, and the count makes the first visible. The third is a
verification task, not a code change.

---

## R7. The logging level question

**Decision**: do not touch the emitted logging setup. Add our own info lines instead.

**Rationale**: the emitted package sets the `livekit` logger to info, and the framework's
lines come from a child of it, so on the face of it that line is what hides the debug
steps. But the framework's own command-line runner configures logging too, and its default
level is info regardless. Which of the two is the effective filter was not established,
and lowering a level to find out would turn every deployed call's log into framework debug
output.

Since the useful lines are the ones we can emit ourselves at info, the question does not
need answering to deliver the story. It is recorded here so that nobody later "fixes" the
emitted logging setup believing it to be the cause, and so that the option stays on the
record if a future defect genuinely needs framework internals visible. If it is ever
wanted, the right shape is a documented opt-in environment variable in the generated
project, not a default.

---

## R8. What `LIVEKIT_SIP_INBOUND_TRUNK` is for, and what it is not

Added 2026-08-12, after the first live cold test. The author challenged the claim that
this variable is needed for cold transfer, on the grounds that the platform's call
forwarding page never mentions it. **The challenge is upheld.**

**Decision**: the variable is a provisioning input for the inbound call path, nothing
else. It is not part of cold transfer, it is not read by the deployed agent, and it is
not a deploy blocker. An earlier reply in this feature's session called its absence from
`.env` a deploy failure, and that framing was wrong.

**Evidence, all from 2026-08-12:**

- The platform's cold transfer page (`docs.livekit.io/telephony/features/transfers/cold.md`,
  rendered 2026-08-12) never names it, because `TransferSIPParticipant` takes a room name,
  a participant identity and a destination. No trunk argument exists. The REFER rides the
  SIP session the caller is already on, so LiveKit knows the trunk from the call itself.
- `grep -c LIVEKIT_SIP_INBOUND_TRUNK build/livekit/agent.py` returns **0**. The runtime
  never reads it. It appears only in `sip-dispatch-rule.json` (as the `trunk_ids`
  placeholder), the README step that materializes that file, `.env.example`, the compile
  report, and the local dev compose.
- In the compiler it is `internal/generate/livekit_v1.go` line ~688 (the dispatch rule
  placeholder) and `livekit_v1_build.go` (the env listing); `internal/ir` classifies it as
  dev-supplied, which is why `unmute dev --telephony` fills it automatically.

**What it actually does**: the generated dispatch rule must be scoped to one inbound
trunk, because a dispatch rule with no `trunk_ids` matches every trunk in the project,
and this project (`slng-atlas`) is shared and holds production trunks. The variable is
the author's handle for "my trunk" when running the one-time
`envsubst | lk sip dispatch create` step from the generated README. After that step it
has no further use on a Cloud deployment.

**Why it is still in `.env.example`**: the README's provisioning commands read it from
the shell, and `unmute dev --telephony` supplies it. That placement is defensible but the
section does not say "one-time provisioning, not a runtime secret", and this session
proves the omission misleads. Follow-up recorded in tasks.md: label it in the emitted
`.env.example`, out of this feature's scope (FR-016, no authoring or classification
change).

**The related by-design fact**: cold transfer cannot be exercised from the Agent Console,
because there is no SIP participant to refer. The platform's own example on the same page
carries the identical guard (`if sip_participant is None: return "no active SIP caller to
transfer"`). Warm works from the console because it dials out. The emitted line
`cold transfer skipped: no phone caller in the room` (contract 2alt) is this fact made
visible in a log.

---

## R9. Can the trunk variable go away entirely, the way the outbound trunk did?

Asked by the author on 2026-08-12, right after R8: outbound dialling needs only the four
carrier values since N33, so why should inbound keep a LiveKit-named variable at all?

**Decision, two halves.**

**The platform records cannot go away.** An unsolicited INVITE arrives with no request of
ours for configuration to ride on, so the platform must already hold two facts: this
number belongs to this project (the inbound trunk), and calls to it go to this agent (the
dispatch rule). The telephony overview says this in its own words: LiveKit SIP exists "to
respond to SIP requests, mediate trunk authentication, and match dispatch rules". This is
the same asymmetry N33 recorded, and no carrier setting changes it. The carrier's side is
one manual fact too: the Twilio trunk's origination URI must point at the project's SIP
endpoint.

**The variable can go away.** Read from `livekit_sip.proto` in the public protocol
repository on 2026-08-12, `SIPDispatchRuleInfo` has three scoping fields:

- `trunk_ids` (field 3): what we emit today, documented on the dispatch rule page.
- `inbound_numbers` (field 7): "will only accept a call made **from** these numbers".
  Caller filtering. Not useful here.
- `numbers` (field 13): "will only accept a call made **to** these numbers". This is the
  package's own from-number, which the Connection already declares.

Two shapes follow:

- **Shape A, cleanest**: emit the dispatch rule scoped by `numbers` with the from-number
  placeholder instead of `trunk_ids`. Both provisioning files then materialize from
  values the package already holds, and `LIVEKIT_SIP_INBOUND_TRUNK` leaves every surface.
  **Gated (principle IV)**: the field exists in the protocol with an explicit comment,
  but the dispatch rule documentation page does not describe it and nothing here proves
  the Cloud SIP service enforces it. The failure mode of an unenforced scope is a rule
  with empty `trunk_ids`, which the docs say matches **every** trunk in the project.
  `slng-atlas` is shared and carries production trunks, so an unenforced scope would
  steal live calls. Verify in a throwaway project first, never in this one.
- **Shape B, documented behaviour only**: keep `trunk_ids`, but resolve the ID
  mechanically at provisioning time. `ListSIPInboundTrunk` filters trunks by number, so
  the README's commands can look the ID up from the from-number and pass it through a
  shell variable that never lands in `.env`, `.env.example`, the required-env list or
  the compile report.

**Scope**: either shape moves `sip-dispatch-rule.json`, the env classification in
`livekit_v1_build.go`, the README template, the `internal/ir` dev-supplied list and the
frozen goldens, so this is its own feature, not an amendment to 003. Until then the
operator's workaround is already real: export the ID in the shell for the two
provisioning commands and never store it.
