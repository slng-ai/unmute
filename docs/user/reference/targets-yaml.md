# Reference: targets.yaml

`targets.yaml` holds named **target instances**. Each carries the infrastructure
that only makes sense per target—the platform, version pins, transport, and
destinations—plus an optional `models:` **override** map for entries a target
cannot run as [defined in agent.yaml](models-and-voices.md). Model definitions
live in `agent.yaml`; this file only overrides them. The same `agent.yaml`
compiles to every instance. See
[models and overrides](../concepts/profiles-and-bindings.md).

Name a simple instance after its provider, not with a `-dev` suffix: what you
test is what you deploy. When one framework has several real routes or accounts,
include that distinction in the name, such as `pipecat_twilio` or
`livekit_plivo`.

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    transport: carrier-websocket
    carrier: twilio
    connection: primary_phone
    # no model overrides: everything runs as defined in agent.yaml
    destinations:
      billing_line: "+14155550123"

  deepgram:
    provider: deepgram
    models:
      # override entries this target cannot run, by name (whole-entry replace)
      front_desk: { model: aura-2-thalia-en }
      transcriber: { model: flux, params: { eot_threshold: 0.7 } }
```

## Instance fields

| Field | Required | Notes |
|---|---|---|
| `provider` | yes | `livekit \| pipecat \| vapi \| deepgram` |
| `version` | code targets | framework pin; the driver checks it against the range its templates support |
| `pins` | no | independently versioned packages (for example LiveKit plugins) get their own entries |
| `sdk_language` | no | the LiveKit driver currently accepts `python` only |
| `transport`, `carrier` | no | driver vocabulary; telephony controls resolve against these, never the brand alone |
| `connection` | telephony routes | name of one `connections/<name>.yaml`; all telephony channels on this v1 target share it |
| `deployment_region` | no | where the platform deploys the agent; forwarded as declared, never validated. Pipecat writes it to `pcc-deploy.toml`'s `region`; LiveKit puts it on the generated README's `lk agent create --region` (create-time, immutable). A model's own service region rides its `params`/`endpoint_env` instead. |
| `models` | no | per-target overrides, keyed by model name, below |
| `destinations` | if any `human_transfer` is used | map of symbolic name to phone number or SIP address |

Pipecat and LiveKit have drivers today; Vapi and Deepgram instances error on `compile` until their driver ships. `validate` still checks any provider against the schema. See the [target pages](../targets/pipecat.md).

## Multiple telephony routes

A package may declare any number of telephony target instances and Connections.
Each target binds exactly one Connection to one
`(framework, transport, carrier)` route and produces its own
`build/<target-name>/` project. This keeps route-specific credentials, limits,
and generated dependencies separate.

```yaml
targets:
  pipecat_twilio:
    provider: pipecat
    version: "1.5.0"
    transport: carrier-websocket
    carrier: twilio
    connection: twilio_api

  livekit_telnyx:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    transport: sip
    carrier: telnyx
    connection: telnyx_sip
```

Add another target and Connection for every additional supported carrier route.
There is no package-level route-count setting and no multi-carrier target: one
target always emits one adapter. See the
[phone-call route matrix](../learn/07-phone-calls.md#choose-a-supported-carrier-route)
for the accepted Pipecat and LiveKit combinations and Connection keys.

## Overrides

The `models:` map is keyed flat by model names from any `agent.yaml` section — names share one namespace, so the kind comes from the definition. An override **replaces the whole entry** for that target (same kind, no field-level merge); the effective model is the override when present, the `agent.yaml` definition otherwise. A target changes what a name means for itself, never which name the package selects.

Each role is **open** (you name an outside model) or **integrated** (the platform builds it in; a slot carries settings only, never an outside model):

| Role | LiveKit | Pipecat | Vapi | Deepgram |
|---|---|---|---|---|
| `listen` | open | open | open | open (Deepgram models only) |
| `turn` | open | open | integrated | integrated (rides the listen `params`) |
| speak | open | open | open | open (Deepgram plus a fixed third-party list) |
| think | open | open | open | open (custom endpoints allowed) |

Rules:

1. Every used model, and a selected listen model on every open-listen target, must have an effective definition (in `agent.yaml` or overridden here). Without one there is nothing to emit.
2. On a target whose role is integrated, the effective entry for that role carries settings only, and can never name an outside model.
3. A definition or override may carry `params:`, an open map for the bound component's own settings. Forwarded as-is, **never validated**. Platform and telephony settings can never ride through it.
4. Placement is derived from the effective entry's `provider` (`local` → local, else api; an explicit `placement:` overrides).
5. If a driver has no slot for a value (a third-party listen model on Deepgram), compilation fails: the value has nowhere to go.
6. An override naming a model `agent.yaml` does not define, or changing its kind, is an error.
7. Every forwarded model and param is listed in the compile or plan report, so what was sent is always inspectable. Some providers keep fields that do nothing, so run the agent to be sure.

### Why models are never validated

Provider model lists change faster than any shipped catalog, the valid set on code targets depends on the pinned versions, and the real validators already exist: the provider API at apply time, and the generated project at startup. Unmute relays those errors word for word rather than guessing ahead of them. The one thing that **is** checked is the `provider:` name itself, because it selects the emitted integration: the [providers reference](providers.md) lists the accepted names per target, and an unknown one fails at `validate` with the alternatives quoted. To point a role at an OpenAI-compatible endpoint, a model may carry `endpoint_env` (an environment variable name), which the Pipecat driver passes as the service `base_url`.
