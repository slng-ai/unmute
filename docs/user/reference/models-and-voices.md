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

Required: yes (may be omitted when the target's engine is integrated and a voice id alone selects the model, e.g. ElevenLabs). Values: text. Default: none.

### description

For humans only. Not used to pick a model.

Required: no. Values: text. Default: none. Targets: all five, core.

## Speak model fields

### voice

The voice id, forwarded as-is.

Required: yes. Values: text. Default: none.

### speed

Playback speed multiplier.

Required: no. Values: number. Default: `1.0`. Tag: warn — lowered through the provider's documented slot, warned where none exists.

### language

A per-model override of the package `language` (BCP-47).

Required: no. Values: a BCP-47 tag. Default: the top-level `language`. Tag: gated per target.

Per-agent voices are native on LiveKit, Pipecat, and ElevenLabs, and work on all five.

## Think model fields

### temperature

Sampling temperature.

Required: no. Values: number. Default: provider default. Targets: all five, core — Vapi `model.temperature`, ElevenLabs `prompt.temperature`, Deepgram `think.provider.temperature`, constructor kwargs on Pipecat and LiveKit.

### top_p, top_k

Sampling cutoffs.

Required: no. Values: number. Tag: warn — lowered through the provider's documented slot, warned where none exists.

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
| ElevenLabs | native; entries are model ids only, so models carrying extra settings warn | gated |
| Deepgram | native (ordered provider array, mixed providers allowed) | gated |

On Pipecat, using `fallback` fails validation today; it is a driver maturity gate, not a platform limit.

## Listen model fields

`provider`, `model`, `language`, `params`, and `description` work as above. Listen models also take `fallback` — an ordered list of other listen models to try when the STT fails or times out. The chain stays within the listen section and every entry must share the primary's placement.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | native (`stt.FallbackAdapter`) | gated |
| Pipecat | the driver does not emit listen fallback yet (maturity gate) | gated |
| Vapi | no documented transcriber fallback slot | gated |
| ElevenLabs | listen is integrated; no STT fallback slot | gated |
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
| ElevenLabs | `local` think works only through its documented custom LLM endpoint | gated |
| Deepgram | `local` think works through a custom LLM endpoint | gated |

A hosted provider is the portable choice. A `local` think model needs somewhere to run: fine on code targets, and on ElevenLabs only via its custom LLM endpoint.
