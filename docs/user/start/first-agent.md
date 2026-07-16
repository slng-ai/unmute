# Build your first YAML package

This page introduces the smallest useful Unmute package. You will define one
portable agent in `agent.yaml` and bind it to LiveKit in `targets.yaml`, without
starting from command-line behavior or generated code.

## Create the package structure

Keep portable behavior, provider bindings, and long-form instructions in
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

Start with one entry agent, one model profile, one voice profile, and one audio
channel in `agent.yaml`.

```yaml
version: 1
language: en
entry_agent: assistant

pipeline:
  listen: { placement: api }
  turn: { placement: local, semantic_endpointing: preferred }
  speak: { placement: api }

models:
  assistant_model:
    description: Fast reasoning for general support
    placement: api

voices:
  assistant_voice:
    description: Warm and concise

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

1. `pipeline` describes where listening, turn detection, and speech happen.
2. `models` and `voices` declare portable profiles, not provider model IDs.
3. `agents` assigns those profiles and an instruction file to the entry agent.
4. `conversation` and `channels` describe what the caller experiences.
5. `capacity` declares the expected concurrency for a code target.

## Bind the profiles to LiveKit

Use `targets.yaml` to choose the concrete integrations for every open pipeline
role and every profile used by the agent.

```yaml
targets:
  livekit-dev:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    models:
      listen:
        provider: slng
        model: "slng/deepgram/nova:3-en"
      turn:
        provider: livekit
        model: turn-detector-mini
      speak:
        assistant_voice:
          provider: slng
          model: "slng/deepgram/aura:2-en"
          voice: "aura-2-thalia-en"
      reason:
        assistant_model:
          provider: openai
          model: gpt-4o-mini
          params: { temperature: 0.4 }
```

The names under `speak` and `reason` match the profile names in `agent.yaml`.
Changing a provider binding doesn't change the agent's behavior or prompt.

## Keep the boundary clear

The two YAML files answer different questions. Keeping that split intact makes
the agent portable.

| Question | File |
|---|---|
| What does the caller experience? | `agent.yaml` |
| Which agents, tasks, and tools exist? | `agent.yaml` |
| Which model and voice profiles do they use? | `agent.yaml` |
| Which provider and model ID implement each profile? | `targets.yaml` |
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
- [Configure LiveKit](../targets/livekit.md) for the full LiveKit YAML surface.
