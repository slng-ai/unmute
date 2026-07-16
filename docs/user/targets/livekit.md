# LiveKit

LiveKit is a code target with a complete driver. This page shows what kind of target it is, what your spec turns into, how to bind models to it, which features it emits, and how to run and deploy the result. It is the LiveKit twin of the [Pipecat page](pipecat.md); the two are worth reading side by side.

## What kind of target

LiveKit is a **code target.** Unmute writes a real Python project on the LiveKit Agents framework, and you host it. This is the opposite of a managed target like Vapi, where the provider runs the agent and you only get an API.

Being a code target is why LiveKit can do the whole schema. When a feature is not a built-in LiveKit setting, like a handoff guard or an ignore-phrase filter, Unmute writes the Python for it. There is a project to put the code in. This is [the pattern rule](../concepts/our-take-on-orchestrators.md) working in your favor.

What it means for you:

- You get a folder of code you can read, run, and deploy without Unmute present.
- You host it yourself, with a free LiveKit Cloud project carrying the media.
- You need LiveKit Cloud credentials plus the keys for whichever providers your bindings use.
- Because you host it, `capacity` in `agent.yaml` is required: Unmute uses it to size the deployment.

## The agency model: one session, agents take turns

Everything Unmute generates for LiveKit is built on one `AgentSession`. Knowing the shape helps you read the generated `agent.py`:

- The **session** owns the microphone, the speech-to-text, the default reasoning model, and the default voice. There is exactly one.
- **Each agent is an `Agent` class.** Only one holds the conversation at a time. An agent with its own model or voice profile carries its own `llm=` or `tts=` override; the rest ride the session defaults.
- **A handoff** (`agent_transfer`) is a tool that returns the next agent object. The session sees the return value and swaps speakers.
- **A task** is an `AgentTask` class with a typed `finish` tool. The delegating agent awaits it and receives only the typed result.
- **A task group** with `context_scope: shared` is a `TaskGroup` that runs the steps in order. With `isolated`, each step runs as a standalone `AgentTask` starting fresh, because `TaskGroup` always shares context.
- **Shared state** (`variables`) is a generated `Userdata` dataclass on the session. Task `assign` writes into it, handoff `requires` guards read from it.

So a two-agent, one-task spec becomes one session, two agent classes, and one task class. You never edit this; you change the spec and recompile. (For how the same yaml lowers on Pipecat, see [how targets run your agent](../concepts/how-targets-run-your-agent.md).)

## What gets generated

`unmute compile acme --target livekit-dev` writes a complete project to `acme/build/livekit-dev/`:

| File | What it is |
|---|---|
| `agent.py` | The whole agent: session wiring, every agent and task class, tools, handoffs, guards, telephony. |
| `pyproject.toml` | Pinned dependencies. Only the plugins your bindings use are included. |
| `Dockerfile` | A container image for deployment. |
| `livekit.toml` | LiveKit Cloud agent config for `lk agent create`. |
| `tools/` | Only when you use `local` tools: your handler files, copied verbatim. |
| `README.md` | A quickstart for the generated project. |
| `.env.example` | The exact environment variables this spec needs, ready to copy to `.env.local`. |
| `compile-report.json` | A machine-readable record: target, version, agents, required env, forwarded bindings, notes. |

The output folder is rewritten from scratch on every compile, so never edit it by hand. `agent.py` carries only the imports and code your spec exercises: no tasks means no `AgentTask` import, no variables means no `Userdata` class. The `compile-report.json` lists every forwarded binding, which matters because bindings are [never validated](../concepts/profiles-and-bindings.md).

## Binding models on LiveKit

All four roles are **open** on LiveKit. The accepted `provider:` values per role, their key envs, and what each choice installs are in the [providers reference](../reference/providers.md). A full binding block:

```yaml
targets:
  livekit-dev:
    provider: livekit
    version: "1.5.2"
    models:
      listen: { provider: slng, model: "slng/deepgram/nova:3-en" }
      turn:   { provider: livekit, model: turn-detector-mini }
      speak:
        front_desk: { provider: slng, model: "slng/deepgram/aura:2-en", voice: "aura-2-thalia-en" }
        specialist: { provider: elevenlabs, voice: EXAVITQu4vr4xnSDxMaL }
      reason:
        fast_reasoning: { provider: openai, model: gpt-4o-mini, params: { temperature: 0.4 } }
```

### Which provider maps to which service

| Role | `provider` | Service used | Key needed |
|---|---|---|---|
| listen | `slng` | `slng.STT` (the `livekit-plugins-slng` package) | `SLNG_API_KEY` |
| listen | `deepgram` | `deepgram.STT` | `DEEPGRAM_API_KEY` |
| speak | `slng` | `slng.TTS` | `SLNG_API_KEY` |
| speak | `elevenlabs` | `elevenlabs.TTS` | `ELEVEN_API_KEY` |
| speak | `cartesia` | `cartesia.TTS` | `CARTESIA_API_KEY` |
| reason | any `provider/model` pair | `inference.LLM` (LiveKit Inference) | none, billed through LiveKit Cloud |

