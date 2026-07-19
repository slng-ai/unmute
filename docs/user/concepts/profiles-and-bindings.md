# Models and overrides

Your agent needs models: something to hear with, something to think with, something to speak with. You define each one **once**, concretely, in `agent.yaml`. When a particular target cannot run a model as defined, you **override** just that entry in `targets.yaml`. Most single-target packages never write a single override.

## Define models once, in agent.yaml

Every model lives in one unified `models:` map, each with its `provider` and `model` (the pairing that reaches the SDK) and the settings for its kind. A model's kind follows from where an agent refers to it: `voice:` names a speak model, `model:` names a think model.

```yaml
models:
  fast_reasoning:                 # think model
    description: cheap and quick, for greeting and routing
    provider: openai
    model: gpt-4o-mini
    temperature: 0.4
  front_desk:                     # speak model
    description: "warm, concise"
    provider: slng
    model: "slng/deepgram/aura:2-en"
    voice: "aura-2-thalia-en"

agents:
  intake:
    model: fast_reasoning
    voice: front_desk
```

Ten agents can share one `fast_reasoning` — you define it once and reference the name. `listen` and `turn` (hear, and detect end of turn) are the two remaining roles; they are per-target plumbing, so they live in `targets.yaml`.

**`placement`** — where a model runs — you almost never write. It is derived from `provider`: `local` means on your own machines, anything else is a hosted vendor endpoint. It matters because `local` cannot work on a managed target (there is no machine of yours to run it on), and because it drives sizing. For your first agents, hosted providers everywhere are the simple, portable choice.

## Override per target, in targets.yaml

`targets.yaml` carries the infrastructure plus an optional `models:` map that **overrides** an entry for one target — and the `listen`/`turn` role slots. An override replaces the whole entry (same kind, no partial merge).

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    models:
      listen: { provider: deepgram, model: nova-3 }
      turn:   { provider: local, model: silero }
      # no speak/think overrides: they come from agent.yaml as-is

  elevenlabs:
    provider: elevenlabs
    models:
      # ElevenLabs can't run the SLNG voice, so override just that entry:
      front_desk: { voice: cgSgspJ2msm6clMCkdW9 }
      fast_reasoning: { model: gemini-2.5-flash }
```

The instance is named after the provider (`pipecat`, not `pipecat-dev`): what you test is what you deploy.

## Forwarded verbatim, never validated

This is the rule that surprises people, so it is stated bluntly: **the provider, model, voice, and `params` on a model are passed to the platform exactly as written, and Unmute never checks them.**

Unmute will not tell you that `gpt-4o` is spelled wrong, or that a voice id does not exist. It cannot, and on purpose:

- Provider model lists change faster than any tool could track.
- On a code target the valid set depends on the exact package versions you pinned.
- The real checkers already exist: the provider's API, and the generated project when it starts. Unmute relays their errors word for word rather than guessing ahead of them.

So a typo is caught when you run the agent, not at validate time. To make this safe, every model and every `params` value is listed in the compile report, so what got sent is always something you can inspect. And because some platforms keep fields that do nothing, the honest advice is: run the agent to be sure.

## params: the escape hatch for provider knobs

Beyond the typed fields (`temperature`, `top_p`, `top_k`, `speed`), a model may carry `params:`, an open map of settings for that one component: audio format, turn thresholds, whatever the provider puts there.

```yaml
fast_reasoning:
  provider: openai
  model: gpt-4o-mini
  params: { max_tokens: 512 }
```

`params` is forwarded as-is and never validated, exactly like the rest of the model. It configures **only** that component. Platform-wide or telephony settings can never ride through it.

## Open and integrated roles

Not every role is bindable on every target. A role is **open** (you name an outside model) or **integrated** (the platform builds it in and you cannot supply your own).

On Pipecat, all four roles are open: you choose the listen model, the voice, and the think model freely, and `turn` runs on your machine with a local voice-activity detector. On managed targets, more roles are integrated. On ElevenLabs, for instance, listening is integrated: a `listen` override can only carry settings, never name an outside speech model. When a role is integrated, trying to name an outside model to it fails, because the value has nowhere to go.

For the full role-by-role table across all five targets, see the [Pipecat target page](../targets/pipecat.md) for Pipecat's column, or `SCHEMA.md` section 6 for all five.

Next: [tiers](tiers.md), the three levels of agent ambition.
