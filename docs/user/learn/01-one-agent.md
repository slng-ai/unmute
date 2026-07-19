# 01. One agent

We will build one customer service agent for a company called Acme Support, and grow it across these pages until it can look up customers, remember what it learned, hand off to a specialist, and delegate work. This page builds the smallest version: one agent that talks.

Everything here is **tier T0** and every field is `core`, so this agent runs on all five platforms. We compile it to Pipecat.

## The package

Three files:

```text
acme/
├── agent.yaml        # what the agent is
├── instructions.md   # the prompt
└── targets.yaml      # where it compiles to
```

### agent.yaml

```yaml
version: 1
entry_agent: intake

models:
  fast_reasoning:
    description: cheap and quick, for greeting and routing
    provider: openai
    model: gpt-4o-mini
  front_desk:
    description: "warm, concise"
    provider: slng
    model: "slng/deepgram/aura:2-en"
    voice: "aura-2-thalia-en"

agents:
  intake:
    instructions: instructions.md
    model: fast_reasoning
    voice: front_desk

conversation:
  greeting:
    speaks_first: agent
    text: "Hi, you have reached Acme Support. How can I help you today?"

channels:
  web: { kind: realtime_audio }

capacity:
  peak_sessions: 40
  max_sessions: 60
  avg_session_duration: 6m
```

### instructions.md

```markdown
You are the front desk voice agent for Acme Support. This is a phone call, so
keep every answer to one or two short sentences.

- Greet the caller and find out what they need.
- Answer questions about Acme in plain language.
- Never guess. If you do not know something, say so.
```

### targets.yaml

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    models:
      listen: { provider: slng, model: "slng/deepgram/nova:3-en" }
      turn:   { provider: local, model: silero }
```

Run it:

```sh
unmute validate acme
unmute dev acme
```

## Reading agent.yaml block by block

**`version`** is always `1`. It pins the spec format.

**`entry_agent`** names the agent that answers first. It must be one of the agents you define below. Right now there is only `intake`.

**`models`** defines every model once, concretely. `fast_reasoning` is a think model (it names an LLM `provider` and `model`); `front_desk` is a speak model (it also carries a `voice`). A model's kind follows from where an agent refers to it. `provider` + `model` are sent to the platform verbatim; you never write `placement` here (it is derived from `provider`). This is explained in [models and overrides](../concepts/profiles-and-bindings.md). The `listen` and `turn` roles (hearing, and end-of-turn) are per-target plumbing, so they live in `targets.yaml`.

**`agents`** is the heart. Each agent has a prompt file (`instructions`), a `model` (a think model), and a `voice` (a speak model). The names must match models you defined.

**`conversation`** shapes the call's behavior. Here, `greeting.speaks_first: agent` means the agent opens the call, and `text` is the exact opening line, spoken word for word every time. (Leave `text` out and the model writes its own opening from the prompt; that is portable on code targets like Pipecat, but see the note below.)

**`channels`** says how callers reach the agent. `realtime_audio` is a browser or app audio session. An agent needs at least one channel. Phone (`telephony`) is a later topic.

**`capacity`** declares your expected traffic: concurrent calls at the busy hour, a hard limit, and average call length. Unmute uses these to size the deployment. It is required on Pipecat because Pipecat is a code target you host yourself.

## What targets.yaml does

`targets.yaml` carries the infrastructure for the `pipecat` target plus the two per-target role slots:

- `listen` goes through a SLNG-hosted model, covered by one `SLNG_API_KEY`.
- `turn` runs locally with Silero, an on-device voice-activity detector. No key, no network.
- The speak model (`front_desk`) and think model (`fast_reasoning`) come straight from `agent.yaml` — this single-target package needs no overrides. `front_desk` uses SLNG TTS (same `SLNG_API_KEY`); `fast_reasoning` is OpenAI's `gpt-4o-mini`, covered by `OPENAI_API_KEY`.

Model names and voice ids are sent to the platform as written and never checked by Unmute. A typo surfaces when the agent runs, not at validate. See [models and overrides](../concepts/profiles-and-bindings.md).

## What just got harder

Nothing yet. This is the safe core:

- Every field is `core`, so this agent passes on all five platforms.
- The one thing to know: your **fixed** greeting line (`speaks_first: agent` with `text`) works everywhere. A **model-written** opening (no `text`) is generated on Pipecat and LiveKit, native on Vapi, but only conditional on ElevenLabs and warns on Deepgram. If you want zero warnings across all five, give a fixed `text`. On Pipecat specifically, both work.

Next: [02. Add a tool](02-add-a-tool.md), so the agent can look things up.
