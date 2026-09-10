# salon-concierge-single-prompt

The baseline. This is [`salon-concierge`](../salon-concierge/) with the booking, verification and complaint workflows in one prompt.

It is here to be read next to that package, not to be copied. Same salon, booking tools, knowledge bases, voice, model and two carrier routes.
The differences in context, state, routing and turn taking are listed below.

It is not a straw man. The prompt is written the way a careful team writes a
single prompt: the voice contract stated once, a clear routing section at the
top, escalation before anything else, and every rule that came out of a call
going wrong. The problem it has is not sloppiness. It is that one prompt has to
be four prompts at once, and it is all in front of the model on every turn.

## Structure

| Path | What it holds |
|---|---|
| `agent.yaml` | one agent, no tasks, no handoffs, no variables, no pre-fetch |
| `targets.yaml` | the same two routes as the optimized package |
| `instructions.md` | the one prompt, holding routing, verification, booking and complaints together |
| `tools/` | the same local Python tools plus the `end_call` builtin, all offered to the agent on every turn |
| `knowledge/refunds/`, `knowledge/services/` | the same two document sets, both held by the one agent |
| `connections/` | the same two carrier connections |

Both examples speak English and run tools without pre-action announcements.
This baseline keeps one continuous conversation, so it has no task or handoff
context settings. After a booking moves, its prompt uses the latest successful
tool result when a caller refers to that booking.

What the one agent does differently:

- The caller is asked for a phone number out loud, even on a route where the
  carrier already supplied one.
- The model calls a tool to find out what day it is, so a caller saying
  "tomorrow" costs two chained requests.
- The number is not a declared variable, so the model reads it off the
  transcript and retypes it into every tool call.

Everything else is held identical on purpose, and that is what makes the
comparison worth reading. Both packages speak to OpenAI directly, with the same
three think params, at the same `pace: snappy`. So a difference you hear between
them is a difference the structure made, not a model, a transport or a turn
setting.

**Routes.** The same two as the optimized package, one per telephony plane. The
LiveKit target carries inbound calls and the manager transfer over a Twilio
Elastic SIP Trunk (`sip`), and the Pipecat target carries them over Pipecat
Cloud's Twilio websocket (`cloud-websocket`). Browser audio on both. There is no
outbound route.

**Speech gateway.** Both targets send STT and TTS through `eu-north.api.slng.ai`.
Change `params.world_part` on each speech model to choose another
[SLNG gateway](../../docs-site/optimization/regional-infrastructure.mdx).

## How to run it

```sh
unmute validate examples/salon-concierge-single-prompt
unmute compile examples/salon-concierge-single-prompt
unmute dev examples/salon-concierge-single-prompt --target livekit
```

Both targets validate with no errors and generate a runnable project, the same
as the optimized package. A baseline that did not run would prove nothing.

To see the difference for yourself, run the same conversation through each
package and compare. Both resolve the same turn floor and ceiling, which
`build/<target>/compile-report.json` records under `notes` and the emitted
`build/<target>/README.md` spells out, so turn taking is one thing you can rule
out of whatever you hear. Both packages trace to Langfuse, so one call through
each is enough to compare what a request carries:

```sh
unmute dev examples/salon-concierge-single-prompt --target livekit
unmute dev examples/salon-concierge --target livekit --source from_number=<E.164 number>
```

`--source` seeds the call fact the optimized package's pre-fetch reads. This
package has no pre-fetch, so it has nothing to seed. It asks for the number out
loud, which is the point.

[`scripts/read_langfuse_trace.py`](../../scripts/read_langfuse_trace.py) reads
the newest trace back: transcript, tool calls, and per-span latency.
