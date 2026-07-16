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

pipeline:
  listen: { placement: api }
  speak:  { placement: api }

models:
  fast_reasoning:
    description: cheap and quick, for greeting and routing
    placement: api

voices:
  front_desk: { description: "warm, concise" }

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
  pipecat-dev:
    provider: pipecat
    version: "1.5.0"
    models:
      listen: { provider: slng, model: "slng/deepgram/nova:3-en" }
      turn:   { provider: local, model: silero }
      speak:
        front_desk: { provider: slng, model: "slng/deepgram/aura:2-en", voice: "aura-2-thalia-en" }
      reason:
        fast_reasoning: { provider: openai, model: gpt-4o-mini }
```

Run it:

```sh
unmute validate acme
unmute dev acme
```

## Reading agent.yaml block by block

**`version`** is always `1`. It pins the spec format.

**`entry_agent`** names the agent that answers first. It must be one of the agents you define below. Right now there is only `intake`.

**`pipeline`** describes the three jobs in a voice loop: `listen` (turn speech into text), `speak` (turn text into speech), and `turn` (decide when the caller has finished talking). Here you set only **where the model runs**, with `placement`. `api` means a hosted endpoint; `local` means your own machines. `api` is the portable choice. You do not name the actual models here; that is `targets.yaml`'s job. (`turn` is optional in `agent.yaml`; the target decides whether it needs one.)

**`models`** and **`voices`** declare **profiles**: abstract names with descriptions. `fast_reasoning` is a reasoning model, `front_desk` is a voice. They are bound to real models per target. This split is explained in [profiles and bindings](../concepts/profiles-and-bindings.md).

**`agents`** is the heart. Each agent has a prompt file (`instructions`), a `model` profile, and a `voice` profile. The names must match profiles you declared.

**`conversation`** shapes the call's behavior. Here, `greeting.speaks_first: agent` means the agent opens the call, and `text` is the exact opening line, spoken word for word every time. (Leave `text` out and the model writes its own opening from the prompt; that is portable on code targets like Pipecat, but see the note below.)

**`channels`** says how callers reach the agent. `realtime_audio` is a browser or app audio session. An agent needs at least one channel. Phone (`telephony`) is a later topic.

**`capacity`** declares your expected traffic: concurrent calls at the busy hour, a hard limit, and average call length. Unmute uses these to size the deployment. It is required on Pipecat because Pipecat is a code target you host yourself.

## What targets.yaml does

`targets.yaml` binds each profile to a real model, for the `pipecat-dev` target:

- `listen` and `speak` go through SLNG-hosted models, covered by one `SLNG_API_KEY`.
- `turn` runs locally with Silero, an on-device voice-activity detector. No key, no network.
- `reason` is OpenAI's `gpt-4o-mini`, covered by `OPENAI_API_KEY`.

Model names and voice ids here are sent to the platform as written and never checked by Unmute. A typo surfaces when the agent runs, not at validate. See [profiles and bindings](../concepts/profiles-and-bindings.md).

## What just got harder

Nothing yet. This is the safe core:

- Every field is `core`, so this agent passes on all five platforms.
- The one thing to know: your **fixed** greeting line (`speaks_first: agent` with `text`) works everywhere. A **model-written** opening (no `text`) is generated on Pipecat and LiveKit, native on Vapi, but only conditional on ElevenLabs and warns on Deepgram. If you want zero warnings across all five, give a fixed `text`. On Pipecat specifically, both work.

Next: [02. Add a tool](02-add-a-tool.md), so the agent can look things up.
