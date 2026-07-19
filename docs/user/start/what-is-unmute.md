# What is Unmute

Unmute builds voice agents. A voice agent is a program you talk to on the phone or in a browser. It listens, thinks, and talks back.

There are many platforms that run voice agents. LiveKit, Pipecat, Vapi, ElevenLabs, and Deepgram are five of them. They all do the same basic job, but each one wants your agent written a different way. If you build on one and later want another, you usually rewrite everything.

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
  assistant_model:
    description: default reasoning model
    provider: openai
    model: gpt-4o-mini
  assistant_voice:
    description: default voice
    provider: slng
    model: "slng/deepgram/aura:2-en"
    voice: "aura-2-thalia-en"

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

You do not need to understand every line yet. The point is the shape: the models are defined once, here, and the agent references them by name. The platform choice and its per-target plumbing (which orchestrator, the listen/turn roles, version pins, keys) live in a separate file called `targets.yaml`, one block per platform, which can also override a model for a platform that cannot run it. Change the target, keep the agent.

## What this documentation covers

These docs take you from nothing to a complex agent running on Pipecat, one of the five platforms.

- [start/](install.md) gets the tool installed and a first agent talking in minutes.
- [learn/](../learn/01-one-agent.md) grows one small customer service agent, one new feature per page, until it has two agents, tools, shared state, handoffs, and delegated tasks.
- [concepts/](../concepts/how-unmute-works.md) explains the ideas behind the fields, for when you are curious or confused.
- [targets/pipecat.md](../targets/pipecat.md) shows exactly what your spec turns into on Pipecat.

Start with [Install](install.md), then build [your first agent](first-agent.md).

## A note on other platforms

This documentation focuses on Pipecat, because that is the platform whose support is complete today. Unmute is designed for all five platforms, and each feature page tells you honestly how that feature behaves on every one of them. When a page says a feature "fails on Vapi" or "warns on Deepgram", that is a real fact from the Unmute schema, not a guess. You can rely on it.
