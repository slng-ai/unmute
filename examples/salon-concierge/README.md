# salon-concierge

The full Sage and Stone Salon project, and the package to read when you want to
see every Unmute path working together in one agent.

The salon takes calls. The agent works out who is calling, books, moves and
cancels appointments, answers questions from the salon's own documents, writes
down complaints, and puts a caller through to a manager when they ask for one.

This example deliberately combines many features. Its variable count is not a
target to match: keep only the facts and saved results your own call needs.
See [choosing fewer variables](https://unmute.ai/build/variables#keep-only-the-values-the-call-needs).

On this page:

- [Quickstart](#quickstart) - validate, compile, talk
- [What you need](#what-you-need) - the values in `.env`
- [Structure](#structure) - what each path holds
- [The two agents and their tasks](#the-two-agents-and-their-tasks) - who does what
- [The booking flow](#the-booking-flow) - one group, two steps
- [Typed values and context](#typed-values-and-context) - what crosses a handoff
- [Before the greeting](#before-the-greeting) - the pre-fetch block
- [The manager transfer and the knowledge bases](#the-manager-transfer-and-the-knowledge-bases) - escalation and documents
- [Routes, tracing and the speech gateway](#routes-tracing-and-the-speech-gateway) - two telephony planes
- [Advanced](#advanced) - the dev page
- [Troubleshooting](#troubleshooting) - what goes wrong
- [Where to go next](#where-to-go-next) - harness and baseline

## Quickstart

Validate and compile:

```sh
unmute validate examples/salon-concierge
unmute compile examples/salon-concierge
```

The generated projects land in `build/livekit/` and `build/pipecat/`. Each one
carries its own `README.md`, which is the deployment and carrier runbook. Do not
commit `build/`, it is disposable.

Talk to it in the browser:

```sh
cp examples/salon-concierge/build/pipecat/.env.example examples/salon-concierge/.env
unmute dev examples/salon-concierge --target pipecat
```

Use `--target livekit` for the same conversation on the other target. Use
headphones, or the agent hears its own voice and interrupts itself.

## What you need

Keep every value in `.env`. No credential and no real phone number belongs in
the package.

| Name | Purpose |
|---|---|
| `GOOGLE_API_KEY` | Gemini 3.5 Flash-Lite through Google's EU Vertex endpoint; needs `aiplatform.endpoints.predict` access |
| `OPENAI_API_KEY` | the knowledge embeddings at startup |
| `SLNG_API_KEY` | the voice and the transcription. One key for both |
| `LANGFUSE_SECRET_KEY`, `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_BASE_URL` | trace ingest. All three together, or startup fails |
| `MANAGER_PHONE_NUMBER` | the transfer destination, in E.164. Needed only for a phone call |

Both targets use their native Google plugin with Gemini 3.5 Flash-Lite and
minimal thinking. The Vertex client is pinned to
`https://aiplatform.eu.rep.googleapis.com`; no model request falls back globally.
This selects the reasoning endpoint only. The knowledge embeddings still use
OpenAI, and speech uses the SLNG gateways declared below.

A real inbound call also needs its carrier credentials. The `livekit` target
needs `SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD` and
`SIP_FROM_NUMBER`. The `pipecat` target needs `TWILIO_ACCOUNT_SID`,
`TWILIO_AUTH_TOKEN` and `TWILIO_PHONE_NUMBER`. Neither is read by a browser
session.

## Structure

| Path | What it holds |
|---|---|
| `agent.yaml` | the package: agents and the tasks they run, handoffs, escalations, variables, pre-fetch, knowledge and secrets |
| `targets.yaml` | the two targets, one per telephony plane |
| `instructions.md` | the concierge prompt |
| `agents/complaint-specialist.md` | the customer care prompt |
| `tasks/` | the verification, booking, confirmation contact and complaint task prompts |
| `tools/` | one file per tool: local Python over one in-memory store, plus the `end_call` builtin |
| `knowledge/refunds/`, `knowledge/services/` | two document sets, each its own index |
| `connections/` | the two carrier connections |

## The two agents and their tasks

**Two agents.** The concierge is the one the caller talks to for almost the whole
call. Customer care is a second agent because it holds a document set and a
permission the concierge must not have: the refund policy and the complaint
record.

**Four tasks, two of them steps of the booking group.**
Verification confirms who is calling.
Booking does create, modify and cancel in one task and saves a typed Appointment.
Taking the confirmation contact is its own step, so a caller who does not want an
email never has to give one and a booking is never held up by a missing address.
Customer care records complaints in its own task and appends typed Complaint
values. It does not verify anyone: see "One agent verifies" below.

**One agent verifies.** `verify_customer` is on the concierge and nowhere else.
A task within reach beats a prompt rule, so the task is not listed on the
specialist, however plainly its prompt says not to verify again. Every tool
that needs the number refuses while it is unconfirmed, so the gate is still
there, and `to_concierge` is the way back to the agent that verifies.

**One agent asks for agreement.** The specialist says the complaint back and
asks once; `handle_complaint` records what was agreed and asks nothing. Both
asking cost the caller a whole turn to learn nothing.

## The booking flow

**One booking flow, two steps, no request between them.** `book` is a task group:
verification, then booking. The concierge calls it once and does not choose
between the two steps, and the group does not ask the concierge which comes
next. Verification carries `skip_when_confirmed: customer_phone`, so a second
booking on the same call goes straight to the booking step; a caller who
corrects their number gets verification again, because entering that step
withdraws the confirmation it made.

**Three steps end on their own tools.** `verify_customer` names its lookup
under `finish:`, `manage_booking` names its three mutations, and
`handle_complaint` names `record_complaint`. When one of those returns a result
the package calls a success, the step saves its `assign:` from that result and
hands over, with no model request in between and without the result reaching
the model. A result that is not a success, a `not_confirmed` or a
`slot_unavailable`, goes back to the model and the step stays open.

Together, the group and `finish:` take the model out of the seams. The group
means the concierge makes one call and never chooses between the two steps, and
`finish:` removes the model request between a tool succeeding and the hand-over
that follows it, on both steps. The one request left on the turn that books is
the concierge saying it is done. Recording a complaint loses its `finish` call
the same way.

Each of those tools returns the record it saved, whole, on success:
`create_booking` returns the `appointment` and `record_complaint` returns the
`complaint`. That is what makes the step's `assign:` possible without the
model, and it is the reason the model can never retype an id it was handed.

**Two rules live in the booking backend, not the prompt.** `create_booking`
refuses with `has_booking` while the caller already holds one, unless the model
passes `additional` because the caller asked for another appointment. A change
to a booking is `modify_booking`, and a prompt rule alone does not stop a model
from answering "move it" with a second booking. And a slot earlier than the
salon's own clock today is not offered and not accepted.

## Typed values and context

**Spoken messages across every task and handoff.** Each context block declares
`history: messages`, also the framework default. The receiver gets the caller
and assistant speech available at entry, without tool calls and results.
Returning from a task restores the owner's earlier conversation and gives only
a completed or unserved status. It does not copy the task's conversation back.

**Typed values shared on purpose.** Verification saves `customer_status` so both
agents know it already happened. Booking saves `appointment` only after a create,
move or cancellation succeeds. The owner and customer care read those values
through explicit prompt references, including `{{appointment}}`, so a later
complaint can refer to the updated date without asking again. Tools inject the
confirmed phone number. No value is automatically added to a prompt.

**An email address checked where it enters.** `confirmation_contact` is a
`NameEmail`, which holds the name and the address as two fields, so the agent can
say whose name the booking is under without reading an address out loud.
`confirmation_email` is an `EmailStr` taken off that pair with a dotted
assignment, so the caller spells the address out once and both values are filled
from the one answer. The address is checked by `email-validator`, which the two
emitted projects declare because this package uses the types, and the check never
asks DNS: it runs while the caller is on the line.

## Before the greeting

**Facts resolved before the greeting.** The `prefetch:` block reads the date,
the weekday and the salon's local time off one clock reading, and the caller's
number off the call, then looks up the caller's name and whether they are on
file, both from that one lookup. Nothing in the block can fail a call: an
entry whose inputs are empty is skipped and the values keep their defaults.

## The manager transfer and the knowledge bases

**A cold manager transfer.** Both agents hold it. Asking for a person is never
gated on identifying yourself first.

**Two knowledge bases.** An agent reaches one by holding its tool:
`look_up_salon_info` for services, `look_up_refund_policy` for refunds. That is
the whole access model. Both document sets are fictional and both are committed.

## Routes, tracing and the speech gateway

**Two routes, one per telephony plane.** The LiveKit target carries inbound calls
and the transfer over a Twilio Elastic SIP Trunk (`sip`). The Pipecat target
carries them over Pipecat Cloud's Twilio websocket (`cloud-websocket`). Both
targets also do browser audio. There is no outbound route.

Phone calls need a deployment. Both targets deploy to a managed platform, and
the generated runbook has the carrier steps for the route you chose. There is no
local phone loop.

**Tracing.** Both targets send traces to Langfuse.

**Speech gateway.** Both targets send STT and TTS through `eu-north.api.slng.ai`.
Change `params.world_part` on each speech model to choose another
[SLNG gateway](../../docs-site/optimization/regional-infrastructure.mdx).

## Advanced

<details>
<summary>What the dev page shows</summary>

The dev page streams caller and generated agent text, running tools and available
measurements. Final caller words do not wait for a model reply. Each numbered
SDK model call shows its own first-response and full-duration values at a glance.
TTS first audio and tool duration also stay visible; Debug details holds
secondary timings and source metadata. A call into a task, such as `verify_customer`,
gets a `HANDOFF` row and no duration, which is what accounts for the extra model
call in that reply. Reply latency excludes browser delivery; source-limited measurements
remain unassigned, and a lost event range labels the call count as observed.
Missing values have no placeholder; measured zero stays visible.
Generated text can be ahead of audio. Disconnect keeps unfinished text visible;
Latest returns to the live end after scrollback. The local
`build/<target>/dev.log` includes transcript snapshots as well as raw timing data.

</details>

<details>
<summary>Run the tools without the agent</summary>

Run the tools' own check on its own:

```sh
python3 examples/salon-concierge/tools/salon.py
```

</details>

<details>
<summary>Read a call back afterwards</summary>

Read a call back after somebody has talked to the agent:

```sh
python3 scripts/read_langfuse_trace.py --env examples/salon-concierge/.env
```

</details>

## Troubleshooting

### It stops at startup and nothing speaks

A value the agent reads at startup is missing from `.env`. `GOOGLE_API_KEY` is
read for Gemini, `OPENAI_API_KEY` embeds the knowledge documents, and the three
Langfuse values have to be there together.

**Fix:** take the names from the generated example file, fill them in, and run
again.

```sh
cp examples/salon-concierge/build/pipecat/.env.example examples/salon-concierge/.env
unmute dev examples/salon-concierge --target pipecat
```

### The agent talks over itself in the browser

It is hearing its own voice through the speakers and treating it as the caller
interrupting.

**Fix:** use headphones.

### The agent does not know the caller's number

A browser session has no carrier, so nothing supplies a caller number. The
`caller` pre-fetch entry is skipped and `customer_phone` keeps its default.

**Fix:** Seed one to exercise the pre-fetch and the readback:

```sh
unmute dev examples/salon-concierge --source from_number=<E.164 number>
```

### The agent will not book a second appointment

That rule is in the booking backend, not the prompt. `create_booking` refuses
while the caller already holds a booking, unless the caller has asked for another
appointment as well as the one they have.

**Fix:** say that the second appointment is an extra one, so the model sends
`additional`, or move the booking instead of making a new one. The backend's own
check shows both rules on their own:

```sh
python3 examples/salon-concierge/tools/salon.py
```

### Asking for a manager goes nowhere

The escalation dials `MANAGER_PHONE_NUMBER`, and there is nothing to dial
without it.

**Fix:** set `MANAGER_PHONE_NUMBER` in `.env`, in E.164. It is read on a phone
call only, so a browser session cannot complete the transfer.

### The phone number rings and no agent answers

The route's carrier credentials are missing from `.env`, or the package has not
been deployed yet.

**Fix:** set the four `SIP_*` values for the `livekit` target, or the three
`TWILIO_*` values for the `pipecat` target, then follow the carrier steps in
that target's generated runbook:

```sh
unmute compile examples/salon-concierge
cat examples/salon-concierge/build/pipecat/README.md
```

## Where to go next

- For a longer scripted conversation, see the [end-to-end harness](../../docs/HARNESS_TEST.md).
- For the same salon with the structural features taken back out, see [`salon-concierge-single-prompt`](../salon-concierge-single-prompt/).
- To check runtime behaviour without a person on the phone, see [self verification](../../docs/SELF_VERIFY.md).
- For the other shipped packages, see the [examples index](../README.md).
