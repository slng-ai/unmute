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
| `tasks/` | the verification, booking, confirmation contact and complaint task prompts |
| `tools/` | one file per tool: local Python over one in-memory store, plus the `end_call` builtin |
| `knowledge/refunds/`, `knowledge/services/` | two document sets, each its own index |
| `connections/` | the two carrier connections |

**Two agents.** The concierge is the one the caller talks to for almost the whole
call. Customer care is a second agent because it holds a document set and a
permission the concierge must not have: the refund policy and the complaint
record.

**Four tasks, one of them shared.** Verification confirms who is calling.
Booking does create, modify and cancel in one task and saves a typed Appointment.
Taking the confirmation contact is its own step, so a caller who does not want an
email never has to give one and a booking is never held up by a missing address.
Customer care records complaints in its own task and appends typed Complaint
values. Customer care offers verification too, and
it does that with a bare name in its own `tasks:` list rather than a second copy:

```yaml
  complaint_specialist:
    tasks:
      - verify_customer
```

so there is one definition, one prompt, and one name in the emitted project.

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

**Ordering carried by the prompt.** `manage_booking` runs after verification,
but not because the compiler holds it back: the concierge's own instructions
say to run verification first and never start booking until it has succeeded.

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

**Speech gateway.** Both targets send STT and TTS through `eu-north.api.slng.ai`.
Change `params.world_part_override` on each speech model to choose another
[SLNG gateway](../../docs-site/optimization/regional-infrastructure.mdx).

## What you need

Keep every value in `.env`. No credential and no real phone number belongs in
the package.

| Name | Purpose |
|---|---|
| `OPENAI_API_KEY` | the OpenAI reasoning model and the knowledge embeddings at startup |
| `SLNG_API_KEY` | the voice and the transcription. One key for both |
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
