# Reference: pipeline

`pipeline` describes the three jobs of a voice loop as **roles, not services**: `listen` (speech to text), `speak` (text to speech), and `turn` (deciding when the caller has finished). You set only where the model runs here, with `placement`. The concrete models are bound per target in [targets.yaml](targets-yaml.md).

A target may cover a role natively (Deepgram and ElevenLabs build in listen and turn) or with separate parts (Pipecat). The `reason` role is not in this block; its placement rides on the [model profiles](models-and-voices.md).

```yaml
pipeline:
  listen: { placement: api }
  turn:   { placement: local, semantic_endpointing: preferred }
  speak:  { placement: api }
```

## placement, in one sentence

`placement` says where the **model** runs, not where the agent runs. `api` calls a hosted vendor endpoint; `local` runs the model on your own machines next to the agent worker. Running the agent on a laptop in dev versus deploying it later changes nothing here (`api` calls the vendor in both). It matters because `local` must fail loudly on managed targets (they cannot run your models), and because it is the main input to sizing.

### listen.placement

Where the speech-to-text model runs.

Required: yes. Values: `api | local`. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | `api` and `local` both work | core / gated |
| Pipecat | `api` and `local` both work | core / gated |
| Vapi | `local` fails (managed, cannot run your model) | gated |
| ElevenLabs | `local` fails (managed) | gated |
| Deepgram | `local` fails (no slot for an outside speech-to-text model) | gated |

So `api` is `core` everywhere; `local` is `gated` and works only on LiveKit and Pipecat.

### speak.placement

Where the text-to-speech model runs. Same gating as `listen`: `api` core everywhere, `local` only on LiveKit and Pipecat.

Required: yes. Values: `api | local`. Default: none.

### turn

An optional block. Turn detection is a **preference, not a promise**: whether it applies depends on the bound listen model at runtime.

Required: no. Values: a block with `placement` and optional `semantic_endpointing`. Tag: warn.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | the full turn model is a Cloud feature, so placement is a preference | warn |
| Pipecat | end-of-turn runs on-device (Silero VAD); the binding is advisory | warn |
| Vapi | turn is built in; `placement` is ignored with a warning | warn |
| ElevenLabs | turn is built in; `placement` is ignored with a warning | warn |
| Deepgram | turn is built into listen; `placement` is ignored with a warning | warn |

### turn.placement

Required: yes, if the `turn` block is present. Values: `api | local`. Default: none. Tag: warn (see the table above).

### turn.semantic_endpointing

A hint about how the platform should decide the caller has finished, forwarded as a preference.

Required: no. Values: `required | preferred | off`. Default: none. Tag: warn. Whether it truly applies depends on the bound listen model at runtime, so every target treats it as a preference.
