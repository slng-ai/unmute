# Reference: targets.yaml

`targets.yaml` holds named **target instances**. Everything provider-specific lives here: the platform, version pins, and the bindings that attach abstract [profiles](models-and-voices.md) to real models. The same `agent.yaml` compiles to every instance. See [profiles and bindings](../concepts/profiles-and-bindings.md).

```yaml
targets:
  pipecat-dev:
    provider: pipecat
    version: "1.5.0"
    transport: daily-sip
    models:
      listen: { provider: deepgram, model: nova-3 }
      turn:   { provider: local, model: silero }
      speak:
        front_desk: { provider: slng, model: "slng/deepgram/aura:2-en", voice: "aura-2-thalia-en" }
      reason:
        fast_reasoning: { provider: openai, model: gpt-4o-mini, params: { temperature: 0.4 } }
    destinations:
      billing_line: "+14155550123"
```

## Instance fields

| Field | Required | Notes |
|---|---|---|
| `provider` | yes | `livekit \| pipecat \| vapi \| elevenlabs \| deepgram` |
| `version` | code targets | framework pin; the driver checks it against the range its templates support |
| `pins` | no | independently versioned packages (for example LiveKit plugins) get their own entries |
| `sdk_language` | no | on LiveKit, warm transfer and MCP need `python` |
| `transport`, `carrier` | no | driver vocabulary; telephony controls resolve against these, never the brand alone |
| `region`, `edition` | no | provider vocabulary; declared, never derived |
| `models` | yes | the binding block, below |
| `destinations` | if any `human_transfer` is used | map of symbolic name to phone number or SIP address |

Only Pipecat has a driver today, so only Pipecat instances compile; other providers error on `compile` until their driver ships. `validate` still checks any provider against the schema. See the [target pages](../targets/pipecat.md).

## Bindings

The `models` block binds each role. `listen` and `turn` bind once each. `reason` binds once per model profile. `speak` binds once per voice profile.

Each role is **open** (you bind an outside model) or **integrated** (the platform builds it in; you can bind settings only, never an outside model):

| Role | LiveKit | Pipecat | Vapi | ElevenLabs | Deepgram |
|---|---|---|---|---|---|
| `listen` | open | open | open | integrated (settings only) | open (Deepgram models only) |
| `turn` | open | open | integrated | integrated | integrated (rides the listen `params`) |
| `speak` | open | open | open | open (ElevenLabs voices only) | open (Deepgram plus a fixed third-party list) |
| `reason` | open | open | open | open (supported list plus custom LLM endpoint) | open (custom endpoints allowed) |

Rules:

1. Every open role in use, and every used model and voice profile, must have a binding. Without one there is nothing to emit.
2. An integrated role's binding is optional. When present it carries settings for the built-in part only, and can never name an outside model.
3. A binding may carry `params:`, an open map for the bound component's own settings (temperature, audio format, turn thresholds). Forwarded as-is, **never validated**. It configures only the bound component; platform and telephony settings can never ride through it.
4. Bindings must agree with the declared `placement`.
5. If a driver has no slot for a value (a custom `speak` endpoint on ElevenLabs, a third-party `listen` model on Deepgram), compilation fails: the value has nowhere to go.
6. Every forwarded binding and param is listed in the compile or plan report, so what was sent is always inspectable. Some providers keep fields that do nothing, so run the agent to be sure.

### Why bindings are never validated

Provider model lists change faster than any shipped catalog, the valid set on code targets depends on the pinned versions, and the real validators already exist: the provider API at apply time, and the generated project at startup. Unmute relays those errors word for word rather than guessing ahead of them. To point a role at an OpenAI-compatible endpoint, a binding may carry `endpoint_env` (an environment variable name), which the Pipecat driver passes as the service `base_url`.
