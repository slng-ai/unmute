# Build your first YAML package

This page introduces the smallest useful Unmute package. You will define one
portable agent in `agent.yaml` and bind it to LiveKit and Pipecat in
`targets.yaml`, without starting from generated code.

## Create the package structure

Keep portable behavior, target infrastructure, and long-form instructions in
separate files.

```text
support-agent/
├── agent.yaml
├── targets.yaml
└── instructions.md
```

`agent.yaml` and `targets.yaml` are the two configuration files. The Markdown
file contains the prompt referenced by the agent definition.

## Describe the agent

Start with one entry agent, one think model, one speak model, and one audio
channel in `agent.yaml`.

```yaml
version: 1
entry_agent: assistant

models:
  think:
    assistant_model:
      description: Fast reasoning for general support
      provider: openai
      model: gpt-4o-mini
      temperature: 0.4
  speak:
    assistant_voice:
      description: Warm and concise
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-thalia-en"
  listen:
    transcriber:
      provider: slng
      model: "slng/deepgram/nova:3-en"
  turn:
    detector: { provider: local, model: silero }

agents:
  assistant:
    instructions: instructions.md
    model: assistant_model
    voice: assistant_voice

conversation:
  greeting:
    speaks_first: agent
    text: "Hi, you have reached Acme Support. How can I help?"
  interruption:
    enabled: true

channels:
  web: { kind: realtime_audio }

capacity:
  peak_sessions: 20
  max_sessions: 40
  avg_session_duration: 4m
```

Read this file from top to bottom as a behavior graph:

1. `models` defines each model once, under its kind section: `think`, `speak`, `listen`, and `turn`. The sole `listen` and `turn` entries select themselves; add alternates and swap with a top-level `listen: <name>`.
2. `agents` assigns the think and speak models and an instruction file to the entry agent.
3. `conversation` and `channels` describe what the caller experiences.
4. `capacity` declares the expected concurrency for a code target.

## Add both code targets

Use `targets.yaml` to name each framework. The model definitions are already
in `agent.yaml`; a target carries only its infrastructure and any whole-model
overrides, by name, for models it cannot run as defined.

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"

  livekit:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    models:
      detector: { provider: livekit, model: turn-detector-mini }
```

Pipecat uses the local Silero turn model from `agent.yaml`. LiveKit replaces
that one entry with its own mini turn detector. Both targets use the same
agents, prompts, listen model, think model, voice, conversation, and capacity.

Validate both targets, then compile both native projects.

```sh
unmute validate support-agent
unmute compile support-agent
```

The compile writes `build/pipecat/bot.py` and `build/livekit/agent.py`, plus
each framework's dependencies, deployment files, environment example, and
compile report.

## Keep the boundary clear

The two YAML files answer different questions. Keeping that split intact makes
the agent portable.

| Question | File |
|---|---|
| What does the caller experience? | `agent.yaml` |
| Which agents, tasks, and tools exist? | `agent.yaml` |
| Which models (provider + model id) exist, and which are selected? | `agent.yaml` |
| Which platform, and which per-target model overrides? | `targets.yaml` |
| Which framework version and plugin pins apply? | `targets.yaml` |
| Where do secrets go? | Environment variables, never YAML values |

## Expand the same package

Add features to these files instead of replacing the initial structure. The
following pages continue from this boundary.

- [Add a tool](../learn/02-add-a-tool.md) with a file in `tools/`.
- [Add shared variables](../learn/03-variables.md) to carry typed call state.
- [Add another agent](../learn/04-two-agents.md) and a guarded handoff.
- [Add a task](../learn/05-tasks.md) or an ordered
  [task group](../learn/06-task-groups.md).
- [Configure LiveKit](../targets/livekit.md) for its full YAML and deployment
  surface.
- [Configure Pipecat](../targets/pipecat.md) for its worker, Flow, transport,
  and deployment surface.
