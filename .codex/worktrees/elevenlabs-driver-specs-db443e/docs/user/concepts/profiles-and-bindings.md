# Profiles and bindings

Your agent needs models: something to hear with, something to think with, something to speak with. But the exact model names differ per platform, and they change often. Unmute keeps the two apart. You name **profiles** in `agent.yaml`, abstractly. You **bind** them to real models in `targets.yaml`, per target.

## Profiles: abstract, in agent.yaml

A profile is a name plus a short description, nothing more. It says "there is a reasoning model here and here is what it is for", without saying which one.

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

Agents then refer to profiles by name (`model: fast_reasoning`, `voice: front_desk`). Nothing here is tied to OpenAI, ElevenLabs, or anyone else. The description is for humans; Unmute never picks a model for you based on it.

**`placement`** is the one real setting on a model profile. It says where the model runs: `api` means a hosted vendor endpoint, `local` means on your own machines next to the agent. It matters because `local` cannot work on a managed target (there is no machine of yours to run it on), and because it drives sizing. For your first agents, `api` everywhere is the simple, portable choice.

## Bindings: concrete, in targets.yaml

A **binding** attaches a profile to a real model, for one target. This is where provider names and model ids finally appear:

```yaml
targets:
  pipecat-dev:
    provider: pipecat
    version: "1.5.0"
    models:
      listen: { provider: deepgram, model: nova-3 }
      turn:   { provider: local, model: silero }
      speak:
        front_desk: { provider: slng, model: "slng/deepgram/aura:2-en", voice: "aura-2-thalia-en" }
        specialist: { provider: slng, model: "slng/deepgram/aura:2-en", voice: "aura-2-orion-en" }
      reason:
        fast_reasoning:    { provider: openai, model: gpt-4o-mini, params: { temperature: 0.4 } }
        careful_reasoning: { provider: openai, model: gpt-4o }
```

Note the four roles: `listen` (hear), `turn` (detect end of turn), `speak` (voice), `reason` (think). `listen` and `turn` bind once each. `reason` binds once per model profile. `speak` binds once per voice profile. Every profile you used in `agent.yaml` needs a binding here, or there is nothing to generate.

## Forwarded verbatim, never validated

This is the rule that surprises people, so it is stated bluntly: **the provider, model, voice, and `params` in a binding are passed to the platform exactly as written, and Unmute never checks them.**

Unmute will not tell you that `gpt-4o` is spelled wrong, or that a voice id does not exist. It cannot, and on purpose:

- Provider model lists change faster than any tool could track.
- On a code target the valid set depends on the exact package versions you pinned.
- The real checkers already exist: the provider's API, and the generated project when it starts. Unmute relays their errors word for word rather than guessing ahead of them.

So a binding typo is caught when you run the agent, not at validate time. To make this safe, every binding and every `params` value is listed in the compile report, so what got sent is always something you can inspect. And because some platforms keep fields that do nothing, the honest advice is: run the agent to be sure.

## params: the one place provider settings live

A binding may carry `params:`, an open map of settings for that one component: temperature, audio format, turn thresholds, whatever the provider puts there.

```yaml
reason:
  fast_reasoning: { provider: openai, model: gpt-4o-mini, params: { temperature: 0.4 } }
```

`params` is forwarded as-is and never validated, exactly like the rest of the binding. It is the single sanctioned way to reach a provider's own knobs, and it configures **only** the bound component. Platform-wide or telephony settings can never ride through it.

## Open and integrated roles

Not every role is bindable on every target. A role is **open** (you bind an outside model to it) or **integrated** (the platform builds it in and you cannot supply your own).

On Pipecat, all four roles are open: you choose the listen model, the voice, and the reasoning model freely, and `turn` runs on your machine with a local voice-activity detector. On managed targets, more roles are integrated. On ElevenLabs, for instance, listening is integrated: the `listen` binding can only carry settings, never name an outside speech model. When a role is integrated, trying to bind an outside model to it fails, because the value has nowhere to go.

For the full role-by-role table across all five targets, see the [Pipecat target page](../targets/pipecat.md) for Pipecat's column, or `SCHEMA.md` section 6 for all five.

Next: [tiers](tiers.md), the three levels of agent ambition.
