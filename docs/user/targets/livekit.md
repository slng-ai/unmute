# Configure LiveKit in YAML

LiveKit is a code target with a complete Python driver. This guide covers its
YAML surface: provider bindings, fallback models, tools, tasks, context
shaping, conversation controls, and telephony.

## Start with the package boundary

Portable behavior stays in `agent.yaml` and `tools/*.yaml`. LiveKit-specific
model bindings, versions, pins, and destinations stay in `targets.yaml`.

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

## Bind models and voices

Declare profiles by purpose in `agent.yaml`, then bind every used profile to a
LiveKit integration in `targets.yaml`.

```yaml
# agent.yaml
models:
  primary_reasoning:
    description: Main conversation model
    placement: api
    fallback: [backup_reasoning]
  backup_reasoning:
    description: Backup conversation model
    placement: api

voices:
  front_desk:
    description: Warm and concise
  specialist:
    description: Calm and deliberate

agents:
  assistant:
    instructions: instructions.md
    model: primary_reasoning
    voice: front_desk
```

The fallback list is ordered. On LiveKit, every profile in the chain must have
a `reason` binding and use the same placement.

```yaml
# targets.yaml
targets:
  livekit-dev:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    pins:
      livekit-plugins-slng: "1.7.0"
    models:
      listen:
        provider: slng
        model: "slng/deepgram/nova:3-en"
      turn:
        provider: livekit
        model: turn-detector-mini
      speak:
        front_desk:
          provider: elevenlabs
          voice: cgSgspJ2msm6clMCkdW9
        specialist:
          provider: cartesia
          model: sonic-3
          voice: f786b574-daa5-4673-aa0c-cbe3e8534c02
      reason:
        primary_reasoning:
          provider: openai
          model: gpt-4o-mini
          params: { temperature: 0.4 }
        backup_reasoning:
          provider: openai
          model: gpt-4o
```

LiveKit accepts the following provider choices through its provider catalogue.

| Role | `provider` | Required environment |
|---|---|---|
| `listen` | `deepgram` | `DEEPGRAM_API_KEY` |
| `listen` | `slng` | `SLNG_API_KEY` |
| `speak` | `cartesia` | `CARTESIA_API_KEY` |
| `speak` | `elevenlabs` | `ELEVEN_API_KEY` |
| `speak` | `slng` | `SLNG_API_KEY` |
| `reason` | `openai` | `OPENAI_API_KEY` |
| `reason` | Any other LiveKit Inference provider, or `livekit` explicitly | LiveKit Cloud credentials |
| `turn` | `livekit` + `turn-detector-mini` | none (runs locally) |
| `turn` | `livekit` + `turn-detector` | LiveKit Cloud credentials |

SLNG remains the default route, not the only route. Its model value keeps the
`slng/<vendor>/<model>` form in YAML and passes to the plugin verbatim — the
`slng/` prefix names the SLNG-hosted route family and is part of the API path.

The `turn` binding expresses intent rather than selecting a replaceable
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
  careful_reasoning:
    description: Careful account verification
    placement: api

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

Bind the task model under the same profile name in `targets.yaml`.

```yaml
targets:
  livekit-dev:
    models:
      reason:
        careful_reasoning:
          provider: openai
          model: gpt-4o
```

LiveKit gives a task with `model` its own reasoning binding. If you omit the
field, the task uses the entry agent's model.

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
  summary_model:
    description: Compact handoff summaries
    placement: api

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
Use `max_messages` with `last_n`, and bind the `summarizer` profile when you use
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

Resolve the symbolic destination for each LiveKit target.

```yaml
# targets.yaml
targets:
  livekit-dev:
    destinations:
      support_line: "+14155550123"
```

Use `mode: cold` for a direct transfer. LiveKit warm transfer accepts
`briefing: summary`; `message` and `wait` don't have faithful LiveKit mappings.
Warm transfer is Beta in the Python SDK. Outbound calls support
`on_voicemail: hangup` and `on_voicemail: leave_message`, and require the
LiveKit outbound SIP trunk environment setting.

## Run it and talk to the agent

Like Pipecat, a LiveKit target runs locally with `unmute dev` — two ways:

```sh
unmute dev acme --console   # talk in the terminal, over your mic and speaker
unmute dev acme             # talk in the browser
```

**Console** runs `uv run agent.py console` entirely on your machine. A
scaffold-default agent (native providers + local turn detection) needs **no
LiveKit credentials** — it never connects to LiveKit Cloud. It asks for
`LIVEKIT_API_KEY`/`LIVEKIT_API_SECRET` only if a binding routes through LiveKit
Inference (a `provider: livekit` reason, or the cloud `turn-detector`); the
preflight names what it needs.

**Web** needs a LiveKit server: set `LIVEKIT_URL`, `LIVEKIT_API_KEY`, and
`LIVEKIT_API_SECRET` (LiveKit Cloud or self-hosted) in a `.env` at the package
root. `unmute dev` runs `uv run agent.py dev`, waits for the worker to register,
then opens a browser client that joins a fresh room; your agent is dispatched to
that room automatically. With no creds, it fails with a message pointing you at
`--console`.

Both read keys from `.env`; press `ctrl-c` to stop. See the
[dev command reference](../reference/cli.md#dev) for all flags.

## Know the LiveKit boundaries

The driver covers every LiveKit capability that passes target validation. The
remaining boundaries are explicit YAML choices, not silent omissions.

| YAML choice | LiveKit behavior |
|---|---|
| `sdk_language: python` | Supported and currently required |
| `models.*.fallback` | Native ordered fallback |
| Per-task `model` | Uses the task-specific reason binding |
| Nested task result | Supported when every target is a code target |
| `context_scope: shared` or `isolated` | Both supported |
| All five handoff history modes | Supported |
| Handoff `requires` and variable subsets | Supported |
| Webhook, local, and MCP tools | Supported |
| Non-default tool `interruption` | Warns; tools run to completion |
| Conversation shaping block | Supported |
| Cold and warm human transfer | Supported; warm summary is Beta |
| Outbound calls and voicemail | Supported |
| Pipeline or reasoning `placement: local` | Supported |
| `speak.endpoint_env` | Rejected; no LiveKit integration slot |
| Warm `briefing: message` or `wait` | Rejected; use `summary` |

## Next steps

Use the focused reference pages when you need the full value set or want to
compare LiveKit with another target.

- [Targets YAML](../reference/targets-yaml.md) defines target instances and
  binding rules.
- [Providers](../reference/providers.md) lists the accepted LiveKit provider
  names and required environment variables.
- [Tools](../reference/tools.md), [tasks](../reference/tasks.md), and
  [controls](../reference/controls.md) define their complete YAML fields.
- [Conversation](../reference/conversation.md) and
  [channels and capacity](../reference/channels-and-capacity.md) define the
  call-lifecycle fields.
