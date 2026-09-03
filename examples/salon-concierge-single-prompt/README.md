# salon-concierge-single-prompt

The baseline. This is [`salon-concierge`](../salon-concierge/) with every
structural feature taken back out, and nothing else changed.

It is here to be read next to that package, not to be copied. Same salon, same
tools, same knowledge bases, same voice, same models, same two targets, same two
carriers. The only thing that differs is how the work is arranged, and that is
what the comparison is about.

It is not a straw man. The prompt is written the way a careful team writes a
single prompt: the voice contract stated once, a clear routing section at the
top, escalation before anything else, and every rule that came out of a call
going wrong. The problem it has is not sloppiness. It is that one prompt has to
be four prompts at once, and it is all in front of the model on every turn.

## Structure

| Path | What it holds |
|---|---|
| `agent.yaml` | one agent, no tasks, no handoffs, no variables, no pre-fetch |
| `targets.yaml` | the same two targets as the optimized package |
| `instructions.md` | the one prompt, holding routing, verification, booking and complaints together |
| `tools/` | the same local Python tools, all offered to the agent on every turn |
| `knowledge/refunds/`, `knowledge/services/` | the same two document sets, both held by the one agent |
| `connections/` | the same two carrier connections |

What the one agent does differently:

- The caller is asked for a phone number out loud, even on a route where the
  carrier already supplied one.
- The model calls a tool to find out what day it is, so a caller saying
  "tomorrow" costs two chained requests.
- The number is not a declared variable, so the model reads it off the
  transcript and retypes it into every tool call.
- "Identify the caller before any booking tool runs" is a sentence in a prompt
  rather than a `requires:` guard the compiler enforces.
- Turn taking is `pace: patient`, which reproduces the framework defaults.
- Thinking goes straight to the model's own endpoint rather than through the
  Context Router.

**Routes.** The same two as the optimized package, one per telephony plane. The
LiveKit target carries inbound calls and the manager transfer over a Twilio
Elastic SIP Trunk (`sip`), and the Pipecat target carries them over Pipecat
Cloud's Twilio websocket (`cloud-websocket`). Browser audio on both. There is no
outbound route.

## How to run it

```sh
unmute validate examples/salon-concierge-single-prompt
unmute compile examples/salon-concierge-single-prompt
unmute dev examples/salon-concierge-single-prompt --target livekit
```

Both targets validate with no errors and generate a runnable project, the same
as the optimized package. A baseline that did not run would prove nothing.

To see the difference for yourself, run the same conversation through each
package and compare. The resolved turn floor and ceiling for each target are in
`build/<target>/compile-report.json` under `notes`, and in the emitted
`build/<target>/README.md`, so the turn-taking difference needs no measurement.
Both packages trace to Langfuse, so one call through each is enough to compare
what a request carries:

```sh
unmute dev examples/salon-concierge-single-prompt --target livekit
unmute dev examples/salon-concierge --target livekit --source from_number=<E.164 number>
```

`--source` seeds the call fact the optimized package's pre-fetch reads. This
package has no pre-fetch, so it has nothing to seed. It asks for the number out
loud, which is the point.

[`scripts/read_langfuse_trace.py`](../../scripts/read_langfuse_trace.py) reads
the newest trace back: transcript, tool calls, and per-span latency.