Two things to notice. First, the SLNG route form: on LiveKit the plugin takes the bare route, so `slng/deepgram/nova:3-en` in your binding is emitted as `model="deepgram/nova:3-en"`. Second, key names are per integration, not a convention: LiveKit's ElevenLabs plugin reads `ELEVEN_API_KEY`, while Pipecat's reads `ELEVENLABS_API_KEY`. You never guess: `.env.example` and the compile report list the exact set.

### Reasoning on LiveKit Inference

The `reason` role lowers to `inference.LLM`, LiveKit's hosted model gateway. Any `provider/model` pair the gateway serves works, you carry no provider API key, and usage bills through your LiveKit Cloud project. `params` forward as `extra_kwargs`. Model fallback chains lower to the native `llm.FallbackAdapter`.

### The turn role

Turn detection lowers to `inference.TurnDetector` plus a Silero voice-activity detector. The turn binding is **advisory**: it records intent, and the compile report notes it.

### Version and pins

`version` is required and must be **`>= 1.5` and `< 2.0`**, the range the driver's templates support. Plugin packages version independently; a `pins:` entry raises that plugin's floor in `pyproject.toml`:

```yaml
    pins:
      livekit-plugins-slng: "1.7.0"
```

A pin below the known floor, or a pin for a package Unmute does not emit, fails the compile with a clear message. `sdk_language` must be `python` (or omitted): the driver has Python templates only today, and anything else fails loud rather than emitting a wrong project.

## Feature support on LiveKit

This is LiveKit's column from the Unmute schema. `ok` means it works, with no failures. Everything below is emitted by the driver today; a parity test in the repository keeps this table honest.

| Feature | LiveKit |
|---|---|
| single agent (T0) | ok |
| agent handoff, any `history`, `variables` all or subset | ok |
| handoff guard (`requires`) | ok, generated check |
| history `summary` | ok, generated with your `summarizer` profile |
| fixed opening line (`greeting.text`) | ok |
| model-written opening (no `text`) | ok (generated) |
| user speaks first | ok |
| webhook tools, on agents and tasks | ok |
| `local` tools (your Python handler, copied into the project) | ok |
| `mcp` tools (server address from the tool's `url_env`) | ok |
| tool `interruption` other than default | warn: tools run to completion |
| task (delegate and return, `assign` into variables) | ok |
| per-task `model` | ok, the task gets its own `llm=` |
| task group, any `then` (return / transfer / end) | ok (warn: TaskGroup is experimental) |
| task group `context_scope` (shared / isolated) | ok |
| model `fallback` | ok, native `FallbackAdapter` |
| interruption `minimum_words` and `ignore_phrases` | ok |
| `inactivity` nudge and end | ok |
| `max_duration` | ok |
| `thinking_audio` | ok, native background audio |
| `placement: local` for listen and speak | ok |
| cold human transfer | ok, SIP transfer |
| warm human transfer, `briefing: summary` | ok (note: Beta on Python) |
| outbound calls and voicemail (`on_voicemail`) | ok, native answering-machine detection |

Two things do not compile here, by structure rather than by gate: a custom `endpoint_env` on a `speak` binding (no catalogued slot takes it), and a warm-transfer `briefing` of `message` or `wait` (LiveKit supports `summary` only). The error names the reason.

## Warnings and notes

Same split as everywhere: **validation warnings** go to standard error and pass with exit 0, **notes** land in `compile-report.json`. Typical warnings on LiveKit:

```text
warning: livekit-dev: LiveKit TaskGroup is experimental
warning: livekit-dev: LiveKit runs tool executions to completion; a per-tool interruption preference is not enforced
```

And a typical note, for a spec with a warm transfer:

```json
"notes": [
  "human_transfer warm uses livekit-agents beta.workflows on Python (Beta)"
]
```

Read both, then test the exact behavior they point at.

## Running and deploying

The browser test client of `unmute dev` is Pipecat only for now. On LiveKit you compile, then run the generated project directly:

```sh
unmute compile acme --target livekit-dev
cd acme/build/livekit-dev
uv sync
cp .env.example .env.local    # LiveKit Cloud creds + your provider keys
python agent.py dev
```

`LIVEKIT_URL`, `LIVEKIT_API_KEY`, and `LIVEKIT_API_SECRET` come from a LiveKit Cloud project (free tier is fine). `dev` mode connects the agent to your project; talk to it with any LiveKit client, for example the Agents Playground. Telephony features (transfers, outbound, voicemail) additionally need a SIP trunk; outbound and warm transfer read its id from `LIVEKIT_SIP_OUTBOUND_TRUNK`.

For hosting, the project ships a `Dockerfile` and a `livekit.toml`, so `lk agent create` deploys it to LiveKit Cloud, and any place that runs Python runs it too.

## Where to go next

- Build the agent step by step: the [learn pages](../learn/01-one-agent.md), 01 through 07.
- The same machinery explained side by side with Pipecat: [how targets run your agent](../concepts/how-targets-run-your-agent.md).
- Understand the binding split: [profiles and bindings](../concepts/profiles-and-bindings.md).
- The exact per-target contract for every field: `SCHEMA.md` in the repository.
