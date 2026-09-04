# salon-concierge-v3

A test package, not a shipped example. It lives here because we compile it,
deploy it and talk to it; nobody is pointed at it as a starting shape. The
public write-up of what it does is
[Scoping a step's context](../../../docs-site/optimization/context-scope.mdx).

## What it is

The typed-inputs verification package. It is
[`salon-concierge-v2`](../salon-concierge-v2/) with one thing added and one
thing taken away. Added: every task and every handoff declares `input:`, the
values it needs in order to start, in the same type words a result field uses.
The agent that heard the caller fills them when it runs the step or hands the
caller over, and the receiving prompt ends with a `Request:` block the compiler
wrote. Taken away: the conversation. Every step and every handoff runs on
`history: reset`, so nothing here reads the transcript, and a step that is
entered late in a long call costs what it cost at the start.

v2 stays frozen at the state that closed typed session state, so a regression
here can be measured against a package known to work. The local spec is
`specs/004-typed-inputs/`.

Both are the same Sage and Stone salon as
[`salon-concierge`](../../../examples/salon-concierge/), with one thing changed:
every step gets the smallest context that still does its job, and what it no
longer reads off the transcript travels as declared state and as a typed
request instead.

Declared state is typed. The facts a call accumulates are groups of named
fields, written in Pydantic's own words, and the compiler puts a block naming
them at the end of every agent prompt and every task prompt. So a step reads
what the call has established rather than re-reading the call, and a later
step's prompt stops growing with the length of the conversation.

Same tools, same knowledge bases, same voice, same models, same targets, same
carriers and the same agents. The `shapes:` block, the `variables:` block, the
`context:` blocks and each step's `result:` are what differ, plus one step the
other salon does not have.

## What it contains

Robin works the front desk, confirms who is calling, runs the booking step and
answers what it can. Customer care takes complaints and refunds. A control
cold-transfers to a manager on a phone call.

Two tasks sit under the concierge and one under customer care, and all three
run with `history: reset`: each gets its own prompt, the declared values, and
what it was handed, and no part of the conversation. `verify_customer` reads a
phone number back and waits for a yes, and declares no `input:`, because
reading a number back needs nothing from the request. `manage_booking` takes
one booking change from start to finish and is handed the request: `action`
(create, modify or cancel) is required, because the step cannot start without
knowing what to do, and `service`, `requested_day` and `requested_time` are
optional, in the caller's own words, because a caller cancelling names no day
and a caller who says "a haircut" has named no time. Marking them optional is
what stops the model inventing a value it was told it must fill.
`handle_complaint` records one thing the caller is unhappy about and is handed
`problem` (required, the caller's words with what the specialist offered) and
`about` (optional, the appointment it concerns, copied from the ones already
recorded so nothing is invented).

Both handoffs run on `history: reset` too and carry a typed brief instead of
the conversation. `to_complaints` hands customer care the same `problem` and
`about`; `to_concierge` hands the front desk `outcome` (what was done) and
`next_request` (what the caller now wants), both required because a handoff
back is always for something. Neither writes `variables:`, because leaving it
out means every declared value travels.

The block each seam receives is in the emitted module, after the conversation
info, and it is the same text on both targets:

```sh
grep -A6 'Request:' internal/voice-agents-tests/salon-concierge-v3/build/livekit/agent.py
```

The concierge's own block lists `outcome` and `next_request` only. No prompt
reads another seam's inputs, and the compiler refuses one that tries.

Three shapes hold what the call learns. `Customer` is who the caller is once the
verification step has looked them up, and it is two fields rather than four: the
phone number is the identity here, so the record carries no id and no name. Both
were tried and both failed the same way, because neither has a tool that fills
it. The id left the model inventing one or sending empty, and the name is absent
for every caller not already on file, so it rendered as an empty string in every
prompt for the rest of the call. `Appointment` is one thing booked, moved
or cancelled. `Complaint` is one thing the caller is unhappy about, and it can
name the appointment it is about, which is a shape inside a shape.

`record_complaint` sits on the complaint step and deliberately not on the
specialist that owns it. With the tool on both, the specialist recorded the
complaint itself and never entered the step, so `complaints` stayed empty on a
call that recorded one. Its prompt already said to run the step. A tool within
reach beat the prompt, and `unmute validate` now warns on the shape.

Five values are declared with those shapes. `customer` holds the record and may
be absent. `appointments` and `complaints` are lists, and the steps that fill
them append rather than replace, so a caller who books twice ends the call with
two bookings instead of one.

One entry per step visit, because the step hands back one appointment. The
booking step finishes as soon as a change is saved and puts anything else the
caller asked for in `unserved_request`, and the concierge sends it straight
back in. A step that stayed in and booked twice would report one of them and
lose the other. And a step that is re-entered and finishes at once re-reports
what it already recorded, which is why the emitted append drops a structured
entry the list already holds rather than trusting the prompt not to send one.

There is no list of reasons the caller rang and no per-step `reason` result.
v2 had both, and they were the cost of `reset` before inputs existed: a step
that saw the conversation recorded why it ran, and a step that did not could
only ask. Here each appointment records its `action` and each complaint its
`reason`, so what the call did is already on the state, and the request each
step was handed says why it was entered. `customer_phone` is text with a
validated shape, and it carries `confirm:`, so it renders in the verification
step's own prompt and nowhere else until the caller has agreed to it.

Neither step announces itself. A task `announce:` speaks one line while the
step starts, and with the step's own opening line as well the caller heard two
acknowledgements before any information: "Let me pull up the diary." then "Let
me check." One of them had to go, and the step's own line is the one that knows
what it is about to do.

`customer` declares no `default:` on purpose, and the booking step names
`customer.status` in `requires:`. A default is a value the variable holds before
the first word, so a defaulted record would let the booking step start on a
caller nobody had looked up. With no default the guard waits.

The files:

- `agent.yaml` is the package: shapes, agents and the tasks they run, handoffs,
  escalations, variables, pre-fetch, knowledge and secrets.
- `targets.yaml` holds the targets, one per telephony plane.
- `instructions.md` is the concierge prompt, `agents/complaint-specialist.md`
  the customer care prompt, and `tasks/` the three task prompts. None of them
  contains the conversation info block or the request block: the compiler
  appends both, so adding a field to a shape or an input to a step reaches the
  prompt with no prompt file edited.
- `tools/` is one file per tool, all local Python over one in-memory store.
- `knowledge/refunds/` and `knowledge/services/` are two document sets, each
  with its own index.
- `connections/` holds the carrier connections.

The LiveKit target carries inbound calls and the transfer over a Twilio Elastic
SIP Trunk (`sip`). The Pipecat target carries them over Pipecat Cloud's Twilio
websocket (`cloud-websocket`). Both targets also do browser audio. There is no
outbound route.

The package is named `salon-concierge-v3`, so its deployments are
`salon-concierge-v3-livekit` and `salon-concierge-v3-pipecat` and neither lands
on top of the other salon.

Its think model points straight at OpenAI rather than through the Context
Router. That is temporary: calls were taking too many turns to reach a tool
call, and taking the router out of the path is how we find out whether its
caching is why. On the router this package needs its own `agent_id`, because
the router keys what it can serve from an earlier request on that id and the
key carries neither the system prompt nor the values substituted into it, so
two packages sharing an id are served each other's prompt.

## What you need

Keep every value in `.env`. No credential and no real phone number belongs in
the package.

`OPENAI_API_KEY` is the reasoning model's upstream and the knowledge embeddings
at startup. `SLNG_API_KEY` is the Context Router, the voice and the
transcription, one key for all three. `LANGFUSE_SECRET_KEY`,
`LANGFUSE_PUBLIC_KEY` and `LANGFUSE_BASE_URL` are trace ingest, and all of them
are needed together or startup fails. `MANAGER_PHONE_NUMBER` is the transfer
destination in E.164, and only a phone call needs it.

A real inbound call also needs its carrier credentials. The `livekit` target
needs `SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD` and
`SIP_FROM_NUMBER`. The `pipecat` target needs `TWILIO_ACCOUNT_SID`,
`TWILIO_AUTH_TOKEN` and `TWILIO_PHONE_NUMBER`. A browser session reads neither.

## How to run it

```sh
unmute validate internal/voice-agents-tests/salon-concierge-v3
unmute compile internal/voice-agents-tests/salon-concierge-v3
```

The generated projects land in `build/livekit/` and `build/pipecat/`. Each one
carries its own `README.md`, which is the deployment and carrier runbook. Do not
commit `build/`, it is disposable.

Talk to it in the browser:

```sh
cp internal/voice-agents-tests/salon-concierge-v3/build/pipecat/.env.example internal/voice-agents-tests/salon-concierge-v3/.env
unmute dev internal/voice-agents-tests/salon-concierge-v3 --target pipecat
```

Use `--target livekit` for the same conversation on the other target. Use
headphones, or the agent hears its own voice and interrupts itself.

A browser session has no carrier, so nothing supplies a caller number. Seed one
to exercise the pre-fetch and the readback:

```sh
unmute dev internal/voice-agents-tests/salon-concierge-v3 --source from_number=<E.164 number>
```

Do not seed `customer`. It declares no `source:`, so the dispatch payload can
fill it and the generated runbook lists it with a `--var` line. That line works,
and using it hands the booking step a record before anything looked the caller
up, which is the one thing this package is arranged to prevent.

To see the declared state as the model sees it, read the block the compiler put
at the end of each prompt:

```sh
grep -A7 'Conversation info:' internal/voice-agents-tests/salon-concierge-v3/build/livekit/agent.py
```

It is the same text on both targets, and the generated classes beside it are
what the steps validate their results against.

Run the tools' own check on its own:

```sh
python3 internal/voice-agents-tests/salon-concierge-v3/tools/salon.py
```

Read a call back afterwards with
[`scripts/read_langfuse_trace.py`](../../../scripts/read_langfuse_trace.py),
which needs the Langfuse values above.

For a longer scripted conversation, see the
[end-to-end harness](../../../docs/HARNESS_TEST.md). The values
`context.history` takes and what each one sends the model are in
[the tasks page](../../../docs-site/build/orchestration/tasks.mdx).
