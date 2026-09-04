# salon-concierge-v2

A test package, not a shipped example. It lives here because we compile it,
deploy it and talk to it; nobody is pointed at it as a starting shape. The
public write-up of what it does is
[Scoping a step's context](../../../docs-site/optimization/context-scope.mdx).

## What it is

The same Sage and Stone salon as
[`salon-concierge`](../../../examples/salon-concierge/), with one thing changed:
every step gets the smallest context that still does its job, and what it no
longer reads off the transcript travels as a declared variable instead.

Same tools, same knowledge bases, same voice, same models, same targets, same
carriers, same agents and the same tasks. Only the `context:` blocks and the
`variables:` block differ.

## What it contains

Robin works the front desk, confirms who is calling, runs the booking step and
answers what it can. Customer care takes complaints and refunds. A control
cold-transfers to a manager on a phone call.

Two tasks sit under the concierge. `verify_customer` reads a phone number back
and waits for a yes, and it runs with `history: reset`, so it gets its own
prompt and its declared values and no part of the conversation. `manage_booking`
takes one booking change from start to finish, and it runs with
`history: messages`, so it gets both sides of the conversation without the tool
records nobody re-reads. Both handoffs carry the spoken turns for the same
reason.

Two values travel as variables rather than as history. `customer_phone` is the
confirmed number. `customer_status` says what the lookup found, and the booking
step names it in `requires:`, which is what makes the step wait for it and what
lets its prompt read it. `customer_status` declares no `default:` on purpose: a
default is a value the variable holds before the first word, so a defaulted
variable would let the booking step start on a caller nobody had looked up.

The files:

- `agent.yaml` is the package: agents and the tasks they run, handoffs,
  escalations, variables, pre-fetch, knowledge and secrets.
- `targets.yaml` holds the targets, one per telephony plane.
- `instructions.md` is the concierge prompt, `agents/complaint-specialist.md`
  the customer care prompt, and `tasks/` the two task prompts.
- `tools/` is one file per tool, all local Python over one in-memory store.
- `knowledge/refunds/` and `knowledge/services/` are two document sets, each
  with its own index.
- `connections/` holds the carrier connections.

The LiveKit target carries inbound calls and the transfer over a Twilio Elastic
SIP Trunk (`sip`). The Pipecat target carries them over Pipecat Cloud's Twilio
websocket (`cloud-websocket`). Both targets also do browser audio. There is no
outbound route.

The package is named `salon-concierge-v2`, so its deployments are
`salon-concierge-v2-livekit` and `salon-concierge-v2-pipecat` and neither lands
on top of the other salon. It declares its own `agent_id` for the same reason:
the Context Router keys what it can serve from an earlier request on that id,
and the key carries neither the system prompt nor the values substituted into
it, so two packages sharing an id would be served each other's prompt.

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

Do not seed `customer_status`. It declares no `source:`, so the dispatch payload
can fill it and the generated runbook lists it with a `--var` line. That line
works, and using it hands the booking step a status before anything looked the
caller up, which is the one thing this package is arranged to prevent.

Run the tools' own check on its own:

```sh
python3 internal/voice-agents-tests/salon-concierge-v2/tools/salon.py
```

Read a call back afterwards with
[`scripts/read_langfuse_trace.py`](../../../scripts/read_langfuse_trace.py),
which needs the Langfuse values above.

For a longer scripted conversation, see the
[end-to-end harness](../../../docs/HARNESS_TEST.md). The values
`context.history` takes and what each one sends the model are in
[the tasks page](../../../docs-site/build/orchestration/tasks.mdx).
