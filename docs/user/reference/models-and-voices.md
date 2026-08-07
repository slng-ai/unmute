# Reference: models

`models` is the central map, grouped into four kind sections: `think` (LLM),
`speak` (TTS), `listen` (STT), and `turn` (VAD/end-of-turn). Every model the
package can use is defined here once; there is no separate `voices` block. A
model's kind is its section. Each entry carries `provider`, `model`, and the
settings for its kind. A target may override an entry it cannot run as defined;
see [models and overrides](../concepts/profiles-and-bindings.md).

```yaml
models:
  think:
    fast_reasoning:
      description: cheap and quick, for greeting and routing
      provider: openai
      model: gpt-4o-mini
      temperature: 0.4
    careful_reasoning:
      description: slower and careful, for billing work
      provider: openai
      model: gpt-4o
  speak:
    front_desk:
      description: "warm, concise"
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-thalia-en"
  listen:
    transcriber: { provider: deepgram, model: nova-3 }
  turn:
    vad: { provider: local, model: silero }
```

References must land in the right section: an agent's `voice:` names a `speak` entry; an agent's or task's `model:`, a `summarizer:`, and a `fallback` list name `think` entries; the top-level `listen:`/`turn:` selectors name entries of their sections (see [listen, turn, and placement](pipeline.md)). A name referenced but not defined is an error naming the reference; a reference into the wrong section is an error naming both kinds; the same name in two sections is an error.

The map is a **palette**: entries that nothing references are legal, swappable alternates. Only referenced or selected entries are compiled and forwarded; the rest are inert, so you can keep test alternates maintained in the file. There is no `tier` field: Unmute never picks a model for you. `placement` is not written here in the common case — it is derived from `provider`.

## Fields on every model

### provider

The catalogue vendor. Selects which service the driver emits. `local` marks an on-machine model.

Required: yes (a hosted managed model may leave it implicit where the provider's engine is integrated). Values: a catalogue vendor. Default: none.

### model

The model identity, forwarded to that provider's SDK verbatim — `gpt-4o-mini` for OpenAI, `slng/deepgram/aura:2-en` for SLNG. Never parsed or rewritten.

Required: yes. Values: text. Default: none.

### description

For humans only. Not used to pick a model.

Required: no. Values: text. Default: none. Targets: all four, core.

## Speak model fields

### voice

The voice id, forwarded as-is.

Required: yes. Values: text. Default: none.

### speed

Playback speed multiplier.

Required: no. Values: number. Default: `1.0`. Tag: gated per target — lowered through the provider's own kwarg name, and setting it on an integration with no speed slot is a compile error.

Each provider spells it differently and Unmute translates: rime takes `speed_alpha` on LiveKit and `speedAlpha` on Pipecat, sarvam takes `pace`, inworld takes `speaking_rate`. You write `speed` either way.

Some integrations have no speed control at all, and those fail validation with the provider named rather than emitting a setting that does not exist. `provider: deepgram` is one, because Aura exposes no rate control. Note this is about the provider you bind, not the voice: the `slng` route that `unmute init` scaffolds does have a speed slot, even when the model behind it is an Aura voice.

If a provider has a knob Unmute does not model, put it in `params:` under the provider's own name. Keys there are forwarded exactly as written, never renamed and never checked, so `params: {speed: 1.1}` sends a literal `speed` even to a provider Unmute has no slot for. That is your escape hatch, and it is the only way to reach a setting like Cartesia's nested `generation_config`.

### language

The spoken language for this STT or TTS model, as a BCP-47 tag (N16). It lives only on the model, never at the package level. When unset, no language is sent and the provider default — or the language already encoded in the model route, such as `slng/deepgram/nova:3-en` — applies.

Required: no. Values: a BCP-47 tag such as `en` or `es-MX`. Default: none (nothing is emitted). Tag: gated per target — setting it on a target whose integration has no language slot is a compile error.

Per-agent voices are native on LiveKit and Pipecat, and work on all four.

## Think model fields

### temperature

Sampling temperature.

Required: no. Values: number. Default: provider default. Targets: all four, core — Vapi `model.temperature`, Deepgram `think.provider.temperature`, constructor kwargs on Pipecat and LiveKit.

### top_p, top_k

Sampling cutoffs.

Required: no. Values: number. Tag: gated per target — setting one on an integration that has no slot for it is a compile error.

`top_k` is the one to watch. Every Pipecat LLM service accepts it. On LiveKit only `anthropic` does, and `anthropic` is the one LiveKit LLM that has no `top_p`. If a target rejects it, the error names the parameter, the provider and the role.

### params

An open map of anything else the bound component accepts (`max_tokens` where a slot exists; never forwarded to Deepgram, which has no max-tokens slot). Forwarded verbatim, never validated.

Required: no. Values: a map. Default: none.

### fallback

An ordered list of other think-model names to try when the primary fails or times out.

Required: no. Values: ordered list of model names. Default: none. The chain is cycle-checked, and every model in a chain must land in the same role kind and placement on the resolved target.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | native (`FallbackAdapter`) | gated |
| Pipecat | supported, but the driver does not emit it yet (maturity gate) | gated |
| Vapi | native, but same-provider chains only; a cross-provider chain fails | gated |
| Deepgram | native (ordered provider array, mixed providers allowed) | gated |

On Pipecat, using `fallback` fails validation today; it is a driver maturity gate, not a platform limit.

## Listen model fields

`provider`, `model`, `language`, `params`, and `description` work as above. Listen models also take `fallback` — an ordered list of other listen models to try when the STT fails or times out. The chain stays within the listen section and every entry must share the primary's placement.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | native (`stt.FallbackAdapter`) | gated |
| Pipecat | the driver does not emit listen fallback yet (maturity gate) | gated |
| Vapi | no documented transcriber fallback slot | gated |
| Deepgram | `agent.listen` takes a single provider; no fallback slot | gated |

```yaml
models:
  listen:
    transcriber:
      provider: slng
      model: "slng/deepgram/nova:3-en"
      fallback: [backup_stt]
    backup_stt:
      provider: deepgram
      model: nova-3
```

### placement

Where this model runs. Derived from `provider` (`local` → local, else api); set explicitly only to override.

Required: no. Values: `api | local`. Default: derived.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | hosted and `local` both work | gated |
| Pipecat | hosted and `local` both work | gated |
| Vapi | `local` think fails (custom LLM endpoint unverified) | gated |
| Deepgram | `local` think works through a custom LLM endpoint | gated |

A hosted provider is the portable choice. A `local` think model needs somewhere to run: fine on code targets.
