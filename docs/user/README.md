# Unmute documentation

Start with the YAML package, understand how its files relate, and then add
features one at a time. The authoring path in these docs doesn't require you to
learn the command-line interface or the generated runtime first.

## Follow the YAML-first path

The shortest path starts with one agent and expands the same files as your
agent grows.

1. [Build your first YAML package](start/first-agent.md) to learn the package
   boundary and the difference between portable models and target overrides.
2. Follow the [learn series](learn/01-one-agent.md) to add tools, variables,
   agents, tasks, task groups, and phone calls.
3. Choose the [LiveKit guide](targets/livekit.md) or
   [Pipecat guide](targets/pipecat.md) for framework-specific runtime and
   deployment choices.

## Understand the package boundary

An Unmute package separates what the agent does from where it runs. Most
changes belong in one of three YAML surfaces.

```text
your-agent/
├── agent.yaml          # portable behavior
├── targets.yaml        # target infrastructure and optional model overrides
├── connections/        # telephony environment-variable names
│   └── primary_phone.yaml
├── tools/
│   └── lookup_order.yaml
├── instructions.md     # entry-agent instructions
├── agents/             # additional agent instructions
└── tasks/              # delegated-task instructions
```

The files have distinct responsibilities:

- `agent.yaml` defines models and describes agents, tasks, controls,
  conversation behavior, channels, and capacity.
- `tools/*.yaml` describes each tool contract and where the tool executes.
- `targets.yaml` selects LiveKit, Pipecat, or a managed target and carries its
  infrastructure settings and optional model overrides.
- `connections/*.yaml` maps telephony route keys to environment-variable
  names. Secret values stay in `.env` or the deployment secret store.
- Markdown files contain instructions. YAML points to them by path, which keeps
  long prompts out of the structural configuration.

A telephony package can declare several Pipecat and LiveKit carrier routes.
Each route uses one named target and one Connection and produces a separate
single-carrier project. The [phone-call guide](learn/07-phone-calls.md) lists
the current Twilio, Telnyx, Plivo, and gated Exotel integrations.

## Choose the right section

The documentation is organized around what you are trying to understand, not
around implementation packages.

- **Start** introduces the package and its YAML files.
- **Learn** changes one part of the package at a time, from a single agent to
  telephony.
- **Concepts** explains models, overrides, portability, tiers, and feature
  gates.
- **Reference** defines every YAML field and its target-specific behavior.
- **Targets** gathers the YAML choices and limitations for one platform.

## Compare the shipped code targets

LiveKit and Pipecat compile the same portable YAML into different native
Python projects. Both projects run in the browser or terminal with
`unmute dev`; their internal agent and task mechanics remain framework-native.

| Target | Entrypoint | Agent and task lowering |
|---|---|---|
| LiveKit | `agent.py` | Agents, handoffs, `AgentTask`, and `TaskGroup` |
| Pipecat | `bot.py` | Workers, worker handoffs, and Flows |

Use the [LiveKit guide](targets/livekit.md) and
[Pipecat guide](targets/pipecat.md) for their generated files, provider
catalogues, deployment paths, and current driver gates. Use these shared
reference pages when you need one exact YAML field:

- [Models](reference/models-and-voices.md) covers model fields and fallback
  chains.
- [Targets YAML](reference/targets-yaml.md) and
  [providers](reference/providers.md) cover model routing, plugin pins, and
  provider choices.
- [Tools YAML](reference/tools.md) covers webhook, local, and MCP tools.
- [Tasks](reference/tasks.md) and [controls](reference/controls.md) cover
  delegates, task models, task groups, handoffs, and human transfers.
- [Conversation](reference/conversation.md) covers interruption, inactivity,
  maximum duration, and thinking audio.
- [Channels and capacity](reference/channels-and-capacity.md) covers outbound
  calls and voicemail handling.

## Documentation contract

Examples show YAML before explanation and keep target-specific facts in the
target and reference pages. [SCHEMA.md](../../SCHEMA.md) and the driver specs
remain the source of truth when a user page and the implementation disagree.

The docs never hide a target limitation. If a YAML field cannot map faithfully
to a target, its reference page says so and names the supported alternatives.
