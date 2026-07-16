# Reference: models and voices

`models` and `voices` declare **profiles**: abstract names with descriptions. They are bound to real models per target in [targets.yaml](targets-yaml.md). The split is explained in [profiles and bindings](../concepts/profiles-and-bindings.md).

```yaml
models:
  fast_reasoning:
    description: cheap and quick, for greeting and routing
    placement: api
  careful_reasoning:
    description: slower and careful, for billing work
    placement: api

voices:
  front_desk: { description: "warm, concise" }
  specialist: { description: "slower, more deliberate" }
```

There is no `tier` field on a profile. Nothing would use it; Unmute never picks a model for you.

## Model profile fields

### description

For humans only. Not used to pick a model.

Required: no. Values: text. Default: none. Targets: all five, core.

### placement

Where this reasoning model runs.

Required: yes. Values: `api | local`. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | `api` and `local` both work | gated |
| Pipecat | `api` and `local` both work | gated |
| Vapi | `local` reasoning fails (custom LLM endpoint unverified) | gated |
| ElevenLabs | `local` reasoning works only through its documented custom LLM endpoint | gated |
| Deepgram | `local` reasoning works (a custom reason endpoint is fine) | gated |

`api` is the portable choice. A `local` reasoning model needs somewhere to run: fine on code targets, and on ElevenLabs only via its custom LLM endpoint.

### fallback

An ordered list of other model profile names to try when the primary fails or times out.

Required: no. Values: ordered list of profile names. Default: none. The chain is cycle-checked, and every profile in a chain must land in the same role kind and placement on the resolved target.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | native (`FallbackAdapter`) | gated |
| Pipecat | supported, but the driver does not emit it yet (maturity gate) | gated |
| Vapi | native, but same-provider chains only; a cross-provider chain fails | gated |
| ElevenLabs | native; entries are model ids only, so profiles carrying binding `params` warn | gated |
| Deepgram | native (ordered provider array, mixed providers allowed) | gated |

On Pipecat, using `fallback` fails validation today; it is a driver maturity gate, not a platform limit.

## Voice profile fields

A voice profile carries only `description`. It is bound per target as `speak.<profile>`.

Required: `description` no. Values: text. Default: none. Targets: all five, core. Per-agent voices are native on LiveKit, Pipecat, and ElevenLabs, and work on all five.
