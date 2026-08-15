# Contract: the exact strings

For a CLI, the message **is** the interface. These are the strings the tests
assert on. Every existing string quoted here was captured from a real run during
Wave A, not transcribed from memory.

Style rules taken from the strings already in the tree: lower case start, no
trailing period, name the thing in double quotes, and where a fix exists, say
what it is rather than only what is wrong.

---

## New: a declaration nothing reaches

Tier 1, so the message carries file and line. Formatted beside the existing
`missing()` helper in `internal/ir/build.go`.

```
agent.yaml:47: control "reach_a_person" is declared but no agent reaches it; add it to the tools: of one of these agents: front_desk, billing
```

```
agent.yaml:61: destination "front_desk_line" is declared but no control resolves to it
```

```
agent.yaml:12: tool "lookup_hours" is declared but no agent reaches it; add it to the tools: of one of these agents: front_desk
```

The agent list is present when there is at least one candidate and omitted when
there is none, so the message never suggests an impossible fix. For an
unreachable agent the wording names the entry agent instead:

```
agent.yaml:33: agent "specialist" is declared but the entry agent "front_desk" cannot reach it, directly or through an agent_transfer
```

**Existing sibling to match**, `internal/ir/build.go`:

```
agent.yaml:51: tool or control "reach_a_person" does not resolve
```

---

## New: cold transfer with no route

Tier 2, so target-prefixed with no position. Written as the sibling of the warm
message that already exists ten lines away.

```
livekit: cold transfer needs a telephony Connection: it hands the caller's own phone leg to the destination, and a session that did not arrive by phone has no leg to hand over
```

**The existing warm message it mirrors**, unchanged,
`internal/ir/validate.go:1173`:

```
livekit: warm transfer needs a telephony Connection: it dials the destination itself, using the connection's sip_address, sip_username, sip_password and from_number
```

**Pipecat keeps its own words.** The `len(row.Errors) == before` guard is
retained, so on Pipecat the table row still produces, alone:

```
pipecat: Pipecat cold transfer requires Daily SIP transport
```

That is Principle II's "quote that provider's own vocabulary", and the new
generic message must never append on top of it. A test asserts the Pipecat
output is exactly one error.

---

## Changed: the secrets completeness warning

The string itself does not change. What changes is that it fires when no
`secrets:` block is declared, and that provider key names are in the set it
compares against.

```
livekit: environment variables referenced but not declared in secrets: BILLING_PHONE_NUMBER (agent.yaml destinations billing_line), SIP_AUTH_USERNAME (connections/twilio_sip.yaml environment sip_username), SIP_TRUNK_HOSTNAME (connections/twilio_sip.yaml environment sip_address)
```

Still stderr, still exit 0, per `docs/SCHEMA.md` N24.

New site strings for the provider keys added to the reference set, following the
existing `<file> <path>` convention:

```
OPENAI_API_KEY (agent.yaml models think assistant_model)
SLNG_API_KEY (agent.yaml models speak assistant_voice)
```

---

## Moved: the eight value checks

Each keeps its wording and gains a target prefix, because tier 2 prints
`<target>: <message>`. The generator keeps its own error as a backstop; only the
fact it checks against moves.

| Field | Message |
|---|---|
| `models.turn.<n>.model` | `livekit turn model "silero" is not recognized; use turn-detector-mini (local) or turn-detector (LiveKit Cloud)` |
| `targets.<n>.sdk_language` | `livekit driver emits python projects only; sdk_language "node" has no templates yet` |
| `pins` key | `livekit pin "livekit-plugins-banana" is not a pinnable package; known: livekit-plugins-silero, livekit-plugins-slng` |
| `pins` value | `livekit pin livekit-plugins-silero: "banana" is not a semantic version` |
| `pins` value | `livekit pin livekit-plugins-silero "0.0.1" is below the catalogue floor >=1.6.1` |
| `targets.<n>.version` | `livekit version "banana" is not a semantic version` |
| `targets.<n>.version` | `livekit version "9.9.9" is outside the driver's template-compatible range (>=1.5, <2.0)` |
| speak `voice` | `entry agent "appointment_desk": livekit speak binding provider "deepgram": voice has no slot here` |

