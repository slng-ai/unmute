# Latency

Read this when the brief is "make it faster", "it feels slow", "reduce latency",
or "optimize the agent".

These are the settings we recommend, from building and calling these agents
ourselves. Where a setting looked promising and did not work out, it says so — a
rejected knob is worth as much as an accepted one, because it saves the next
person a round of testing.

Do not quote latency figures to the user. Measure the package in front of you and
report that. For like-for-like model comparisons across providers, point them at
Coval's voice AI benchmarks: https://benchmarks.coval.ai/overview

## Where the silence actually is

A caller's wait on one turn is four spans, and they are nothing like equal:

| span | what it is |
|---|---|
| VAD silence window | how long the caller must stay quiet before the runtime believes they finished |
| transcript | end of speech to the final transcript |
| LLM | one round trip, and a tool turn costs two |
| TTS | synthesis to first audio |

The LLM is usually the largest and the silence window the most predictable.
**Fix them in the order below, which is order of certainty, not order of size.**
Most wasted effort goes on the smallest span.

Two traps to avoid when measuring:

- **`e2e_latency` is not silence.** It anchors on `stopped_speaking_at`, which is
  only cleared when no new speech arrived in the meantime, so on any turn where
  the caller speaks in more than one segment it counts the agent's own speech and
  the caller's talking as "silence". Compare per-span numbers instead.
- **One call proves nothing.** Two calls on an identical build can differ by more
  than any single setting here is worth. Take several calls per setting, and
  compare the many-sample span the knob actually touches rather than the
  end-to-end number, which yields two or three samples per call.

## What to change, in order

### 1. Bind SLNG for listening and speaking

`unmute init` already does this and every shipped example keeps it. It is the
choice that unlocks the rest: the caching described in the docs' Optimization
section only exists on SLNG's own layer, and the per-connection knobs below are
only exposed by the SLNG plugins.

```yaml
models:
  listen:
    transcriber:
      provider: slng
      model: "deepgram/nova:3"
      language: en
  speak:
    voice:
      provider: slng
      model: "deepgram/aura:2"
      voice: "aura-2-thalia-en"
      language: en
```

**Use the proxied id, not the `slng/` hosted one, for listening.** They are the
same vendor model reached two ways, and we measured 280ms against 605ms to the
final transcript. A hosted id is also not offered in every world part, so a
region requirement can decide it for you. Both sample sets were small, so treat
it as a default rather than a law.

### 2. Pick the transcriber on its *final* latency, not its accuracy score

The turn detector reads the transcript to decide whether the caller finished, so
a transcriber that has not finalised yet holds the whole turn open. The metric is
time from end of speech to the final transcript, and it varies widely between
models that score alike on accuracy — two models can return identical words with
very different waits.

Send the user to https://benchmarks.coval.ai/overview to compare, then confirm on
their own audio: accents, phone codecs and typical utterance length all move it.

**Probe the route before shipping it.** Not every model is available on every
transport or region, and some combinations only fail once a call is live. There
is no models listing endpoint, so a route has to be tried.

### 3. Hold the TTS socket open

**LiveKit only.** `pipecat-slng` 0.5.0 does not implement it, and a `params:`
block reaches the plugin verbatim, so on Pipecat the setting is accepted and
discarded with no error. `unmute validate` warns. Do not author it on a package
that only ships to Pipecat.

Off by default. It removes the provider's session setup from the front of every
segment, which shows up most on the first segment of a call and on the fastest
turns.

```yaml
models:
  speak:
    voice:
      provider: slng
      params:
        warm_standby_enabled: true
```

The plugin reports `standby_used` per segment, so whether it engaged is
observable rather than assumed. Note what it does *not* do: LiveKit's
`tts ttfb` tracks synthesis alone and never contained the connection handshake,
so the saving on the socket wait is not all caller-visible.

### 4. Set the silence window deliberately

```yaml
models:
  turn:
    detector:
      provider: local
      model: silero
      pace: balanced
      endpointing_delay: 400ms
```

**Two numbers, and they are not the same number.** Confusing them is the mistake
this section exists to prevent.

`endpointing_delay` is the **floor**: how long silence has to last before the
caller counts as finished. It is charged on every turn, and on LiveKit it also
gates the transcript, because the transcriber is only asked to finalise once the
VAD says the caller stopped.

`pace` is the **ceiling**: the longest a turn may run before closing regardless.
`snappy`, `balanced` or `patient`, defaulting to `balanced`. Nothing in a package
could reach this before it existed, so it sat at the framework defaults of 2.5s
on LiveKit and 3.0s on Pipecat no matter how small the authored window was.

**Lowering the floor alone does not shorten a turn.** A turn that runs long is
sitting at the ceiling, and only `pace` moves that. Reach for `pace` first.

`patient` reproduces the framework defaults exactly, so it is the escape hatch
for a caller reading out digits. `pace` takes no per-target override:
`endpointing_delay` is the field for a value that really is per-target.

**Do not go to the floor.** LiveKit refuses below 250ms and `unmute compile`
rejects it, but even a legal-but-short value splits utterances: a caller who
paused mid-sentence had "tomorrow" committed as one turn and "afternoon" as the
next, and the agent asked twice. Come down from the default in steps and listen
for interruptions. **The defaults differ per target** — LiveKit uses Silero's
window, Pipecat uses `stop_secs`, and they are not the same — so set it
explicitly if the package runs on both.

