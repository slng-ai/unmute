# salon-concierge-single-prompt

**The baseline.** This is [`salon-concierge`](../salon-concierge/) with every
structural optimization taken back out, and nothing else changed.

It is here to be read next to that package, not to be copied. Same salon, same
tools, same two knowledge bases, same voice, same models, same two targets, same
two carriers. What differs is how the work is arranged, and that is the only
thing the comparison is about.

```sh
bin/unmute validate examples/salon-concierge-single-prompt
bin/unmute compile examples/salon-concierge-single-prompt
bin/unmute dev examples/salon-concierge-single-prompt --target livekit
```

Both targets validate with no errors and generate a runnable project, the same
as the optimized package. A baseline that did not run would prove nothing.

## What the two packages do differently

| | `salon-concierge-single-prompt` | `salon-concierge` |
|---|---|---|
| prompts | one, 13,978 characters | four, 6,700 to 7,906 characters |
| what one request carries | the whole prompt, every turn | only the step's own prompt |
| callables offered to the model | 11, every turn | 5 on the concierge, 7 in the booking step, 2 in verification |
| the caller's number | the model reads it off the transcript and retypes it into every tool call | one declared variable, injected by the compiler, confirmed once |
| today's date | a `get_current_date` tool call, so two chained model requests per relative day | one `prefetch:` entry, read before the greeting |
| who the caller is | asked out loud on every call | read from the carrier before the greeting, then read back for a yes |
| turn taking | `pace: patient`, which reproduces the framework defaults | `pace: balanced`, plus a measured silence window per target |
| thinking | straight at OpenAI, so every turn costs a model request | the same model through the SLNG Context Router, one cache scope per prompt. The router judges which turns are repeatable, so a repeat it serves from cache never reaches the model and a repeat it sends on is expected, not a fault |

Everything in the right-hand column is a feature of Unmute, not of the salon.
The left-hand column is what the same agent looks like without them.

## What one request carries

Measured off the emitted LiveKit project, which is the same code either target
runs:

```text
salon-concierge-single-prompt
  Concierge              prompt 13,978 chars | 11 callables

salon-concierge
  Concierge              prompt  7,005 chars |  5 callables
  Booking                prompt  6,856 chars |  7 callables
  CustomerVerification   prompt  7,906 chars |  2 callables
  ComplaintSpecialist    prompt  6,700 chars |  5 callables
```

The four prompts on the right add up to more text than the one on the left. That
is not the number that matters. **A prompt is re-sent on every request**, so what
the caller pays for is the prompt that is in front of the model on the turn they
are having, and that is roughly half the size here. A caller asking what time the
salon opens does not pay for the booking flow, the digit-readback rules, or the
refund policy wording.

The callables move the same way, and they are easy to forget because they are not
in the prompt file. Every tool the agent can call is sent as a schema on every
request, and the baseline sends eleven of them to a model that, on most turns,
could only usefully call one.

## Reading the difference on a real call

Both packages set `tracing.provider: langfuse`, so one call through each is
enough to see it. Run the same conversation twice and compare the input token
count on the generation observations.

```sh
bin/unmute dev examples/salon-concierge-single-prompt --target livekit
bin/unmute dev examples/salon-concierge --target livekit --source from_number=+34600111222
```

`--source` seeds the call fact the pre-fetch reads. The baseline has no
pre-fetch, so it has nothing to seed: it asks for the number out loud, which is
the point.

[`scripts/read_langfuse_trace.py`](../../scripts/read_langfuse_trace.py) reads the
newest trace back: transcript, tool calls, and per-span latency.

## The turn-taking numbers

`unmute compile` resolves a floor and a ceiling for each target from the
package's `pace`. The difference needs no measurement to see:

| Package | Target | Pace | Floor | Ceiling |
|---|---|---|---|---|
| `salon-concierge-single-prompt` | livekit | patient | 0.4s, from the pace | 2.5s |
| `salon-concierge-single-prompt` | pipecat | patient | 0.2s, from the pace | 3s |
| `salon-concierge` | livekit | balanced | 0.4s, authored | 1.6s |
| `salon-concierge` | pipecat | balanced | 0.2s, authored | 1.6s |

Both the emitted `build/<target>/README.md` (which also says whether the wait
adapts) and `build/<target>/compile-report.json`, under `notes`, name these
same numbers.

`patient` is not a mistake in this file. It reproduces the framework defaults
exactly, which is what a package that never thought about turn taking is running
today. See [Turn taking](../../docs-site/optimization/turn-taking.mdx) for what
each value does, and why the floor and the ceiling are different numbers.

## Routes

The same two as the optimized package, one per telephony plane. The LiveKit
target routes inbound calls and the cold manager transfer over a Twilio Elastic
SIP Trunk (`sip`), and the Pipecat target routes them over Pipecat Cloud's Twilio
websocket (`cloud-websocket`). Browser audio on both. No outbound route:
`channels.phone.outbound` is `false`.

## What this package is not

It is not a straw man. The prompt is written the way a careful team writes a
single prompt: one voice contract stated once, a clear routing section at the
top, escalation before everything else, and every rule that came out of a real
call going wrong. Read it. It is a good prompt.

That is what makes the comparison worth showing. The problem it has is not
sloppiness, it is that **one prompt has to be four prompts at once**, and no
amount of care changes what gets sent on every request.

The failures that follow are the ones the optimized package makes structural
rather than hoped-for:

- **Scope drift.** Every rule is in front of the model on every turn, including
  the ones for a job the caller is not doing.
- **A guardrail nothing enforces.** "Identify the caller first, always, before
  any booking tool runs" is a sentence in a prompt. In the optimized package it
  is `requires: [customer_phone]` on the booking step, and the compiler refuses
  the step to the model rather than to the caller.
- **A phone number in two shapes.** Here the model retypes the number into every
  tool call from what it remembers of the transcript. In the optimized package
  the value is declared once, injected by the compiler, and every tool that holds
  it refuses itself to the model until the caller has agreed to it.
- **Turns nobody needed.** Asking for a number the carrier already supplied, and
  a tool call to find out what day it is.
