# What is Unmute

Unmute builds voice agents. A voice agent is a program you talk to on the phone or in a browser. It listens, thinks, and talks back.

There are many platforms that run voice agents. LiveKit, Pipecat, Vapi, and Deepgram are four of them. They all do the same basic job, but each one wants your agent written a different way. If you build on one and later want another, you usually rewrite everything.

Unmute fixes that. You describe your agent once, in plain files. Then you pick a platform and Unmute writes that platform's version for you.

## The one idea

You write **what the agent should do**. You never write **how a platform does it**.

Here is the whole promise:

- You describe the agent once, in a small set of files.
- You pick a target platform.
- Unmute compiles your description into that platform's own project or settings.
- If a platform cannot do something you asked for, Unmute tells you before anything runs, in that platform's own words. It never quietly drops a feature.

That last point is the important one. Most tools that "support many platforms" do it by only offering what every platform has in common, or by silently ignoring the parts a platform cannot do. Unmute does neither. It lets you ask for real features, and when a feature will not work on your chosen platform, it stops and says so clearly. This is called **failing loud**, and it runs through everything Unmute does.

## A tiny example

This is a complete one-agent spec. It is a file called `agent.yaml`:

```yaml
version: 1
entry_agent: assistant

models:
  think:
    assistant_model:
      description: default reasoning model
      provider: openai
      model: gpt-4o-mini
  speak:
    assistant_voice:
      description: default voice
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-thalia-en"
  listen:
    transcriber:
      provider: slng
      model: "slng/deepgram/nova:3-en"
  turn:
    vad: { provider: local, model: silero }

agents:
  assistant:
    instructions: instructions.md
    model: assistant_model
    voice: assistant_voice

conversation:
  greeting:
    speaks_first: agent
    text: "Hi, thanks for calling. How can I help you today?"

channels:
  web: { kind: realtime_audio }

capacity:
  peak_sessions: 10
  max_sessions: 20
  avg_session_duration: 5m
```

You do not need to understand every line yet. The point is the shape: the models are defined once, here, and the agent references them by name. The platform choice (which orchestrator, version pins, keys) lives in a separate file called `targets.yaml`, one block per platform, which can also override a model, by name, for a platform that cannot run it. Change the target, keep the agent.

## What this documentation covers

These docs take you from nothing to a complex agent that can compile to both
shipped Python code targets, LiveKit and Pipecat.

- [Start](install.md) gets the tool installed and a first agent talking.
- [Learn](../learn/01-one-agent.md) grows one customer-service agent one feature
  at a time. Its generated-code examples use Pipecat as one concrete lowering.
- [Concepts](../concepts/how-unmute-works.md) explains the portable YAML and how
  each target interprets it.
- [LiveKit](../targets/livekit.md) and
  [Pipecat](../targets/pipecat.md) show the two native generated projects,
  local development paths, deployment files, and driver gates.

Start with [Install](install.md), then build
[your first agent](first-agent.md).

## Know which drivers ship

LiveKit and Pipecat are shipped code targets: `compile` writes a runnable
Python project that you host. Vapi and Deepgram still validate against
the shared schema, but their generators aren't implemented.

Every feature page names the target-specific warning or failure when the
frameworks differ. These outcomes come from the same capability table that
guards generation, so a driver never silently drops an accepted field.
