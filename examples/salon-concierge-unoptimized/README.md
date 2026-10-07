# salon-concierge-unoptimized

The starting point. This is [`salon-concierge`](../salon-concierge/) with every
unmute optimization taken out: one prompt, no tasks, no handoffs, no pre-fetch
and no injected values.

It is here to measure what the optimizations are worth, not to be copied. Read
it next to [`salon-concierge`](../salon-concierge/), the same salon with all of
them, and [`salon-concierge-single-prompt`](../salon-concierge-single-prompt/),
the same one prompt with pre-fetch added.

On this page:

- [Quickstart](#quickstart) - validate, compile, run
- [Structure](#structure) - what the package holds
- [The differences](#the-differences) - what it does without unmute
- [Routes and speech gateway](#routes-and-speech-gateway) - the two carrier planes
- [Read them side by side](#read-them-side-by-side) - one call through each
- [Troubleshooting](#troubleshooting) - what stops a run
- [Where to go next](#where-to-go-next) - read the optimized package

## Quickstart

```sh
unmute validate examples/salon-concierge-unoptimized
unmute compile examples/salon-concierge-unoptimized
cp examples/salon-concierge-unoptimized/build/livekit/.env.example \
   examples/salon-concierge-unoptimized/.env                        # then fill it in
unmute dev examples/salon-concierge-unoptimized --target livekit
```

Both targets validate with no errors and generate a runnable project, the same
as the optimized package. A starting point that did not run would prove nothing.

## Structure

| Path | What it holds |
|---|---|
| `agent.yaml` | one agent, no tasks, no handoffs, no pre-fetch, no `state.py` |
| `targets.yaml` | the same two routes as the optimized package |
| `instructions.md` | the one prompt, holding routing, verification, booking and complaints together |
| `tools/` | the optimized package's local Python tools, plus `get_current_date`, all offered on every turn |
| `knowledge/refunds/`, `knowledge/services/` | the same two document sets, both held by the one agent |
| `connections/` | the same two carrier connections |

## The differences

What the one agent does without unmute's optimizations:

- One prompt holds verification, booking and complaints, and the model reads
  all of it on every turn.
- Every tool is offered on every turn.
- The caller is asked for a phone number out loud, even on a route where the
  carrier already supplied one.
- The model calls `get_current_date` to find out what day it is, so a caller
  saying "tomorrow" costs a chained request.
- The model carries the caller's number itself and passes it into each tool
  call.

Everything else is held identical on purpose. The three salon packages run the
same booking tools, on native Gemini through Google's EU Vertex endpoint, at the
same `pace: snappy`. So a difference between them is a difference the
optimizations made, not a model, a tool or a turn setting.

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

Run the same conversation through this package and the optimized one. Both trace
to Langfuse, so one call through each is enough to compare what a request
carries:

```sh
unmute dev examples/salon-concierge-unoptimized --target livekit
unmute dev examples/salon-concierge --target livekit --source from_number=<E.164 number>
```

`--source` seeds the caller ID that the optimized package's pre-fetch reads.
This package has no pre-fetch, so it asks for the number out loud, which is the
point.

[`scripts/read_langfuse_trace.py`](../../scripts/read_langfuse_trace.py) reads
the newest trace back: transcript, tool calls, per-span latency and token usage.

## Troubleshooting

### The agent will not start

The run reads its credentials from `.env`. This package declares the OpenAI and
Google keys, the SLNG key and the three Langfuse values in `agent.yaml`, and it
traces to Langfuse on every call.

**Fix:** compile first, then copy the generated example and fill it in.

```sh
unmute compile examples/salon-concierge-unoptimized
cp examples/salon-concierge-unoptimized/build/livekit/.env.example examples/salon-concierge-unoptimized/.env
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
- [`salon-concierge-single-prompt`](../salon-concierge-single-prompt/) - the same one prompt, with pre-fetch
- [`examples/README.md`](../README.md) - every shipped example
- [SLNG gateway](../../docs-site/optimization/regional-infrastructure.mdx) - pick another world part
