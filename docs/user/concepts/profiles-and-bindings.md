# Models and overrides

Your agent needs models: something to hear with, something to think with, something to speak with. You define each one **once**, concretely, in `agent.yaml`. When a particular target cannot run a model as defined, you **override** just that entry in `targets.yaml`. Most single-target packages never write a single override.

## Define models once, in agent.yaml

Every model lives in one central `models:` map, grouped into four kind sections: `think` (LLM), `speak` (TTS), `listen` (STT), and `turn` (VAD/end-of-turn). Each entry carries its `provider` and `model` (the pairing that reaches the SDK) and the settings for its kind.

```yaml
models:
  think:
    fast_reasoning:
      description: cheap and quick, for greeting and routing
      provider: openai
      model: gpt-4o-mini
      temperature: 0.4
  speak:
    front_desk:
      description: "warm, concise"
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-thalia-en"
  listen:
    transcriber:
      provider: deepgram
      model: nova-3
    experiment:   # alternate, kept for testing
      provider: soniox
      model: stt-rt-v5
  turn:
    vad:
      provider: local
      model: silero

listen: transcriber   # swap the STT with this one line; omit when only one is defined

agents:
  intake:
    model: fast_reasoning
    voice: front_desk
```

Ten agents can share one `fast_reasoning` — you define it once and reference the name. `listen` and `turn` are call plumbing (one STT hears the whole call, whichever agent is active), so they are selected once for the package, not per agent: a section's sole entry selects itself, and with two or more entries a top-level `listen: <name>` / `turn: <name>` picks one.

The map is a **palette**: entries that nothing currently selects are legal and stay in the file as swappable alternates. Only referenced or selected entries are compiled and forwarded; the rest are inert. That makes model testing a one-line pointer flip, and lets a production package keep its maintained alternates in place.

**`placement`** — where a model runs — you almost never write. It is derived from `provider`: `local` means on your own machines, anything else is a hosted vendor endpoint. It matters because `local` cannot work on a managed target (there is no machine of yours to run it on), and because it drives sizing. For your first agents, hosted providers everywhere are the simple, portable choice.

## Override per target, in targets.yaml

`targets.yaml` carries the infrastructure plus an optional `models:` map that **overrides** an entry, by name, for one target. An override replaces the whole entry (same kind, no partial merge); a target changes what a name means for itself, never which name is selected.

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    # no overrides: every model runs as defined in agent.yaml

  livekit:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    models:
      # Replace the local VAD entry with LiveKit's turn detector.
      vad:
        provider: livekit
        model: turn-detector-mini

  deepgram:
    provider: deepgram
    models:
      # Deepgram runs its own voices and STT, so override just those entries
      # (by name; an override replaces the whole entry):
      front_desk:
        model: aura-2-thalia-en
      transcriber:   # turn rides the listen params: turn is integrated
        model: flux
        params:
          eot_threshold: 0.7
```

Each instance is named after its provider (`pipecat`, not `pipecat-dev`): what
you test is what you deploy.

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
  params:
    max_tokens: 512
```

`params` is forwarded as-is and never validated, exactly like the rest of the model. It configures **only** that component. Platform-wide or telephony settings can never ride through it.

## Open and integrated roles

Not every role is bindable on every target. A role is **open** (you name an outside model) or **integrated** (the platform builds it in and you cannot supply your own).

On LiveKit and Pipecat, all four roles are open: you choose listen, speak,
think, and turn models. The example keeps Pipecat's turn detection local and
uses a LiveKit override for that framework's detector. On Vapi and Deepgram,
turn detection is integrated: a `turn` override can carry settings but cannot
name an outside model. When a role is integrated, an outside model fails because
the value has nowhere to go.

For the full role-by-role table, see
[targets.yaml](../reference/targets-yaml.md). The
[LiveKit guide](../targets/livekit.md) and
[Pipecat guide](../targets/pipecat.md) explain each generated service.

Next: [tiers](tiers.md), the three levels of agent ambition.