---

## Unchanged by design

These already say the right thing and are quoted so nobody "improves" them:

```
livekit: LiveKit turn placement is a preference
```

```
connections/twilio_voice.yaml:21: "banana_key" is not accepted by route (pipecat, cloud-websocket, twilio); it accepts account_sid, auth_token, from_number
```

```
agent.yaml:51: control "send_to_billing": destination "nowhere_line" is missing from target "livekit"
```

---

## Generated output: the supplied-for-you block

`build/<target>/.env.example` today, for an outbound telephony package:

```
# required by the target or a connection
REDIS_URL=
UNMUTE_OUTBOUND_TOKEN=
UNMUTE_PUBLIC_URL=
```

Measured on `examples/livekit-human-transfer`, today, twelve names:

```
# Environment for livekit (generated by unmute).
# Copy this file to `.env` and fill in the values. Never commit `.env`.

BILLING_PHONE_NUMBER=
OPENAI_API_KEY=
SIP_AUTH_PASSWORD=
SIP_AUTH_USERNAME=
SIP_FROM_NUMBER=
SIP_TRUNK_HOSTNAME=
SLNG_API_KEY=
SUPERVISOR_PHONE_NUMBER=

# Supplied for you, not by you. On LiveKit Cloud the platform injects the
# LIVEKIT_* connection values into the deployed agent and drops them from any
# secrets file, and its managed SIP service owns Redis, which this agent never
# reads. Set these only for a local run or a self-hosted deployment.
LIVEKIT_API_KEY=
LIVEKIT_API_SECRET=
LIVEKIT_URL=
REDIS_URL=
```

After: eight names, and the block is gone. Not relabelled, not commented out,
absent. The file is a to-do list and every line of it is a to-do.

```
# Environment for livekit (generated by unmute).
# Copy this file to `.env` and fill in the values. Never commit `.env`.

BILLING_PHONE_NUMBER=
OPENAI_API_KEY=
SIP_AUTH_PASSWORD=
SIP_AUTH_USERNAME=
SIP_FROM_NUMBER=
SIP_TRUNK_HOSTNAME=
SLNG_API_KEY=
SUPERVISOR_PHONE_NUMBER=
```

The Pipecat file loses `REDIS_URL` the same way, and both lose
`UNMUTE_PUBLIC_URL` and `UNMUTE_OUTBOUND_TOKEN` on outbound routes.

The emitted README's "Set these before running (values are never written into
the project)" list renders from the same set, so the two cannot drift.

**Where the hidden names go instead.** Nowhere in any env file. The emitted
README's carrier setup already carries better text than a blank line, because it
says where each value comes from:

```
get LIVEKIT_URL and the API key pair from the LiveKit Cloud project settings,
or from a self-hosted LiveKit Server configuration; a self-hosted deployment
configures LiveKit Server and LiveKit SIP with the same Redis deployment
```

That is `route.ManualSteps` in `internal/target/telephony.go`, already emitted
today. `required_env` in `compile-report.json` stays complete as the
machine-readable form. Hiding is not deleting.

### The startup check, when the missing name was never shown

Hiding a name from `.env.example` does not remove it from `REQUIRED_ENV`, so the
message has to carry what the file no longer does. Two shapes, chosen by whether
the name is in `LocalEnvironment`.

Author-set name, unchanged from today:

```
Missing required environment variable: OPENAI_API_KEY
```

Locally-supplied name, new:

```
Missing required environment variable: REDIS_URL
This one is supplied for you: `unmute dev` sets it locally, and your platform
or operator sets it at deploy time. See "Carrier setup" in README.md.
```

Pipecat is the case that forces this. `templates/pipecat_v1/telephony_state.py.tmpl:36`
already raises `RuntimeError("Missing required environment variable: REDIS_URL")`,
the Compose graph supplies the value at `compose.telephony.yaml.tmpl:8`, and a
hand-written `.env` copied from `.env.example` does not. Dropping the name from
`REQUIRED_ENV` instead would move the failure from container start to the moment
a call arrives, which Principle II names as the worst trade available.

### The classification is already in the code

