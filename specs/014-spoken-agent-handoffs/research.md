# Research: Spoken Agent Handoffs

## Decision 1: `announce:` is exact spoken text

The authoring shape is one optional string on `kind: agent_transfer`:

```yaml
announce: "I’m connecting you with our appointment manager now."
```

The active agent speaks this exact sentence once. Empty text and `{{variable}}`
templates fail validation. Omission keeps the current silent handoff.

Alternatives rejected:

- A boolean cannot say who is taking over or set the tone.
- An LLM-written cue adds latency and can race the receiving agent on a shared
  context. One live Pipecat transfer produced two LLM turns and four TTS jobs.
- A prompt-only rule has no ordering gate and already failed in the live call.

## Decision 2: synthesize once, then activate

LiveKit awaits `session.say(text, allow_interruptions=False)` before returning
the next `Agent`. Pipecat queues one `TTSSpeakFrame(text)` through the source
worker, waits for that worker to receive `BotStartedSpeakingFrame` followed by
`BotStoppedSpeakingFrame`, then calls `activate_worker`. It uses
`super().queue_frame` because `LLMWorker.queue_frame` defers ordinary frames
while a tool is running. The existing Pipecat `args.messages` remains the
receiving worker's reason message and starts the one continuation inference.

Verified 2026-08-15 against:

- https://docs.livekit.io/agents/logic/agents-handoffs/
- https://docs.livekit.io/agents/logic/workflows/
- https://docs.pipecat.ai/pipecat/learn/agent-handoff
- installed `livekit-agents` and `pipecat-ai` source used by the examples

Pipecat's public outgoing `messages=` hook was rejected after the live log
showed its source LLM output arriving after target activation with per-agent
TTS. Its `flush_pipeline()` probe drains only the source pipeline; it does not
wait for the frame's bus round trip through the transport. The direct public
speech frame removes the extra LLM turn, and the transport's stopped-speaking
frame is the barrier that prevents the target from replaying late source audio.

## Decision 3: mark the first LiveKit entry explicitly

The entry agent currently owns the greeting in `on_enter`, so every later
handoff that constructs the entry agent runs the opening again. Give the entry
agent constructor an `initial` flag, pass `initial=True` only from call startup,
and let later entries generate a continuation from their carried context. This
also works for an intentional `history: reset`: it does not mistake a reset
handoff for a new call.

Alternatives rejected:

- Looking for an empty chat context confuses a reset handoff with call start.
- A prompt saying "do not re-greet" cannot override an imperative
  `session.say`.
- A session-global handoff counter is more state than the one call-site fact.

## Decision 4: unexpected Pipecat tool arguments become tool results

The live Pipecat run passed an undeclared `customer_id` to
`check_availability`. Python rejected the call before the handler ran; the model
saw no usable failure and later invented a slot. Add one generated decorator at
the LLM-worker boundary. It accepts runtime keyword arguments, preserves the
original signature used to derive the provider schema, and returns a structured
error naming allowed and unexpected fields. Apply it to every generated worker
tool, not only this example.

Alternative rejected: deleting `customer_id` from one tool description reduces
the chance of this exact slip but leaves every generated tool exposed to the
same runtime failure.

## Decision 5: support is explicit

Add one target capability field for `controls.agent_transfer.announce`. Mark it
emitted on LiveKit and Pipecat. Validation-only targets stay gated until their
drivers exist and the behavior is proved. The emitter/capability agreement test
prevents a green validation result from silently losing the instruction.

## Decision 6: prove behavior, not startup

Offline tests cover strict decode, build, validation, scaffold/TUI round-trip,
generated ordering, greeting-once emission, the extra-argument guard, and
goldens. The final proof is the same round-trip spoken script on both targets,
checking announcement order, remembered customer data, exact returned slot IDs,
one greeting, and zero errors.