**On Pipecat this window is one of three stages, not the whole wait.** Silero
decides you stopped making sound, then Pipecat's Smart Turn v3 classifier decides
whether you finished a thought (a small ONNX model inside the framework, running
in every compiled package whether or not it was asked for), and then the turn
waits for the transcriber to mark the transcript final. `endpointing_delay` sets
the first stage and `pace` sets the second.

Both stages are spelled `stop_secs` in the emitted `bot.py` and they are
different fields: `VADParams(stop_secs=...)` is the silence window,
`SmartTurnParams(stop_secs=...)` is the classifier's ceiling.

**On Pipecat the silence window does not move with the pace**, and that is
measured. A window wider than the transcript's arrival time makes Pipecat pay the
flat safety-net second described below, and observed transcripts arrive from
0.27s. So the floor stays at Pipecat's own 0.2s for every pace and the pace
reaches this target through the ceiling alone. Do not "fix" the inconsistency
with LiveKit's column; the inconsistency is the finding.

That third stage was the largest until recently. A transcriber that never marks a
transcript final leaves Pipecat waiting out a safety-net timer, one second by
default, however fast the transcript arrived. Every Unmute package hit that until
`pipecat-slng` 0.5.0, which the Pipecat catalog rows now require. If you are
reading old advice that says `stop_secs` is the real window on Pipecat, that was
true of the setting and not of the wait.

`interruption.minimum_words` used to swap the default stop strategy for a plain
timeout, which dropped the Smart Turn classifier and made turn taking worse. It
no longer does: the classifier survives and carries the `pace` ceiling. The word
count now narrows turn *start* only, which is what it was for.

### 5. Cut LLM round trips before tuning anything else

The largest span, and each control hop costs a full round trip. Collapsing a
multi-step flow into one task, and asking for one piece of information instead of
three, bought more than every connection-level knob combined.

```yaml
models:
  think:
    reasoning:
      provider: openai
      model: gpt-5.6-luna
      params:
        reasoning_effort: "none"
```

`reasoning_effort: "none"` stops the model thinking before its first token. On
the `livekit` target this needs `api: responses` and `use_websocket: true`,
authored on the **target override** if the package has one, because a target's
`params:` block replaces the base block rather than merging into it. A param
authored in the wrong place is discarded with no warning.

**Check that the model honours a parameter at all before relying on one.** Not
every model's thinking can be turned off that way, and a parameter that is
accepted and ignored looks identical to one that worked. `qwen/qwen3-32b` ignored
three spellings across three hosts (2026-08-27, nine requests) and answers only to
its own `/no_think` directive in the prompt. For a model like that, use
`prompt_suffix` on the think entry, which the compiler appends to every system
prompt that binding sends. It is prompt text, so it works wherever the model reads
its instructions, and you can read what it did in the emitted `*_PROMPT`
constants.

Read the number, not the setting. `usage.completion_tokens_details.reasoning_tokens`
on a live response is what says whether thinking is actually off: 0 or 1 means it
is, and a figure in the hundreds means whatever you set was ignored.

### 6. Speak before a tool runs

The tool is usually not the wait — a local handler returns in milliseconds. The
caller is waiting through the second LLM round trip plus the speech after it.

```yaml
local:
  handler: tools/salon.py

announce: Let me check.
```

Keep the line **shorter than the gap**. A long line runs into the answer and
breaks its own promise of a wait: "Okay, one sec." works where "One sec, let me
pull up your details and see what we have" does not. Put it only on tools that
fetch or push data, and never on two tools that fire in the same turn, or the
caller hears two lines for one request.

## Rejected, with the reason

Do not spend a round of testing re-deriving these.

| tried | outcome |
|---|---|
| `service_tier: priority` on OpenAI | Real parameter, verified in the API reference. One call looked good, a second at the same config did not, and the typical turn never moved. Bills at a higher rate, so not worth enabling without evidence from the user's own traffic. |
| `preemptive_tts: True` | Mechanism verified in SDK source, benefit not. Its failure mode is audible: speculative audio for a turn the caller talks through is discarded and still billed. |
| `first_audio_timeout_s` on TTS | On timeout the plugin advances to the next candidate in `connections`; with no list there is none, so it re-raises and a slow turn becomes a dead turn. Needs a fallback candidate to be usable at all. |
| `final_timeout_s` on STT | Same shape: a failover watchdog, not a "finalise sooner" knob. |
| `vad_min_silence_duration_ms` below its default | Inside the noise on one route, worse on another. |
| `max_delay` below the default | Small gain, and it also bounds the legitimate wait for a slow transcript, so pushing it makes the agent answer fragments. |
| Phrase-variation examples in the prompt | Five quoted example sentences read as a script. The agent recited one verbatim and repeated itself either side of it. If the user wants a more natural voice, write rules about form, not speakable sentences. |

## What is not a latency knob

`max_tool_steps` is a ceiling on consecutive tool calls, not a speed control.
Lowering it truncates a working chain.

An intermittently slow provider looks exactly like a latency problem and is not
one. We have had calls where speech took seconds to start and then arrived slower
than real time, which a caller hears as broken audio rather than as delay. Read
the provider's own per-segment timings before changing any setting: if chunk
delivery is slow, nothing on this page will help.