`internal/target/telephony.go` carries `LocallySuppliedEnvironment` per route,
cloned into the IR as `LocalEnvironment`. It is already right about four names
and missing two.

| Route | `LocallySuppliedEnvironment` today | Should also hold |
|---|---|---|
| pipecat carrier-websocket (`:122`) | `REDIS_URL` | `UNMUTE_PUBLIC_URL`, `UNMUTE_OUTBOUND_TOKEN` |
| livekit sip (`:194`) | `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `LIVEKIT_URL`, `REDIS_URL` | unchanged |
| livekit connector (`:385`) | `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `LIVEKIT_URL` | `UNMUTE_PUBLIC_URL`, `UNMUTE_OUTBOUND_TOKEN` |

Both env templates then **exclude** that set. Today only the LiveKit template
reads it, and it labels rather than excludes; the Pipecat template ignores it
entirely, which is why the same `REDIS_URL` is explained on one target and
silently demanded on the other.

### The naming rule as a table

| Today | Becomes | Owner |
|---|---|---|
| `UNMUTE_DAILY_ROOM_GEO` | `DAILY_ROOM_GEO` | Daily |
| `UNMUTE_HOLD_AUDIO_URL` | `DAILY_HOLD_AUDIO_URL` | Daily |
| `UNMUTE_LIVEKIT_PORT` | `LIVEKIT_HOST_PORT` | LiveKit |
| `UNMUTE_LIVEKIT_SIP_PORT` | `LIVEKIT_SIP_HOST_PORT` | LiveKit |
| `UNMUTE_LIVEKIT_RTP_PORT_RANGE` | `LIVEKIT_RTP_HOST_PORT_RANGE` | LiveKit |
| `UNMUTE_PUBLIC_URL`, `UNMUTE_OUTBOUND_TOKEN`, `UNMUTE_LOG_LEVEL`, `UNMUTE_AGENT_HEALTH_PORT`, `UNMUTE_TELEPHONY_BRIDGE_PORT`, `UNMUTE_DEV_PORT`, `UNMUTE_TELEPHONY_PORT`, `UNMUTE_CALL_START` | unchanged | the generated agent itself, no vendor |
| `UNMUTE_SIP_TRUNK_ID` | not an env var; stops being shaped like one | a `sed` token in one JSON file |

`HOST` stays in the three LiveKit names because they are Docker Compose
host-side mappings, not configuration the LiveKit server reads inside its
container.

`docs/TELEPHONY.md` drops "**Generate this secret yourself** with a
cryptographically secure password generator" for the outbound token, since
`unmute dev` mints it at `internal/cli/dev_telephony.go:87-92`.

---

## Scaffold output

The greeting and the prompt stop describing a phone call. The exact new text is
written with the `voice-agent-prompting` skill against
`internal/skill/assets/references/prompting.md`, so it is not fixed here; the
contract is the two properties a test can hold:

- neither the greeting nor `instructions.md` contains "call", "calling",
  "caller", or "phone" while the package's only channel is `web`;
- the package-root `.env.example` and `build/<target>/.env.example` name exactly
  the same set.

### The think model, exactly

One shape, everywhere: the scaffold, all eleven examples, every `docs/` and
`docs-site/` page, the root `README.md`, and the skill bundle.

```yaml
models:
  think:
    assistant_model:
      description: default reasoning model
      provider: openai
      model: gpt-5.6-luna
      params:
        reasoning_effort: minimal
```

Three things a test must reject:

| Rejected | Why |
|---|---|
| `model: gpt-4o-mini` or `model: gpt-4.1-mini` | stale, and the whole point is one identifier |
| `model: openai/gpt-5.6-luna` | `docs/SCHEMA.md` N15 forbids folding the provider into the model string; the value is forwarded to the SDK verbatim and would fail there |
| `temperature:` on a think model | OpenAI does not state that this model accepts it, and an unverified parameter fails on a live call |

### The scaffold's own env instruction

One string that is simply wrong today, in
`internal/scaffold/templates/env.example.tmpl:2`:

```
# ... then run `unmute dev hello-agent`
```

The file lives inside `hello-agent/`, so following it resolves to
`hello-agent/hello-agent` and fails. It becomes:

```
# ... then run `unmute dev .` from this directory
```
