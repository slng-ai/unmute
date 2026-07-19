# Reference: targets.yaml

`targets.yaml` holds named **target instances**. Each carries the infrastructure that only makes sense per target — the platform, version pins, transport, destinations — plus an optional `models:` **override** map for the entries a given target cannot run as [defined in agent.yaml](models-and-voices.md). Model definitions themselves live in `agent.yaml`; this file never defines a model, it only overrides one. The same `agent.yaml` compiles to every instance. See [profiles and bindings](../concepts/profiles-and-bindings.md).

Instances are named after the provider, not a `-dev` suffix: what you test is what you deploy. Add a second instance only when you have a real second environment.

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    transport: daily-sip
    # no model overrides: everything runs as defined in agent.yaml
    destinations:
      billing_line: "+14155550123"

  elevenlabs:
    provider: elevenlabs
    models:
      # override entries this target cannot run, by name (whole-entry replace)
      front_desk: { voice: cgSgspJ2msm6clMCkdW9 }
      transcriber: { params: { user_input_audio_format: ulaw_8000 } }
```

## Instance fields

| Field | Required | Notes |
|---|---|---|
| `provider` | yes | `livekit \| pipecat \| vapi \| elevenlabs \| deepgram` |
| `version` | code targets | framework pin; the driver checks it against the range its templates support |
| `pins` | no | independently versioned packages (for example LiveKit plugins) get their own entries |
| `sdk_language` | no | the LiveKit driver currently accepts `python` only |
| `transport`, `carrier` | no | driver vocabulary; telephony controls resolve against these, never the brand alone |
| `region`, `edition` | no | provider vocabulary; declared, never derived |
| `models` | no | per-target overrides, keyed by model name, below |
| `destinations` | if any `human_transfer` is used | map of symbolic name to phone number or SIP address |

Pipecat, LiveKit, and ElevenLabs have drivers today; Vapi and Deepgram instances error on `compile` until their driver ships. `validate` still checks any provider against the schema. See the [target pages](../targets/pipecat.md).

## Overrides

The `models:` map is keyed flat by model names from any `agent.yaml` section — names share one namespace, so the kind comes from the definition. An override **replaces the whole entry** for that target (same kind, no field-level merge); the effective model is the override when present, the `agent.yaml` definition otherwise. A target changes what a name means for itself, never which name the package selects.

Each role is **open** (you name an outside model) or **integrated** (the platform builds it in; a slot carries settings only, never an outside model):

| Role | LiveKit | Pipecat | Vapi | ElevenLabs | Deepgram |
|---|---|---|---|---|---|
| `listen` | open | open | open | integrated (settings only) | open (Deepgram models only) |
| `turn` | open | open | integrated | integrated | integrated (rides the listen `params`) |
| speak | open | open | open | open (ElevenLabs voices only) | open (Deepgram plus a fixed third-party list) |
| think | open | open | open | open (supported list plus custom LLM endpoint) | open (custom endpoints allowed) |

Rules:

1. Every used model, and a selected listen model on every open-listen target, must have an effective definition (in `agent.yaml` or overridden here). Without one there is nothing to emit.
2. On a target whose role is integrated, the effective entry for that role carries settings only, and can never name an outside model.
3. A definition or override may carry `params:`, an open map for the bound component's own settings. Forwarded as-is, **never validated**. Platform and telephony settings can never ride through it.
4. Placement is derived from the effective entry's `provider` (`local` → local, else api; an explicit `placement:` overrides).
5. If a driver has no slot for a value (a custom speak endpoint on ElevenLabs, a third-party listen model on Deepgram), compilation fails: the value has nowhere to go.
6. An override naming a model `agent.yaml` does not define, or changing its kind, is an error.
7. Every forwarded model and param is listed in the compile or plan report, so what was sent is always inspectable. Some providers keep fields that do nothing, so run the agent to be sure.

### Why models are never validated

Provider model lists change faster than any shipped catalog, the valid set on code targets depends on the pinned versions, and the real validators already exist: the provider API at apply time, and the generated project at startup. Unmute relays those errors word for word rather than guessing ahead of them. The one thing that **is** checked is the `provider:` name itself, because it selects the emitted integration: the [providers reference](providers.md) lists the accepted names per target, and an unknown one fails at `validate` with the alternatives quoted. To point a role at an OpenAI-compatible endpoint, a model may carry `endpoint_env` (an environment variable name), which the Pipecat driver passes as the service `base_url`.
