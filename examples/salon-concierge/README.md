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
- [The booking flow](#the-booking-flow) - two tasks, run in order
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
| `OPENAI_API_KEY` | the knowledge embeddings at startup |
| `SLNG_API_KEY` | the voice and the transcription. One key for both |
| `LANGFUSE_SECRET_KEY`, `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_BASE_URL` | trace ingest. All three together, or startup fails |
| `MANAGER_PHONE_NUMBER` | the transfer destination, in E.164. Needed only for a phone call |

Both targets reason on `gpt-5.6-luna` with `reasoning_effort: none`, which is
the model this package was qualified on: 12 out of 12 on multi-turn tool
routing, measured against its own prompts and tools.

Native Gemini 3.5 Flash-Lite held the slot for two days in September 2026 and
came out after one live call lost 18.5 seconds to two `MALFORMED_FUNCTION_CALL`
aborts, which is Gemini throwing away a turn whose tool call it emitted as plain
text rather than as a structured part. Nothing in this package triggers it and
Google has no fix, so the model left rather than the symptom being made cheaper.
Screen a replacement on multi-turn tool routing before latency; latency and
single-turn tool calls predict neither.

The knowledge embeddings use OpenAI, and speech uses the SLNG gateways declared
below.

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
| `tasks/` | the verification and booking step prompts |
| `tools/` | one file per tool: local Python over one in-memory store, plus the `end_call` builtin |
| `knowledge/refunds/`, `knowledge/services/` | two document sets, each its own index |
| `connections/` | the two carrier connections |

## The two agents and their tasks

**Two agents.** The concierge is the one the caller talks to for almost the whole
call. Customer care is a second agent because it holds a document set and a
permission the concierge must not have: the refund policy and the complaint
record.

**Two tasks, run in that order by the concierge.** Verification confirms who is
calling. Booking does book, move and cancel in one task and saves a typed
Appointment.

A task is worth a model request when something has to happen in an order.
Verification before a write is that; filing a complaint is not, so
`record_complaint` sits on customer care as an ordinary tool. Entering a task
costs one model request whatever the task does, and that request speaks no
words, so a task wrapped around a single action is a silence the caller pays
for.

**One agent verifies.** `verify_customer` is on the concierge and nowhere else.
A task within reach beats a prompt rule, so the task is not listed on the
specialist, however plainly its prompt says not to verify again. Every tool
that needs the number refuses while it is unconfirmed, so the gate is still
there, and `to_concierge` is the way back to the agent that verifies.

**One agent asks for agreement.** The specialist says the complaint back and
asks once, then calls `record_complaint` with what was agreed. The tool speaks
its own fixed line as it starts, so the prompt adds none of its own.

## The booking flow

**Two steps, and the concierge runs them in order.** Verification, then booking.
The concierge reads `customer_verified`: empty means nobody on this call has been
identified, so it runs `verify_customer` first; a status there means that caller
is verified for the rest of the call and it goes straight to `manage_booking`. A
caller who corrects their number gets verification again, because entering that
step withdraws the confirmation it made.

**This was a task group until September 2026, and the group was the better
shape.** `book` held the same two steps and chained them itself, so the concierge
spent no model request deciding to enter the second one. It came out because it
does not run on Pipecat: on a live call the group was entered, held the call for
eleven seconds, made no model request and produced no audio at all, and the
caller heard only the greeting. The lowering compiles and validates; it never
speaks. One extra model request on the booking turn is what the example costs to
work on both targets. `skip_when_confirmed:` went with it, because that is a
group-step field, and the decision it made structurally is now the concierge's
to make from `customer_verified`.

**Both steps end on their own tools.** `verify_customer` names its lookup
under `finish:`, and `manage_booking` names `save_booking` once, with its three
successful statuses listed under the one `status:` field. Written as three
entries naming the same tool it used to compile to the last one alone, so an
ordinary booking left the step open; that shape is refused now. When one of
those returns a result
the package calls a success, the step saves its `assign:` from that result and
hands over, with no model request in between and without the result reaching
the model. A result that is not a success, a `not_confirmed` or a
`slot_taken`, goes back to the model and the step stays open.

`finish:` is what takes the model out of the seams now: it removes the model
request between a tool succeeding and the hand-over that follows it, on both
steps. The one request left on the turn that books is the concierge saying it is
done.

`save_booking` returns the record it saved, whole, on success: the
`appointment`, complete. That is what makes the step's `assign:` possible
without the model, and it is the reason the model can never retype an id it was
handed.

**One read tool and one write tool.** `find_slots` answers what the caller
already holds and what is free in one call, because a caller moving an
appointment needs their booking id and the new day's free times in the same
breath. Asking for those separately cost two model round trips for one request.
`save_booking` is the only tool that changes anything, so the caller's yes is
checked in one place.

**One line per step, and neither is on a tool.** A booking has two silences in
it and they need different lines, because different things are happening behind
them.

The front one is `verify_customer`'s. It fires as the concierge calls the step,
covering the two model requests it takes to ask the caller for their number.

The second is `manage_booking`'s, covering the three requests between the caller
confirming their number and hearing what is free: the lookup, the diary read,
then the turn that says what is open. On a booking after the first this is the
only line the caller hears, because verification is skipped.

**Both sit on the step, not on the tool it calls, and that is the useful part.**
A tool's `announce:` is emitted inside the tool body, so it fires once per call
rather than once per request. The diary line lived on `find_slots` until
September 2026, when a live call had the model read the diary twice in one turn
and the caller heard two different announcements 0.8 seconds apart. No prompt
rule stops a model chaining a tool, and this package had one that said not to.

A step's line fires once in the entry, before the model has decided anything, so
the repetition is not a rule to follow but a shape that cannot break. It is also
earlier: measured on that same call, the step entry ran 0.64 seconds ahead of
the tool.

**Both lines are written as alternatives.** `announce:` takes a list, and the
caller hears one of its entries each time the line fires, never the one that site
used last. One fixed sentence is the same sentence every time, and a caller who
books and then moves the booking hears it twice inside a minute.

Three other places were tried and each was a different mistake. The `book` group
carried one line for both steps, and no sentence was true on both halves: "let
me pull that up" was followed by a question about the caller's phone number, and
"let me get you verified" would play again on a move where nobody is verified.
`find_or_create_customer` lost its line to the step above it, which is earlier
and honest on both routes in. And `save_booking`'s line was a promise the tool
could break, because an `announce:` is spoken when a tool is called and this one
refuses a save that arrives unconfirmed. The caller heard "putting that through
now" and then a question asking their permission.

**A spoken line is not a caller turn.** A step that opens with "Got it," after
its own announcement is agreeing with itself, which a live call on Pipecat did,
because a `TTSSpeakFrame` never reaches the model's context there. Both step
prompts say in one sentence that a fixed line was already spoken and to go
straight to the question, so the two targets behave the same.

The hesitation a person actually makes before answering rides on the answer
instead. The booking prompt opens its first sentence with a written "hmm" or
"okay", which the voice reads as thinking, and which costs no speech of its own
and no extra request. When the caller has just confirmed their number it opens
by closing that off instead, "Perfect, got you.", and writes no hesitation as
well. That opener is not "you're all set", because the concierge already owns
that line for the moment a booking lands, and a live call played both of them
thirteen seconds apart.

**The offer turn asks a question, and which question depends on the count.**
Three times read out with nothing asked is a wasted round trip: a live call
answered "Gotcha." to a bare list, and the next turn had to guess which slot the
caller meant. Several free times end the turn with "which works best for you?".
Exactly one free time ends it with "would that work for you?", because a call
offered a single slot and asked which one suited them, and the caller answered
"Um, well. It's the only one that you have." That second question is already the
confirming question, so a yes to it saves.

**Three rules live in the booking backend, not the prompt.** `save_booking`
refuses with `has_booking` while the caller already holds one, unless the model
passes `additional` because the caller asked for another appointment. A change
to a booking is `action: move`, and a prompt rule alone does not stop a model
from answering "move it" with a second booking. And a slot earlier than the
salon's own clock today is not offered and not accepted.

The third is `find_slots`, and it is a rule about what the tool does **not**
offer. The booking step opens with a generated turn, and a step whose whole job
is the diary pulls the model straight at the diary tool. While an empty `date`
was a documented way to call it, the model took that route on entry, every turn,
before the caller had named a day. A live call on 2026-09-16 did it twice, read
the two empty lists and the `ok` beside them as a finished search, invented an
appointment the diary had just denied, and asked for the service three times.
The step prompt already said to ask for a day first, which is the repo's own
lesson that a tool in reach beats a prompt rule.

So the empty date is gone from the tool's description: every call names a day.
Nothing is lost, because `bookings` comes back whatever day you ask for, so a
caller changing an appointment is found by asking for today. `need_date` stays
as the backstop, returned when a dateless call arrives from a caller holding
nothing, and it says what to do rather than reporting success. A text run before
the description changed called the tool with an empty date on three consecutive
turns, each one speaking an `announce:` line; after it, every call carried a day
and the turn with nothing new to look up made no call at all.

## Typed values and context

**Spoken messages across every task and handoff.** Each context block declares
`history: messages`, also the framework default. The receiver gets the caller
and assistant speech available at entry, without tool calls and results.
Returning from a task restores the owner's earlier conversation and gives only
a completed or unserved status. It does not copy the task's conversation back.

**Typed values shared on purpose.** Booking saves `appointment` only after a
book, move or cancellation succeeds. The owner and customer care read it through
explicit prompt references, so a later complaint can refer to the updated date
without asking again. Tools inject the confirmed phone number. No value is
automatically added to a prompt.

That record carries a `spoken` field, which is the confirmation sentence itself:
"Friday at 9:00 AM". It is there because every part of that sentence the agent
composed itself, it got wrong on a live call. A weekday worked out from a date
turned a Friday booking into "Thursday the 18th". A 24 hour time copied out of
the record was read back as "09:00 AM". A move saved at 09:00 was confirmed as
"3:00 PM", which was what the caller had meant two turns earlier by "the same
time". A value the backend already knows is cheaper to write down than to ask
the model to derive, and it is right every time.

**Five variables, and each one is read somewhere.** A saved value costs a task
to write and a line in every prompt that names it, so a value nothing reads is
pure cost. This package used to carry a `customer_status` that only ever said
what `confirm:` already enforced, and a name and an on-file flag that no prompt
mentioned at all. They are gone. See
[choosing fewer variables](https://unmute.ai/build/variables#keep-only-the-values-the-call-needs).

## Before the greeting

**Facts resolved before the greeting.** The `prefetch:` block reads the date,
the weekday and the salon's local time off one clock reading, and the caller's
number off the call. Nothing in the block can fail a call: an entry whose
inputs are empty is skipped and the values keep their defaults.

No entry runs a tool, and that is deliberate. A pre-fetch runs unasked on every
inbound call, so an entry nothing reads costs the caller time on every wrong
number that ever rings. `salon-concierge-v3` keeps a tool-bearing entry if you
want to see one.

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
call in that reply. On Pipecat each measured reply is also split into the parts
that make it up, with what each cost and who owns it, and a tool that produced no
result says whether it failed or ran past its deadline. Reply latency excludes
browser delivery; source-limited measurements
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

A value the agent reads at startup is missing from `.env`. `OPENAI_API_KEY`
reasons and embeds the knowledge documents, `SLNG_API_KEY` covers both speech
legs, and the three Langfuse values have to be there together.

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

That rule is in the booking backend, not the prompt. `save_booking` refuses
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
