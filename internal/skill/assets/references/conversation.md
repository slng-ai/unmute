# The conversation

Who speaks first, whether the caller can talk over the agent, what happens in a
silence, and when the call ends.

## The block

```yaml agent.yaml
conversation:
  greeting:
    speaks_first: agent
    text: "Hi, this is Sage and Stone Salon. How can I help with your appointment?"
  interruption:
    enabled: true
    minimum_words: 2
    ignore_phrases:
      - "mm hm"
      - "right"
  inactivity:
    nudge_after: 8s
    end_after: 45s
  max_duration: 20m
```

| Field | Values |
|---|---|
| `greeting.speaks_first` | `agent` or `user` |
| `greeting.text` | the opening line, requires `speaks_first: agent` |
| `interruption.enabled` | required whenever `interruption` is present |
| `interruption.minimum_words` | how much speech counts as an interruption |
| `interruption.ignore_phrases` | phrases that do not interrupt |
| `interruption.protect` | `greeting`, `tool_calls`, or both; Pipecat only |
| `inactivity.nudge_after`, `inactivity.end_after` | durations |
| `max_duration` | a duration |
| `thinking_audio` | `none` or `subtle` |

## The greeting

Write it. Do not leave it out and hope.

With no `greeting` block at all, both code targets warn and the agent opens with
a line the model writes on the spot. That is a different first impression every
call, which is almost never what a user wants:

```
  livekit: LiveKit has no greeting block: the agent opens with a model-written line
  pipecat: Pipecat has no greeting block: the agent opens with a model-written line
```

`speaks_first: user` means the agent says nothing until spoken to. Use it for
outbound calls where the person answering speaks first, and for anything where a
robot opening would be wrong.

A greeting renders `{{variables}}` **once, at session start**, so it can only
name a variable that already has a value: `source: call_start`, a system source,
or a `default`. A greeting naming a variable with none of those is refused, not
silently blanked.

What decides it is the value, not the source. A `source: conversation` variable
**with a `default`** is legal in a greeting and renders that default — which is
usually not what you want, because the greeting is built before the model has
learned anything, and it does not re-render when the model saves a value later.
A conversation variable earns its keep at the per-call sites: `inject:` on a
tool, and a webhook `path`.

Write the greeting the way it will sound. It is spoken text, so the rules in
`prompting.md` apply to it more than to anything else in the package.

## Interruption

```yaml
  interruption:
    enabled: true
```

`enabled` is required the moment the block exists. There is no default to fall
into.

`minimum_words` and `ignore_phrases` both compile on Pipecat and LiveKit. They
are the fix for an agent that stops talking every time the caller says "mm hm".
Reach for `ignore_phrases` before turning interruption off, because an agent
that cannot be interrupted is worse than one that stops too often.

`protect` names the stretches the caller cannot talk over while barge-in stays on
for the rest of the call. It takes `greeting`, `tool_calls`, or both, and it
compiles on Pipecat only:

```yaml
  interruption:
    enabled: true
    protect: [greeting]
```

You rarely need to write it, because a phone route protects the greeting on its
own and says so in the compile output. That default exists for a reason worth
knowing: a phone leg has no echo cancellation. A caller on speakerphone sends the
agent's own greeting back through their microphone, the transcriber reports it as
caller speech, and the agent interrupts itself a second into the call. The
garbled turn then sits in the model's context for the rest of the conversation
and every later answer is built on top of it.

Write `protect` when you want something other than that default: add
`tool_calls` so a cough cannot abandon a booking mid-write, or set `protect: []`
to ask for no protection at all. An empty list is not the same as leaving the key
out, and that is the whole point of it: leaving it out accepts the route's
default, and `[]` overrides it.

`protect` and `enabled: false` are refused together, because `enabled: false`
already mutes the caller for the whole call.

## Inactivity and duration

```yaml
  inactivity:
    nudge_after: 8s
    end_after: 45s
  max_duration: 20m
```

Both warn on every target, and the warning is real: the driver has to range
check the durations, so an absurd value is your problem rather than the
compiler's. Pick numbers a person would pick. A nudge under about five seconds
interrupts a caller who is thinking.

`max_duration` is the cap that stops a stuck call from running all day. Set one
on anything that will take real traffic.

