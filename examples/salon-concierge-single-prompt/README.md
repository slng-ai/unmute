# salon-concierge-single-prompt

The baseline. This is [`salon-concierge`](../salon-concierge/) with the booking, verification and complaint workflows in one prompt.

It is here to be read next to that package, not to be copied. Same salon, booking tools, knowledge bases, voice, model and two carrier routes.
The differences in context, state, routing and turn taking are listed below.

It is not a straw man. The prompt is written the way a careful team writes a
single prompt: the voice contract stated once, a clear routing section at the
top, escalation before anything else, and every rule that came out of a call
going wrong. The problem it has is not sloppiness. It is that one prompt has to
be four prompts at once, and it is all in front of the model on every turn.

On this page:

- [Quickstart](#quickstart) - validate, compile, run
- [Structure](#structure) - what the package holds
- [The differences](#the-differences) - what one prompt costs
- [Routes and speech gateway](#routes-and-speech-gateway) - the two carrier planes
- [Read them side by side](#read-them-side-by-side) - one call through each
- [Troubleshooting](#troubleshooting) - what stops a run
- [Where to go next](#where-to-go-next) - read the optimized package

## Quickstart

```sh
unmute validate examples/salon-concierge-single-prompt
unmute compile examples/salon-concierge-single-prompt
cp examples/salon-concierge-single-prompt/build/livekit/.env.example \
   examples/salon-concierge-single-prompt/.env                        # then fill it in
unmute dev examples/salon-concierge-single-prompt --target livekit
```

Both targets validate with no errors and generate a runnable project, the same
as the optimized package. A baseline that did not run would prove nothing.

## Structure

| Path | What it holds |
|---|---|
| `agent.yaml` | one agent, no tasks, no handoffs, the same pre-fetch as the optimized package |
| `state.py` | what the pre-fetch reads before the greeting: today's date and the caller ID |
| `targets.yaml` | the same two routes as the optimized package |
| `instructions.md` | the one prompt, holding routing, verification, booking and complaints together |
| `tools/` | the same local Python tools plus the `end_call` builtin, all offered to the agent on every turn |
| `knowledge/refunds/`, `knowledge/services/` | the same two document sets, both held by the one agent |
| `connections/` | the same two carrier connections |

Both examples speak English and run tools without pre-action announcements.
This baseline keeps one continuous conversation, so it has no task or handoff
context settings. After a booking moves, its prompt uses the latest successful
tool result when a caller refers to that booking.

## The differences

What the one agent does differently:

- One prompt holds verification, booking and complaints, and the model reads
  all of it on every turn.
- Every tool is offered on every turn, where the optimized package offers each
  step only its own.
- The model carries the caller's number itself. Nothing confirms the number
  without a task, so `inject:` cannot read it from state. The prompt reads the
  caller ID back, and the model passes the agreed number into each tool call.

Everything else is held identical on purpose, and that is what makes the
comparison worth reading. Both packages run the same tools and the same
pre-fetch, on native Gemini through Google's EU Vertex endpoint, at the same
`pace: snappy`. So a difference you hear between them is a difference the
structure made, not a model, a tool or a turn setting.

## Routes and speech gateway

**Routes.** The same two as the optimized package, one per telephony plane. The
LiveKit target carries inbound calls and the manager transfer over a Twilio
Elastic SIP Trunk (`sip`), and the Pipecat target carries them over Pipecat
Cloud's Twilio websocket (`cloud-websocket`). Browser audio on both. There is no
outbound route.

**Speech gateway.** Both targets send STT and TTS through `eu-north.api.slng.ai`.
Change `params.world_part` on each speech model to choose another
[SLNG gateway](../../docs-site/optimization/regional-infrastructure.mdx).

## Read them side by side

To see the difference for yourself, run the same conversation through each
package and compare. Both resolve the same turn floor and ceiling, which
`build/<target>/compile-report.json` records under `notes` and the emitted
`build/<target>/README.md` spells out, so turn taking is one thing you can rule
out of whatever you hear. Both packages trace to Langfuse, so one call through
each is enough to compare what a request carries:

```sh
unmute dev examples/salon-concierge-single-prompt --target livekit --source from_number=<E.164 number>
unmute dev examples/salon-concierge --target livekit --source from_number=<E.164 number>
```

`--source` seeds the caller ID that both packages' pre-fetch reads. Without it
a browser call has no caller ID, and both ask for the number out loud.

[`scripts/read_langfuse_trace.py`](../../scripts/read_langfuse_trace.py) reads
the newest trace back: transcript, tool calls, and per-span latency.

## Troubleshooting

### The agent will not start

The run reads its credentials from `.env`. This package declares the OpenAI and
Google keys, the SLNG key and the three Langfuse values in `agent.yaml`, and it
traces to Langfuse on every call.

**Fix:** compile first, then copy the generated example and fill it in.

```sh
unmute compile examples/salon-concierge-single-prompt
cp examples/salon-concierge-single-prompt/build/livekit/.env.example examples/salon-concierge-single-prompt/.env
```

### Asking for a manager does not transfer

The escalation is a cold transfer to the `MANAGER_PHONE_NUMBER` destination,
which a carrier has to dial. A browser session stops where the phone leg starts.

**Fix:** put the destination in `.env` in E.164, and make the transfer on a
phone call rather than in the browser.

```
MANAGER_PHONE_NUMBER=<E.164 number>
```

## Where to go next

- [`salon-concierge`](../salon-concierge/) - the same salon, optimized
- [`salon-concierge-unoptimized`](../salon-concierge-unoptimized/) - this prompt with the pre-fetch taken out too
- [`examples/README.md`](../README.md) - every shipped example
- [SLNG gateway](../../docs-site/optimization/regional-infrastructure.mdx) - pick another world part
