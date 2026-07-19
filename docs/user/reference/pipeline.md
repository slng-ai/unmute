# Reference: listen, turn, and placement

There is no `pipeline` block. Models are defined once in [`agent.yaml` models](models-and-voices.md) and the think and speak roles ride each agent's `model:` and `voice:` references. What stays at the top level of `agent.yaml` is the conversation plumbing that is shared by the whole package: `listen` (speech to text) and `turn` (deciding when the caller has finished). These are optional blocks; most packages set them per target in [targets.yaml](targets-yaml.md) instead, because a target may cover a role natively (Deepgram and ElevenLabs build in listen and turn) or with separate parts (Pipecat).

```yaml
# optional, in agent.yaml — a package-wide default; targets can override
listen:
  provider: deepgram
  model: nova-3
turn:
  provider: local
  model: silero
  semantic_endpointing: preferred
```

## placement, in one sentence

`placement` says where the **model** runs, not where the agent runs, and you almost never write it: it is derived from `provider`. `provider: local` runs the model on your own machines next to the agent worker; any other provider calls a hosted vendor endpoint. Running the agent on a laptop in dev versus deploying it later changes nothing (a hosted provider calls the vendor in both). Placement matters because a `local` model must fail loudly on managed targets (they cannot run your models), and because it is the main input to sizing. A model may set `placement:` explicitly to override the derivation — rare, for a self-hosted deployment of a vendor's stack.

## listen

Where the speech-to-text model runs. Carries `provider` + `model` like any model, plus an optional `params` map.

Required: no in the file, but every resolved target whose listen role is open needs an effective listen model (set here or overridden per target). Values: a `provider`/`model` pair. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | any catalogued STT vendor; `provider: local` also runs your own | core / gated |
| Pipecat | any catalogued STT vendor; `provider: local` also runs your own | core / gated |
| Vapi | `provider: local` fails (managed, cannot run your model) | gated |
| ElevenLabs | integrated ASR; a listen block is settings-only and can never name an outside model | gated |
| Deepgram | Deepgram models only; a third-party listen model or `provider: local` fails | gated |

So a hosted listen model is `core` everywhere it is open; `provider: local` is `gated` and works only on LiveKit and Pipecat.

## turn

An optional block. Turn detection is a **preference, not a promise**: whether it applies depends on the bound listen model at runtime.

Required: no. Values: a `provider`/`model` pair plus an optional `semantic_endpointing`. Tag: warn.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | the full turn model is a Cloud feature, so the binding is a preference | warn |
| Pipecat | end-of-turn runs on-device (Silero VAD); the binding is advisory | warn |
| Vapi | turn is built in; a turn model is ignored with a warning | warn |
| ElevenLabs | turn is built in; a turn model is ignored with a warning | warn |
| Deepgram | turn is built into listen; a turn model is ignored with a warning | warn |

### turn.semantic_endpointing

A hint about how the platform should decide the caller has finished, forwarded as a preference.

Required: no. Values: `required | preferred | off`. Default: none. Tag: warn. Whether it truly applies depends on the bound listen model at runtime, so every target treats it as a preference.