## Thinking audio

```yaml
  thinking_audio: subtle
```

`none` disables thinking audio. `subtle` enables it and is **LiveKit only
today**. Pipecat refuses `subtle` by name:

```
pipecat: the Pipecat driver does not emit thinking audio yet
```

So do not use `subtle` in a package that declares a Pipecat target, and do not
offer it as a fix for a slow agent on Pipecat. Fix the latency instead.

## Turn detection

Turn detection decides when the caller has finished speaking. It is the single
biggest lever on whether an agent feels natural, and it is a model entry like
any other:

```yaml agent.yaml
models:
  turn:
    detector:
      provider: local
      model: silero
```

### The window of silence that ends a turn

`endpointing_delay` on the turn entry is how long the caller has to stay quiet
before the runtime treats them as finished. It is the **floor on every turn**:
nothing downstream can start earlier, because the transcriber is only asked to
finalise once the VAD reports end of speech.

Lower it to answer sooner, at the cost of cutting off a caller who pauses
mid-sentence. Raise it to give them more room, or when a transcriber lands its
final text after the turn is already committed, which makes the agent answer half
a sentence.

It lowers to the VAD silence window on both code targets: LiveKit's prewarmed
`silero.VAD.load(min_silence_duration=...)` and Pipecat's `VADParams(stop_secs=...)`.
The hosted stacks own turn taking, so they ignore it and say so at validation.

Two things worth knowing before you set it:

- **The defaults differ per target.** LiveKit's Silero VAD defaults to `550ms`
  and Pipecat's `stop_secs` to `200ms`, so a package with nothing authored
  answers noticeably faster on Pipecat. Set it explicitly if the package runs on
  both and you want them to match.
- **LiveKit floors it at `250ms`** and `unmute compile` rejects anything lower,
  because the turn detector raises rather than degrading. It is *not* lowered to
  LiveKit's `turn_handling` endpointing `min_delay`: that cannot fire before the
  VAD reports end of speech, so a value under the window would do nothing there.

```yaml agent.yaml
models:
  turn:
    detector:
      provider: local
      model: silero
      endpointing_delay: 1s
```

Neither target has a catalogue of turn vendors, because each ships its own
mechanism rather than exposing a list to bind. Pipecat runs Silero locally.
LiveKit runs its own turn detector, and the usual shape is to override the entry
on the LiveKit target:

```yaml targets.yaml
targets:
  livekit:
    provider: livekit
    version: "1.6.10"
    sdk_language: python
    models:
      detector:
        provider: livekit
        model: turn-detector-mini
```

Every LiveKit target with a turn entry gets this warning, whether or not you
wrote a `placement:` field. LiveKit decides for itself where turn detection
runs, so the field is a preference there:

```
  livekit: LiveKit turn placement is a preference
```

It is information, not a problem, and the command still exits 0. Do not go
looking for a `placement:` line to remove; there usually is not one.

`semantic_endpointing` no longer warns on the code targets. It used to, and the
warning said its effect depended on the bound model; the real situation was that
the value reached no emitted project at all. It is load-bearing now:

- `off` removes the semantic end-of-turn model. LiveKit decides end of turn from
  voice activity alone; Pipecat ends the turn on a speech timeout instead of the
  smart-turn analyzer's verdict. On Pipecat this also gives up the `pace`
  ceiling, because that ceiling is the analyzer's own setting.
- `preferred`, `required` and leaving it out all keep the model. Both code
  targets can always provide one, so there is nothing for `required` to assert
  that is not already true.

Reach for `off` only if the semantic model is judging turns wrong for your
callers. It is a downgrade in exchange for predictability, not a speed win.

## What to check by ear

A package that validates can still sound wrong. When you hand an agent over,
tell the user to listen for these four:

1. **The greeting.** Does it sound like a person opening a call?
2. **The pause before the first reply.** If it drags, the lever is `pace` on the
   turn binding, then the model, in that order. `pace` sets the ceiling on a
   turn; `endpointing_delay` sets only the floor, and lowering the floor alone
   does not shorten a turn.
3. **Interrupting mid sentence.** Does the agent stop, and does it stop for
   "mm hm" too?
4. **A long silence.** Does the nudge come at a reasonable moment, or does it
   talk over someone who was thinking?
