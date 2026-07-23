# Configure LiveKit in YAML

LiveKit is a code target with a complete Python driver. This guide covers its
YAML surface: models, target infrastructure, fallback chains, tools, tasks,
context shaping, conversation controls, and telephony.

## Start with the package boundary

Portable behavior and model definitions stay in `agent.yaml` and
`tools/*.yaml`. LiveKit-specific versions, pins, transports, destinations, and
optional model overrides stay in `targets.yaml`.

```text
your-agent/
├── agent.yaml
├── targets.yaml
├── tools/
│   ├── lookup_order.yaml
│   └── search_knowledge.yaml
├── instructions.md
├── agents/
└── tasks/
```

This boundary lets you add or replace a target without rewriting the agents,
tasks, controls, or conversation outcomes.

## Define models

Define every model concretely in `agent.yaml`'s kind sections; a LiveKit target
then carries infrastructure and any by-name overrides.

```yaml
# agent.yaml
models:
  think:
    primary_reasoning:
      description: Main conversation model
      provider: openai
      model: gpt-4o-mini
      temperature: 0.4
      fallback: [backup_reasoning]
    backup_reasoning:
      description: Backup conversation model
      provider: openai
      model: gpt-4o
  speak:
    front_desk:
      description: Warm and concise
      provider: elevenlabs
      voice: cgSgspJ2msm6clMCkdW9
    specialist:
      description: Calm and deliberate
      provider: cartesia
      model: sonic-3
      voice: f786b574-daa5-4673-aa0c-cbe3e8534c02
  listen:
    transcriber: { provider: slng, model: "slng/deepgram/nova:3-en" }
  turn:
    detector: { provider: livekit, model: turn-detector-mini }

agents:
  assistant:
    instructions: instructions.md
    model: primary_reasoning
    voice: front_desk
```

The fallback list is ordered. On LiveKit, every model in the chain must use the
same placement (`fallback` is a think-model field).

```yaml
# targets.yaml
targets:
  livekit:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    pins:
      livekit-plugins-slng: "1.7.0"
```

