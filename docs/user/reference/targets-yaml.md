# Reference: targets.yaml

`targets.yaml` holds named **target instances**. Each carries the infrastructure
that only makes sense per target—the platform, version pins, the deployment
region, and the connection its calls arrive on—plus an optional `models:`
**override** map for entries a target cannot run as
[defined in agent.yaml](models-and-voices.md). Model definitions live in
`agent.yaml`; this file only overrides them. The same `agent.yaml` compiles to
every instance. See
[models and overrides](../concepts/profiles-and-bindings.md).

A target says nothing about **how** a call reaches it. It names one connection,
and [that file](connections.md) declares the transport, the carrier, and the
credentials.

Name a simple instance after its provider, not with a `-dev` suffix: what you
test is what you deploy. When one framework has several real routes or accounts,
include that distinction in the name, such as `pipecat_twilio` or
`livekit_plivo`.

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    connection: primary_phone
    # no model overrides: everything runs as defined in agent.yaml

  deepgram:
    provider: deepgram
    models:
      # override entries this target cannot run, by name (whole-entry replace)
      front_desk:
        model: aura-2-thalia-en
      transcriber:
        model: flux
        params:
          eot_threshold: 0.7
```

## Instance fields

| Field | Required | Notes |
|---|---|---|
| `provider` | yes | `livekit \| pipecat \| vapi \| deepgram` |
| `version` | code targets | framework pin; the driver checks it against the range its templates support |
| `pins` | no | independently versioned packages (for example LiveKit plugins) get their own entries |
| `sdk_language` | no | the LiveKit driver currently accepts `python` only |
| `connection` | telephony routes | name of one [`connections/<name>.yaml`](connections.md), which declares the transport, the carrier, and the credentials. All telephony channels on this v1 target share it. On a LiveKit SIP route its four SIP values reach the deployed agent's dial-out path directly: the agent passes them inline with each call, so no platform-side outbound trunk is registered (N33) |
| `deployment_region` | no | where the platform deploys the agent: one region, or a list of them (N32). Forwarded as declared, never validated. See below. |
| `models` | no | per-target overrides, keyed by model name, below |

`transport`, `carrier`, and `destinations` were target fields and are not any
more. The first two live in the connection; `destinations` lives at the top
level of [`agent.yaml`](agent-yaml.md), because who the agent escalates to is
the same desk whichever carrier reaches it. Writing any of the three on a target
fails with a message naming its new home.

Pipecat and LiveKit have drivers today; Vapi and Deepgram instances error on `compile` until their driver ships. `validate` still checks any provider against the schema. See the [target pages](../targets/pipecat.md).

## Deployment region

One region, or several. Both shapes are the same field, and a one-element list
behaves exactly like the scalar:

```yaml
targets:
  livekit:
    provider: livekit
    deployment_region: eu-central       # one region

  livekit_multi:
    provider: livekit
    deployment_region:                  # several: one deployment each
      - us-east
      - eu-central
```

- **A list of more than one is LiveKit only.** Each region becomes its own
  deployment from the one build directory, and the generated
  `build/<target>/README.md` prints one first-deploy and one redeploy command per
  region, each naming its own config file. Every deployment keeps the package's
  single dispatch name, so callers reach the nearest one.
- **On Pipecat a list of more than one fails validation**, because agent names
  are globally unique across regions there: a second region is a differently
  named agent. Declare one region, and the generated README prints the single
  extra command that puts a second agent in another region with its own secret
  set. Declaring two Pipecat instances, one per region, works too.
- **Unset is fine on both.** LiveKit's first deploy then asks which region to
  use, so set the field when the deploy has to be unattended. Pipecat uses your
  organisation's default region (`us-west` unless you changed it).
- **Region codes are never validated** and this repository keeps no list of
  them: the value is forwarded exactly as written and the platform CLI rejects a
  wrong one. The same region twice in one list is an error rather than being
  quietly deduplicated.
- **Fixed once deployed.** On LiveKit a region cannot be moved: creating in the
  new region and deleting the old agent is the procedure, and the generated
  README says so. Nothing is promised about moving a Pipecat agent's region,
  because the platform documents nothing either way.
- A model's own service region is a different knob and rides its
  `params`/`endpoint_env` instead. An agent deployed in one region may pin a
  model endpoint in another.

Every declared region appears in `build/<target>/compile-report.json` as
`deployment_regions`, so what was sent can be read back without opening the
source package.

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
    connection: twilio_api          # transport: carrier-websocket, carrier: twilio

  livekit_telnyx:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    connection: telnyx_sip          # transport: sip, carrier: telnyx

  # Pipecat's Daily route with your own carrier: the carrier forwards the call
  # over SIP into the Daily room. The connection declares both halves, and this
  # route also needs a `channels.phone` entry (SCHEMA N37).
  pipecat_daily_twilio:
    provider: pipecat
    version: "1.5.0"
    connection: twilio_sip_daily    # transport: daily-sip, carrier: twilio
```

Add another target and Connection for every additional supported carrier route.
There is no package-level route-count setting and no multi-carrier target: one
target always emits one adapter. Two targets on different transports need one
connection file each, even when they share a carrier account.

`transport: daily-sip` is the one transport with two shapes. With no `carrier` it
means a Daily-provisioned number, which carries its own calls, dials out only,
and so takes no phone channel. With a carrier it means your own number, and then
the channel goes with it.
Its Connection keys are `account_sid`, `auth_token`, `sip_address`, `from_number`,
and it **rejects** `sip_username` and `sip_password` with the route named, because
Daily's outbound SIP carries no credential on any documented surface and your trunk
authenticates Daily by IP address list instead.

A Connection stores environment variable **names**, never values, and the names
are yours to choose: the compiler carries whatever you write through verbatim and
knows none of them. That is what lets the same emitted LiveKit SIP code dial
through any SIP carrier, and why the shipped examples use plain
`SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD` and
`SIP_FROM_NUMBER` rather than carrier-prefixed ones (N33). A package written with
carrier-prefixed names keeps working with no edit. See the
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
