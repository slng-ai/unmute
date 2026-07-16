# Your first agent

This page gets one agent talking, in your browser, in a few minutes. Three commands: `init`, `validate`, `dev`.

You need `unmute` built (see [Install](install.md)) and `uv` installed. You also need two API keys, explained below.

## 1. Create the package

An agent is a small folder of files. Unmute calls it a **package**. Create one with `init`:

```sh
unmute init support-bot
```

This writes a ready-to-run package:

```text
support-bot/
├── agent.yaml          # what the agent is
├── instructions.md     # the agent's prompt
├── targets.yaml        # one block per platform you compile to
└── .env.example        # the keys this agent needs
```

Open `agent.yaml` and you will see the same shape from [What is Unmute](what-is-unmute.md): a version, one agent named `assistant`, a greeting, a web channel, and a capacity block. Open `instructions.md` and you will see the agent's prompt, a single plain sentence you can edit.

`targets.yaml` already has one target ready to go, named `pipecat-dev`:

```yaml
targets:
  pipecat-dev:
    provider: pipecat
    version: "1.5.0"
    transport: daily-sip
    models:
      listen: { provider: slng, model: "slng/deepgram/nova:3-en" }
      turn:   { provider: local, model: silero }
      speak:
        assistant_voice: { provider: slng, model: "slng/deepgram/aura:2-en", voice: "aura-2-thalia-en" }
      reason:
        assistant_model: { provider: openai, model: "gpt-4.1-mini" }
```

This says: hear the caller with a SLNG-hosted speech-to-text model, detect end-of-turn on your own machine, speak with a SLNG-hosted voice, and think with OpenAI's `gpt-4.1-mini`. You will learn what every line means later. For now it works as is.

## 2. Add your keys

The scaffold tells you which keys this target needs, in `.env.example`:

```text
SLNG_API_KEY=
OPENAI_API_KEY=
```

`SLNG_API_KEY` covers the hosted speech-to-text and voice. `OPENAI_API_KEY` covers the reasoning model. Copy the file to `.env` and fill in your values:

```sh
cd support-bot
cp .env.example .env
# edit .env and paste your two keys
```

Keys live only in `.env`, never in the spec files, and `.env` should never be committed.

## 3. Validate

Before running anything, check that the agent is valid for its target:

```sh
unmute validate support-bot
```

You get one line per target:

```text
TARGET        PROVIDER  RESULT
pipecat-dev   pipecat   pass
```

If something is wrong (a missing binding, a feature the target cannot do), `validate` prints an `error:` line explaining exactly what and why, and exits non-zero. This is the fail-loud rule: you find out here, not when a caller is on the line. See [tags and gating](../concepts/tags-and-gating.md) for how these checks are decided.

## 4. Talk to it

```sh
unmute dev support-bot
```

This compiles the agent to a Pipecat project, starts it, and opens a page in your browser. Click to connect your microphone, and say hello. The agent greets you with the line from `agent.yaml` and answers using your prompt.

Under the hood `dev` does three things:

- Compiles the first Pipecat target into `support-bot/build/pipecat-dev/`.
- Runs that project with `uv run bot.py`.
- Serves a small web client and proxies your browser's audio to it.

Press `ctrl-c` to stop. Agent logs are written to `build/pipecat-dev/bot.log`; add `--verbose` to also stream them to your terminal.

## What you just built

One agent, one greeting, one prompt, running on a real Pipecat pipeline. That is the smallest useful thing. From here the [learn pages](../learn/01-one-agent.md) add one feature at a time: tools, shared state, a second agent with a handoff, and delegated tasks, until you have a full customer service agent.

Start with [learn/01: one agent](../learn/01-one-agent.md), which walks through every line of the spec you just ran.
