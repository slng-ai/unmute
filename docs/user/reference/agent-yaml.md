# Reference: agent.yaml

`agent.yaml` is the portable description of your agent. It never contains provider settings; those live in [targets.yaml](targets-yaml.md). This page is the top-level map. Each block links to its own reference page.

## How to read these reference pages

Every field lists its **Required** status, allowed **Values**, and **Default**. Where a field behaves differently across the five targets (LiveKit, Pipecat, Vapi, ElevenLabs, Deepgram), a table shows what happens on each and its [tag](../concepts/tags-and-gating.md). Where a field is plain `core` on all five, that is stated in one line instead of five identical rows. Either way, the per-target outcome is always given. Rows come from `SCHEMA.md`; the reference never invents one.

Where a note says "the Pipecat driver does not emit this yet", that is a driver maturity gate: it fails validation today and lifts when the driver ships it. See the [Pipecat target page](../targets/pipecat.md).

## Package layout

```text
agent.yaml            # this file
instructions.md       # the entry agent's prompt
agents/               # one .md per additional agent (T2)
tasks/                # one .md per task (T1)
tools/
  lookup_customer.yaml  # tool contract: input, output, execution, interruption, effect
  lookup_customer.py    # handler, code targets only (local execution)
targets.yaml          # named target instances
```

Rules that hold across the whole package:

- Secrets never appear in any file. `targets.yaml` carries environment variable names and secret references only, never values.
- Remote ids, regions, editions, SDK language, version pins, and carriers live in `targets.yaml`. Model definitions live in `agent.yaml`; a target only overrides one it cannot run.
- Machine sizes, replica counts, and GPU counts appear in neither file. They are derived, not stored.
- All names (agents, tasks, groups, tools, controls, models, variables, and
  destinations) are lowercase `snake_case`. A name starting with an underscore
  is reserved by providers and rejected.
- Durations use Go syntax: `90s`, `15m`, `1h30m`.

## Top-level fields

| Field | Required | Type | Tag |
|---|---|---|---|
| `version` | yes | int, must be `1` | core |
| `language` | no, defaults to `en` | BCP-47 tag such as `en` or `es-MX` | gated |
| `entry_agent` | yes | name of an agent | core |
| `models` | yes | four kind sections (`think`/`speak`/`listen`/`turn`) | core |
| `listen` | only when `models.listen` has 2+ entries | name of a listen model | gated |
| `turn` | only when `models.turn` has 2+ entries | name of a turn model | warn |
| `variables` | no | map | core |
| `agents` | yes, must include `entry_agent` | map | core |
| `tasks` | no | map | gated (T1) |
| `task_groups` | no | map | gated (T1) |
| `controls` | no | map | per kind |
| `tools` | no | list of tool names | core |
| `conversation` | no | block | mixed |
| `channels` | yes, at least one | map | core |
| `capacity` | conditional | block | core |

### version

The spec format version. Always `1`.

Required: yes. Values: the integer `1`. Default: none. Targets: all five, core.

### language

The primary spoken language for STT and TTS. Pipecat and LiveKit lower it
through the selected catalogue integrations; ElevenLabs uses its unified agent
language. A model's `language` or `params.language` overrides it for that
model.

Required: no. Values: a BCP-47 tag. Default: `en`. Targets: shipped generators; Vapi and Deepgram stay unavailable until their generators ship.

### entry_agent

The name of the agent that answers first. Must be a key in the `agents` map.

Required: yes. Values: an agent name. Default: none. Targets: all five, core.

### The blocks

Each block has its own reference page with every field:

- **`models`**: the central palette, every model defined once under its kind section. See [models](models-and-voices.md).
- **`listen`, `turn`**: name selectors into their sections; a sole entry selects itself. See [listen, turn, and placement](pipeline.md).
- **`variables`**: typed shared state. See [variables](variables.md).
- **`agents`**: prompt, model, voice, tools per agent. See [agents](agents.md).
- **`tasks`, `task_groups`**: delegate-and-return work (T1). See [tasks](tasks.md).
- **`controls`**: transfers and delegates the model can invoke. See [controls](controls.md).
- **`tools`**: the top-level load manifest (which tool files compile in). Each file is defined in [tools/*.yaml](tools.md). Which agent can call a tool is set by that agent's own `tools:` list, not here.
- **`conversation`**: greeting, interruption, inactivity, duration, thinking audio. See [conversation](conversation.md).
- **`channels`**: how callers reach the agent. See [channels and capacity](channels-and-capacity.md).
- **`capacity`**: declared traffic, required on code targets and telephony. See [channels and capacity](channels-and-capacity.md).

The `safe core`, the subset that passes on all five targets, is collected on its own page: [safe core](safe-core.md).
