# salon-concierge

The full Sage and Stone Salon project, and the package to read when you want to
see every Unmute path working together in one agent.

The salon takes calls. The agent works out who is calling, books, moves and
cancels appointments, answers questions from the salon's own documents, writes
down complaints, and puts a caller through to a manager when they ask for one.

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

**Two agents.** The concierge is the one the caller talks to for almost the whole
call. Customer care is a second agent because it holds a document set and a
permission the concierge must not have: the refund policy and the complaint
record.

**Two tasks, one of them shared.** Verification confirms who is calling. Booking
does create, modify and cancel in one step. Both are written inside the concierge,
which is the agent that defines them. Customer care offers verification too, and
it does that with a bare name in its own `tasks:` list rather than a second copy:

```yaml
  complaint_specialist:
    tasks:
      - verify_customer
```

so there is one definition, one prompt, and one name in the emitted project.

**A guarded step.** `manage_booking` declares `requires: [customer_phone]`, so
booking cannot start before the caller is identified. The compiler refuses the
step to the model rather than to the caller, so nobody hears the guard.

**Facts resolved before the greeting.** The `prefetch:` block reads the date,
the weekday and the salon's local time off one clock reading, and the caller's
number off the call, then looks up the caller's name and whether they are on
file, both from that one lookup. Nothing in the block can fail a call: an
entry whose inputs are empty is skipped and the values keep their defaults.

**A cold manager transfer.** Both agents hold it. Asking for a person is never
gated on identifying yourself first.

**Two knowledge bases.** An agent reaches one by holding its tool:
`look_up_salon_info` for services, `look_up_refund_policy` for refunds. That is
the whole access model. Both document sets are fictional and both are committed.

**Two routes, one per telephony plane.** The LiveKit target carries inbound calls
and the transfer over a Twilio Elastic SIP Trunk (`sip`). The Pipecat target
carries them over Pipecat Cloud's Twilio websocket (`cloud-websocket`). Both
targets also do browser audio. There is no outbound route.

**Tracing.** Both targets send traces to Langfuse.

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

A browser session has no carrier, so nothing supplies a caller number. Seed one
to exercise the pre-fetch and the readback:

```sh
unmute dev examples/salon-concierge --source from_number=<E.164 number>
```

Run the tools' own check on its own:

```sh
python3 examples/salon-concierge/tools/salon.py
```

Read a call back after somebody has talked to the agent:

```sh
python3 scripts/read_langfuse_trace.py --env examples/salon-concierge/.env
```

Phone calls need a deployment. Both targets deploy to a managed platform, and
the generated runbook has the carrier steps for the route you chose. There is no
local phone loop.

For a longer scripted conversation, see the
[end-to-end harness](../../docs/HARNESS_TEST.md). For the same salon with the
structural features taken back out, see
[`salon-concierge-single-prompt`](../salon-concierge-single-prompt/).