Every model comes straight from `agent.yaml`; add an override under this
instance's `models:` (keyed by model name) only if LiveKit needs a different
one. This table highlights the common scaffold routes. The
[providers reference](../reference/providers.md#livekit) contains the complete
catalogue.

| Role | `provider` | Required environment |
|---|---|---|
| `listen` | `deepgram` | `DEEPGRAM_API_KEY` |
| `listen` | `slng` | `SLNG_API_KEY` |
| `speak` | `cartesia` | `CARTESIA_API_KEY` |
| `speak` | `elevenlabs` | `ELEVEN_API_KEY` |
| `speak` | `slng` | `SLNG_API_KEY` |
| `think` | `openai` | `OPENAI_API_KEY` |
| `think` | Any other LiveKit Inference provider, or `livekit` explicitly | LiveKit Cloud credentials |
| `turn` | `livekit` + `turn-detector-mini` | none (runs locally) |
| `turn` | `livekit` + `turn-detector` | LiveKit Cloud credentials |

SLNG remains the default route, not the only route. Its model value keeps the
`slng/<vendor>/<model>` form in YAML and passes to the plugin verbatim — the
`slng/` prefix names the SLNG-hosted route family and is part of the API path.

The selected turn model expresses intent rather than choosing an arbitrary
service: `turn-detector-mini` runs the local model with no cloud credentials,
`turn-detector` uses the LiveKit Cloud model. Its placement and
`semantic_endpointing` values remain preferences on LiveKit.

The driver accepts `livekit-agents` versions from `1.5` up to, but not
including, `2.0`. It currently accepts `sdk_language: python` only. A plugin
pin can raise a known package floor, but it cannot lower the catalogue floor
or name a package the target doesn't use.

## Attach tools to agents and tasks

Each tool is a YAML contract in `tools/`. Add its file name to the top-level
`tools` manifest, then list the same name on every agent or task that can call
it.

### Use a webhook tool

A webhook tool sends JSON to the URL stored in the named environment variable.
The YAML contains the variable name, never the URL or secret value.

```yaml
# tools/lookup_order.yaml
description: Look up an order by its reference number.

input:
  type: object
  properties:
    order_id: { type: string }
  required: [order_id]

output:
  type: object
  properties:
    status: { type: string }

execution: webhook
url_env: LOOKUP_ORDER_URL
interruption: provider_default
effect: returns_data
```

### Use a local tool

A local tool names a handler file in the package. The YAML still defines the
input, output, interruption policy, and conversation effect.

```yaml
# tools/load_account_notes.yaml
description: Load the caller's saved account notes.

input:
  type: object
  properties:
    topic: { type: string }
  required: [topic]

output:
  type: object
  properties:
    notes:
      type: array
      items: { type: string }

execution: local
handler: tools/load_account_notes.py
interruption: provider_default
effect: returns_data
```

### Use an MCP tool

An MCP tool names the environment variable that contains the MCP server
address. LiveKit exposes only the tool names assigned to the current agent.

```yaml
# tools/search_knowledge.yaml
description: Search the support knowledge base.

input:
  type: object
  properties:
    query: { type: string }
  required: [query]

execution: mcp
url_env: SUPPORT_MCP_URL
interruption: provider_default
effect: returns_data
```

Attach the declared tools in `agent.yaml`.

```yaml
agents:
  assistant:
    instructions: instructions.md
    model: primary_reasoning
    voice: front_desk
    tools: [lookup_order, load_account_notes, search_knowledge]

tools: [lookup_order, load_account_notes, search_knowledge]
```

LiveKit runs tool calls to completion. A non-default `interruption` value is
accepted with a warning because LiveKit cannot honor cancellation or
continuation as a per-tool preference. `effect: ends_conversation` ends the
session after a successful tool call.

## Delegate focused work

A task has its own instructions, tools, optional model, context, and typed
result. A delegate control makes that task available to an agent.

```yaml
# agent.yaml
models:
  think:
    careful_reasoning:
      description: Careful account verification
      provider: openai
      model: gpt-4o

variables:
  customer_id: { type: string }
  verified: { type: boolean, default: false }

tasks:
  verify_customer:
    instructions: tasks/verify_customer.md
    tools: [lookup_order]
    model: careful_reasoning
    result:
      customer_id: string
      verified: boolean
    context:
      history: messages

controls:
  run_verification:
    kind: delegate
    task: verify_customer
    when: Verify the caller before discussing an order.
    assign:
      customer_id: result.customer_id
      verified: result.verified
```

The task's `model:` names `careful_reasoning`, defined once in `agent.yaml`
above like any other think model — there is nothing extra to add in
`targets.yaml`. LiveKit gives a task with `model` its own think model; if you
omit the field, the task uses the entry agent's model.

LiveKit always gives the task its own instructions. When task history carries
over, the generated context excludes the owning agent's instructions. The
[context strategy map](../concepts/how-targets-run-your-agent.md#compare-context-strategies)
shows what `full`, `messages`, `last_n`, `summary`, and `reset` send to the
task.

LiveKit also accepts nested task results when every configured target is a code
target. Wrap the JSON Schema value in `schema`.

```yaml
tasks:
  collect_shipping_address:
    instructions: tasks/collect_shipping_address.md
    result:
      address:
        schema:
          type: object
          properties:
            city: { type: string }
            postal_code: { type: string }
          required: [city, postal_code]
    context:
      history: full
```

## Sequence tasks with a group

A task group runs named tasks in order. `context_scope` decides whether the
steps share conversation history, and `then` decides what happens afterward.

```yaml
task_groups:
  booking_flow:
    steps: [find_slot, confirm_booking]
    context_scope: shared
    then: return
    merge: results

  private_intake:
    steps: [collect_identity, collect_request]
    context_scope: isolated
    then: transfer
    then_target: specialist
    merge: results
```

On LiveKit, `shared` uses the native task-group flow. `isolated` runs the steps
as standalone tasks so each starts with a fresh context. In both cases,
`merge: results` returns typed results without appending the tasks' turns to
the owning agent's conversation.

`then` accepts `return`, `transfer`, or `end`. Any LiveKit task group currently
produces an experimental-feature warning.

## Shape agent handoffs

An agent transfer can guard the handoff, select conversation history, exclude
tool calls, and carry all or a subset of shared variables.

```yaml
models:
  think:
    summary_model:
      description: Compact handoff summaries
      provider: openai
      model: gpt-4o-mini

controls:
  to_specialist:
    kind: agent_transfer
    to: specialist
    when: The verified caller needs specialist help.
    requires: [verified]
    context:
      history: summary
      summarizer: summary_model
      include_tool_calls: false
      variables: [customer_id, verified]
```

LiveKit supports `full`, `messages`, `last_n`, `summary`, and `reset` history.
Use `max_messages` with `last_n`, and name a `summarizer` think model when you use
`summary`. A failed `requires` guard names the missing variables instead of
performing the transfer.

## Shape the conversation

Conversation YAML describes caller-visible outcomes rather than LiveKit
settings. LiveKit supports the full block below.

```yaml
conversation:
  greeting:
    speaks_first: agent
    text: "Hi, you have reached Acme Support. How can I help?"
  interruption:
    enabled: true
    minimum_words: 2
    ignore_phrases: [okay, right, uh-huh]
  inactivity:
    nudge_after: 15s
    end_after: 45s
  max_duration: 20m
  thinking_audio: subtle
```

Remove `greeting.text` to let the model write the opening, or set
`speaks_first: user` to wait for the caller. `thinking_audio` accepts `none` or
`subtle`. If you omit the entire `greeting` block, LiveKit uses its target
default and reports a warning because target defaults differ.

## Configure phone calls and human transfers

Telephony uses three parts of the YAML: a control in `agent.yaml`, a channel in
`agent.yaml`, and the symbolic destination mapping in `targets.yaml`.

```yaml
# agent.yaml
agents:
  assistant:
    instructions: instructions.md
    model: primary_reasoning
    voice: front_desk
    tools: [to_human]

controls:
  to_human:
    kind: human_transfer
    destination: support_line
    mode: warm
    briefing: summary

channels:
  phone:
    kind: telephony
    inbound: true
    outbound: true
    required_controls:
      - warm_transfer
      - voicemail_detection
      - hangup
    on_voicemail: leave_message
```

Declare one SIP Connection. The keys stay the same across supported SIP
carriers; only their environment-variable names and values change.

```yaml
# connections/primary_phone.yaml
kind: telephony
environment:
  sip_address: TWILIO_SIP_ADDRESS
  sip_username: TWILIO_SIP_USERNAME
  sip_password: TWILIO_SIP_PASSWORD
  from_number: TWILIO_PHONE_NUMBER
```

The carrier matrix makes those values explicit:

| Route | Carrier | Connection environment values | Generated integration | Status |
|---|---|---|---|---|
| `sip` | Twilio | `TWILIO_SIP_ADDRESS`, `TWILIO_SIP_USERNAME`, `TWILIO_SIP_PASSWORD`, `TWILIO_PHONE_NUMBER` | Self-hosted LiveKit SIP and Twilio trunk inputs | Offline-tested; provisional |
| `sip` | Telnyx | `TELNYX_SIP_ADDRESS`, `TELNYX_SIP_USERNAME`, `TELNYX_SIP_PASSWORD`, `TELNYX_PHONE_NUMBER` | Self-hosted LiveKit SIP and Telnyx trunk inputs | Offline-tested; provisional |
| `sip` | Plivo | `PLIVO_SIP_ADDRESS`, `PLIVO_SIP_USERNAME`, `PLIVO_SIP_PASSWORD`, `PLIVO_PHONE_NUMBER` | Self-hosted LiveKit SIP and Plivo trunk inputs | Offline-tested; provisional |
| `sip` | Exotel | Exotel SIP values | No emitted adapter | Gated pending provider-specific proof |
| `connector` | Twilio | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_PHONE_NUMBER` | Generated bridge into a self-hosted LiveKit room | Offline-tested; provisional |

Every SIP row runs now and is tagged provisional: validation and compilation
emit it cleanly, with no warning. The SIP emitter contains inbound, outbound,
voicemail, hangup, cold-transfer, and warm-transfer paths. The Twilio connector
also runs now and is provisional: its generated `telephony_bridge.py` speaks the
Twilio Media Streams protocol and bridges the call into a local, self-hosted
LiveKit room, with no LiveKit Cloud. It supports inbound, outbound, and hangup;
transfers and voicemail stay on the SIP route. Only the Exotel LiveKit route is
gated and fails closed.
Follow the
[SIP trunking guide](../learn/07-phone-calls.md#configure-telephony-by-orchestrator)
for the complete Twilio, Telnyx, and Plivo setup.

Bind the Connection and symbolic destinations to the exact route.

```yaml
# targets.yaml
targets:
  livekit:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    transport: sip
    carrier: twilio
    connection: primary_phone
    destinations:
      support_line: "+14155550123"
```

Bind a target to the self-hosted `sip` route and one telephony Connection. The
distinct Twilio `connector` route is a usable alternative that runs on a laptop;
it cannot inherit SIP transfer behavior.

To configure several carriers, declare several LiveKit targets, such as
`livekit_twilio` and `livekit_plivo`, and bind each to its own Connection. Each
compiles to a separate project and SIP setup, and each provisional route runs
cleanly, with no warning. See
[phone calls](../learn/07-phone-calls.md#configure-multiple-carriers) for a full
Pipecat and LiveKit example.

The emitter's `.env.example` names every deployment value and is written by
public compilation now. Get the LiveKit API key pair from the self-hosted
server's `keys` configuration, set `LIVEKIT_URL` to that server, and set
`REDIS_URL` to the Redis deployment shared by LiveKit Server and LiveKit SIP.
For inbound calls, point the carrier's origination URI at your public LiveKit
SIP endpoint. Local `unmute dev --telephony` supplies its own clearly
non-production LiveKit key pair and Redis connection instead.

For Twilio, get the SIP address, Credential List username and password, and
associated number from **Elastic SIP Trunking** in the Twilio Console. For
Telnyx, use the SIP connection address, credentials, and assigned number from
Telnyx Mission Control. For Plivo, use the Zentrunk termination domain,
outbound credential, and linked number from the Plivo Console.

Compilation emits the selected `sip-inbound-trunk.json`,
`sip-outbound-trunk.json`, and `sip-dispatch-rule.json` inputs for production
deployments. Materialize their
environment placeholders with `envsubst`, then run the `lk sip ... create`
commands in the generated README. Copy the returned IDs to
`LIVEKIT_SIP_INBOUND_TRUNK` and `LIVEKIT_SIP_OUTBOUND_TRUNK` as requested by
`.env.example`. For local development none of this is needed:
`unmute dev --telephony` creates the same records on the local server itself
and supplies both IDs.

Self-hosted SIP runs LiveKit Server and LiveKit SIP against the same Redis.
Redis is their shared datastore and message bus, so calls, SIP participants,
and Agent dispatch remain coherent when either service has multiple replicas.
It is not an audio buffer. Audio flows through SIP/RTP and LiveKit's media
services. Pipecat's media and conversation remain in one long-lived worker too,
but its generated telephony application uses Redis for correlation, callback
idempotency, transfer locks, and admission. LiveKit's generated Agent does not
use Redis; LiveKit Server and LiveKit SIP are the consumers.

An HTTPS development tunnel isn't enough for LiveKit SIP. The carrier must
reach SIP signaling and RTP directly; the local defaults are SIP port `5060`
and UDP RTP ports `10000-10100`. Production must configure a range sized for
its traffic. The generated worker itself exposes LiveKit's `/` health endpoint,
which returns success only while the worker is connected and operating
normally.

This is the local SIP command:

```sh
unmute dev acme --target livekit --telephony
```

The command runs the whole bootstrap itself: Docker Compose builds the Agent and
starts Redis, LiveKit Server, and LiveKit SIP first; the command then creates or
reuses the inbound trunk, outbound trunk, and `call-` dispatch rule on that
local server with the generated development key pair, injects the returned
`LIVEKIT_SIP_INBOUND_TRUNK` and `LIVEKIT_SIP_OUTBOUND_TRUNK`, and starts the
application. Record creation is idempotent: the named Redis volume persists,
so restarts reuse existing records instead of duplicating them.

Non-empty external `LIVEKIT_URL`, API key/secret, `REDIS_URL`, or trunk ID
values conflict with this local graph and are rejected. `--verbose` follows
Compose logs; normal output is retained in `build/livekit/telephony.log`.
Stopping preserves the named Redis volume. Remember that a real call still
needs carrier-reachable SIP signaling and RTP; the local stack runs, but an
HTTPS tunnel cannot carry the media path.

### Use the Twilio connector route

The `connector` transport is the laptop-friendly LiveKit route. It is
Twilio-only and uses the same three Twilio credentials as the Pipecat carrier
route: `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, and `TWILIO_PHONE_NUMBER`. No
SIP trunk and no Redis.

```yaml
# connections/twilio_connector.yaml
kind: telephony
environment:
  account_sid: TWILIO_ACCOUNT_SID
  auth_token: TWILIO_AUTH_TOKEN
  from_number: TWILIO_PHONE_NUMBER
```

```yaml
# targets.yaml
targets:
  livekit_connector:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    transport: connector
    carrier: twilio
    connection: twilio_connector
```

The generated `telephony_bridge.py` speaks the Twilio Media Streams protocol and
bridges the call into a local, self-hosted LiveKit room, where the agent worker
handles it. There is no LiveKit Cloud. Local Compose runs the application
container and a local `livekit-server --dev`; `LIVEKIT_URL`, `LIVEKIT_API_KEY`,
and `LIVEKIT_API_SECRET` come from that pair (`devkey`/`secret`) locally, or from
your self-hosted server in production.

Run it like the Pipecat route:

```sh
unmute dev acme --target livekit_connector --telephony --to +15551234567
```

`unmute dev --telephony` starts a managed cloudflared tunnel, sets the Twilio
voice webhook automatically, and places an outbound call with `--to`. Twilio
reaches the bridge over HTTPS and WSS, so both inbound and outbound work fully on
a laptop. The route supports inbound, outbound, and hangup; call transfers and
voicemail detection stay on the SIP route.

## Run it and talk to the agent

Like Pipecat, a LiveKit target runs locally with `unmute dev` — two ways:

```sh
unmute dev acme --console   # talk in the terminal, over your mic and speaker
unmute dev acme             # talk in the browser
```

**Console** runs `uv run agent.py console` entirely on your machine. A
scaffold-default agent (native providers + local turn detection) needs **no
LiveKit credentials** — it never connects to LiveKit Cloud. It asks for
`LIVEKIT_API_KEY`/`LIVEKIT_API_SECRET` only if a model routes through LiveKit
Inference (a think model with `provider: livekit`, or the cloud
`turn-detector`); the preflight names what it needs.

**Web (the default)** runs everything in containers and needs Docker (Docker
Desktop or Docker Engine with the Compose plugin). `unmute dev` builds the same
worker image you deploy and, from the emitted `compose.dev.yaml`, starts a
single-node `livekit-server --dev` container next to it, then serves the WebRTC
web client. There is no LiveKit install, no cloud account, and no credentials:
the dev server uses LiveKit's `--dev` key pair, the worker joins it over the
Compose network, and the browser reaches it on the published ports (`7880` for
signalling, `7881` for the TCP fallback, `7882/udp` for the UDP mux). The page
mints a token and joins a fresh room; your agent is dispatched to that room
automatically. To point at LiveKit Cloud or your own deployment instead, deploy
the built image yourself; local dev always runs the containerized dev server.

Both modes read shared keys from the current directory's `.env`, then apply
package-root `.env` overrides. Press `ctrl-c` to stop and the stack comes down
(the data volume is kept). See the
[dev command reference](../reference/cli.md#dev) for all flags.

## Know the LiveKit boundaries

The driver covers every LiveKit capability that passes target validation. The
remaining boundaries are explicit YAML choices, not silent omissions.

| YAML choice | LiveKit behavior |
|---|---|
| `sdk_language: python` | Supported and currently required |
| `models.*.fallback` | Native ordered fallback |
| Per-task `model` | Uses the task-specific think model |
| Nested task result | Supported when every target is a code target |
| `context_scope: shared` or `isolated` | Both supported |
| All five handoff history modes | Supported |
| Handoff `requires` and variable subsets | Supported |
| Webhook, local, and MCP tools | Supported |
| Non-default tool `interruption` | Warns; tools run to completion |
| Conversation shaping block | Supported |
| New `sip` telephony route | Provisional pending credentialed route smokes |
| Twilio `connector` route | Provisional; generated bridge into a self-hosted LiveKit room, inbound/outbound/hangup, never inherits SIP capabilities |
| A `provider: local` model (listen, speak, or think) | Supported |
| `speak.endpoint_env` | Rejected; no LiveKit integration slot |
| Warm `briefing: message` or `wait` | Rejected; use `summary` |

## Next steps

Use the focused reference pages when you need the full value set or want to
compare LiveKit with another target.

- [Targets YAML](../reference/targets-yaml.md) defines target instances and
  model override rules.
- [Providers](../reference/providers.md) lists the accepted LiveKit provider
  names and required environment variables.
- [Tools](../reference/tools.md), [tasks](../reference/tasks.md), and
  [controls](../reference/controls.md) define their complete YAML fields.
- [Conversation](../reference/conversation.md) and
  [channels and capacity](../reference/channels-and-capacity.md) define the
  call-lifecycle fields.
