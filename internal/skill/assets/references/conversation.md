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

### When the transcriber is slower than the turn

`endpointing_delay` on the turn entry is how long the runtime waits after speech
stops before it takes the turn. Raise it when a transcriber lands its final text
after the turn is already committed: the agent answers half a sentence, the
caller repeats themselves, and a task can be cancelled mid-step. LiveKit warns
about exactly this in its logs ("consider raising `min_delay`"). It lowers to
LiveKit's endpointing `min_delay` and to Pipecat's VAD `stop_secs`; the hosted
stacks own turn taking, so they ignore it and say so at validation.

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

`semantic_endpointing` warns on both code targets, because what it does depends
on the model bound underneath it. Read the warning to the user rather than
hiding it.

## What to check by ear

A package that validates can still sound wrong. When you hand an agent over,
tell the user to listen for these four:

1. **The greeting.** Does it sound like a person opening a call?
2. **The pause before the first reply.** If it drags, the lever is turn
   detection, then the model, in that order.
3. **Interrupting mid sentence.** Does the agent stop, and does it stop for
   "mm hm" too?
4. **A long silence.** Does the nudge come at a reasonable moment, or does it
   talk over someone who was thinking?
