# salon-concierge-v2

A test package, not a shipped example. It lives here because we compile it,
deploy it and talk to it; nobody is pointed at it as a starting shape. The
public write-up of what it does is
[Scoping a step's context](../../../docs-site/optimization/context-scope.mdx).

The same Sage and Stone salon as [`salon-concierge`](../../../examples/salon-concierge/), with
one thing changed: every step now gets the smallest context that still does its
job, and what it no longer reads off the transcript travels as a declared
variable instead.

Same salon, same tools, same knowledge bases, same voice, same models, same two
targets, same two carriers, same two agents and the same two tasks. Only the
`context:` blocks and the `variables:` block differ. That is what makes the two
packages worth reading against each other.

There are three salons now, and they are a ladder:

| Package | Every turn carries | Every step carries |
|---|---|---|
| [`salon-concierge-single-prompt`](../../../examples/salon-concierge-single-prompt/) | one prompt holding four jobs, every tool | there are no steps |
| [`salon-concierge`](../../../examples/salon-concierge/) | the concierge prompt, the tools it holds | the whole transcript, tool records included |
| `salon-concierge-v2` | the same | its instructions, its declared values, and only the turns it needs |

## What changed

| | `salon-concierge` | `salon-concierge-v2` |
|---|---|---|
| `verify_customer` context | `history: full` | `history: reset` |
| `manage_booking` context | `history: full` | `history: messages` |
| `to_complaints` context | `history: full` | `history: messages` |
| `to_concierge` context | `history: full` | `history: messages` |
| variables the model gets from a step | `customer_phone` | `customer_phone`, `customer_status` |
| `manage_booking` requires | `customer_phone` | `customer_phone`, `customer_status` |

Nothing else. No new tool, no new agent, no new task, no new key.

## What each step gets, and why

**`verify_customer` runs on nothing.** `history: reset` sends it its own prompt,
the number the pre-fetch found and the name on that record. It reads the number
back and waits for a yes, and it needs no part of the conversation to do that.
This is the step that runs most often and it now carries the least.

Two consequences, both handled in the prompts rather than left to be discovered:

- The step cannot see that it already ran. `salon-concierge` put "if the history
  already holds a successful verification, reuse it" inside the step, which only
  worked because the step could see the history. Keeping verification to once per
  call is now the concierge's job, and the concierge is the one that can actually
  do it: a finished task's call and its result stay in the owner's own context.
- The step does not receive the caller's triggering utterance. For reading a
  number back that costs nothing, which is exactly the test for choosing `reset`.

**`manage_booking` runs on what was said out loud.** `history: messages` drops
every tool record from what it inherits: the availability lists, the booking
rows, the document chunks the concierge looked a price up in. It keeps both
sides of the conversation, so the service and the day the caller mentioned are
still there. Anything it needs from a tool, it calls the tool for itself, and its
prompt says so.

**Both handoffs carry the spoken turns.** Customer care needs the complaint, and
the complaint was spoken. It does not need the diary rows the booking step
pulled, so it does not get them.

**Two values now travel as variables.** `customer_phone` did already.
`customer_status` is new: `verify_customer` assigns it from its own result, and
the booking step declares it and reads it. It buys one fewer tool call, because
a record `created` during this call has nothing booked on it, so there is nothing
to list.

## The two rules the compiler holds

**A step declares what its prompt reads.** `tasks/booking.md` reads
`{{customer_status}}`, so `manage_booking` names it in `requires:`. Take that one
line out of `agent.yaml` and the package stops compiling:

```text
task "manage_booking" instructions references {{customer_status}}, which only task "verify_customer" assigns. Add customer_status to this task's requires: list, so the step waits for the value and its prompt can read it
```

That is the whole point of trimming the history: once a step cannot see the
conversation, a value its prompt names has to come from somewhere the compiler
can check. The same list is what holds the step back until the value exists, so
the prompt never renders an empty one.

**A value with a default is not waited for.** `customer_status` declares no
`default:` on purpose. A default is a value the variable holds before the first
word, so a defaulted variable satisfies the guard on an empty string and the
booking step would start on a caller nobody had looked up.

For the same reason, do not seed it. It declares no `source:`, so the dispatch
payload can fill it and the generated runbook lists it under input variables
alongside a `--var customer_status=...` line. That line works, and using it hands
the booking step a status before anything looked the caller up, which is the one
thing this package is arranged to prevent. Seed `--source from_number=...`
instead: that is a fact a real call carries.

## What this package deliberately does not use

Three things would each cut a request further, and each is left out for a stated
reason.

- **`history: summary`** compiles on livekit only. Both targets here have to run,
  so no step uses it.
- **A `variables:` subset on a handoff** compiles on livekit only. Both handoffs
  say `variables: all`, and the narrowing happens on the task side, where
  `requires:` works on both targets.
- **`history: last_n`** on a handoff is permanent. A task's context is restored
  when the task returns, so a short window there costs nothing; a handoff
  replaces what the receiving agent has for the rest of the call, and a second
  truncation on top of the first is how an agent forgets a booking it made. Both
  handoffs keep the full spoken thread.

**None of this is a privacy control.** Shortening a step's history does not unsay
what the caller said out loud. A number read back aloud is in the transcript
whether a variable holds it or not.

## Structure

| Path | What it holds |
|---|---|
| `agent.yaml` | the package: agents and the tasks they run, handoffs, escalations, variables, pre-fetch, knowledge and secrets |
| `targets.yaml` | the two targets, one per telephony plane |
| `instructions.md` | the concierge prompt |
| `agents/complaint-specialist.md` | the customer care prompt |
| `tasks/` | the two task prompts, verification and booking |
| `tools/` | one file per tool, all local Python over one in-memory store |
| `knowledge/refunds/`, `knowledge/services/` | two document sets, each its own index |
| `connections/` | the two carrier connections |

**Two routes, one per telephony plane.** The LiveKit target carries inbound calls
and the transfer over a Twilio Elastic SIP Trunk (`sip`). The Pipecat target
carries them over Pipecat Cloud's Twilio websocket (`cloud-websocket`). Both
targets also do browser audio. There is no outbound route.

**Its own name and its own router id.** The package is `salon-concierge-v2`, so
its deployments are `salon-concierge-v2-livekit` and `salon-concierge-v2-pipecat`
and neither lands on top of the other salon. It also declares its own
`agent_id`. The Context Router decides which repeats it can serve from an earlier
one, and it keys that on `agent_id`; the key carries neither the system prompt
nor the values substituted into it, so a package with different prompts and the
same id would be served the other package's prompt.

## What you need

Keep every value in `.env`. No credential and no real phone number belongs in
the package.

| Name | Purpose |
|---|---|
| `OPENAI_API_KEY` | the reasoning model's upstream, and the knowledge embeddings at startup |
| `SLNG_API_KEY` | the Context Router, the voice, and the transcription. One key for all three |
| `LANGFUSE_SECRET_KEY`, `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_BASE_URL` | trace ingest. All three together, or startup fails |
| `MANAGER_PHONE_NUMBER` | the transfer destination, in E.164. Needed only for a phone call |

A real inbound call also needs its carrier credentials. The `livekit` target
needs `SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD` and
`SIP_FROM_NUMBER`. The `pipecat` target needs `TWILIO_ACCOUNT_SID`,
`TWILIO_AUTH_TOKEN` and `TWILIO_PHONE_NUMBER`. Neither is read by a browser
session.

## How to run it

```sh
unmute validate internal/voice-agents-tests/salon-concierge-v2
unmute compile internal/voice-agents-tests/salon-concierge-v2
```

The generated projects land in `build/livekit/` and `build/pipecat/`. Each one
carries its own `README.md`, which is the deployment and carrier runbook. Do not
commit `build/`, it is disposable.

Talk to it in the browser:

```sh
cp internal/voice-agents-tests/salon-concierge-v2/build/pipecat/.env.example internal/voice-agents-tests/salon-concierge-v2/.env
unmute dev internal/voice-agents-tests/salon-concierge-v2 --target pipecat
```

Use `--target livekit` for the same conversation on the other target. Use
headphones, or the agent hears its own voice and interrupts itself.

A browser session has no carrier, so nothing supplies a caller number. Seed one
to exercise the pre-fetch and the readback:

```sh
unmute dev internal/voice-agents-tests/salon-concierge-v2 --source from_number=<E.164 number>
```

## Seeing the difference

Run the same conversation through this package and through
[`salon-concierge`](../../../examples/salon-concierge/), then read both calls back. Both trace
to Langfuse, and the per-request messages are in the spans:

```sh
unmute dev examples/salon-concierge --target livekit --source from_number=<E.164 number>
unmute dev internal/voice-agents-tests/salon-concierge-v2 --target livekit --source from_number=<E.164 number>
python3 scripts/read_langfuse_trace.py --env internal/voice-agents-tests/salon-concierge-v2/.env
```

The emitted code says it without a call, too. On livekit, the verification step
is constructed with the parent's context in `salon-concierge`:

```python
result = await VerifyCustomer(
    chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True)
)
```

and with none of it here:

```python
result = await VerifyCustomer()  # history: reset — the task starts fresh
```

On pipecat the same step's node carries
`context_strategy=ContextStrategyConfig(strategy=ContextStrategy.RESET)`.

Run the tools' own check on its own:

```sh
python3 internal/voice-agents-tests/salon-concierge-v2/tools/salon.py
```

For a longer scripted conversation, see the
[end-to-end harness](../../../docs/HARNESS_TEST.md). The values `context.history`
takes and what each one sends the model are in
[the tasks page](../../../docs-site/build/orchestration/tasks.mdx).
