# Pipecat

Pipecat is the target these docs build toward, and the most complete one today. This page shows what kind of target it is, what your spec turns into, how to bind models to it, exactly which features it emits, and how to run and deploy the result.

## What kind of target

Pipecat is a **code target.** Unmute writes a real Python project, you host it. This is the opposite of a managed target like Vapi, where the provider runs the agent and you only get an API.

Being a code target is why Pipecat can do so much. When a feature is not a built-in Pipecat setting, like a handoff guard or a delegated task, Unmute writes the Python for it. There is a project to put the code in. This is [the pattern rule](../concepts/our-take-on-orchestrators.md) working in your favor: on Pipecat, nearly the whole schema is available.

What it means for you:

- You get a folder of code you can read, run, and deploy without Unmute present.
- You host it yourself (locally with `unmute dev`, or on your own infrastructure or Pipecat Cloud).
- You need the keys for whichever providers your bindings use.
- Because you host it, `capacity` in `agent.yaml` is required: Unmute uses it to size the deployment.

## The agency model: workers

Everything Unmute generates for Pipecat is built on Pipecat's **workers** model. Knowing the shape helps you read the generated `bot.py`:

- A single **main worker** owns the transport and the listen (speech-to-text) step. It hears the caller.
- **Each agent is its own worker**, with its own reasoning model and its own voice. Only one agent is active at a time.
- **A handoff** (`agent_transfer`) is one worker activating another and stepping aside.
- **A task or task group** runs as a Pipecat Flow on the agent that delegated it: the agent's prompt and tools are swapped for the step's, then restored when the step finishes with its typed result. No extra worker is involved.

So a two-agent, one-task spec becomes a main worker plus two agent workers, wired together, with the task living inside its delegating agent. You never edit this; you change the spec and recompile. (For how the same yaml lowers on LiveKit, see [how targets run your agent](../concepts/how-targets-run-your-agent.md).)

## What gets generated

`unmute compile acme --target pipecat-dev` writes a complete project to `acme/build/pipecat-dev/`:

| File | What it is |
|---|---|
| `bot.py` | The whole agent: the pipeline, every agent worker, every task flow, tools, handoffs. |
| `pyproject.toml` | Pinned dependencies. Only the services your spec uses are included. |
| `Dockerfile` | A container image for deployment. |
| `pcc-deploy.toml` | Pipecat Cloud deploy config. |
| `README.md` | A quickstart for the generated project. |
| `.env.example` | The exact environment variables this spec needs, ready to copy to `.env`. |
| `compile-report.json` | A machine-readable record of what was compiled: target, version, agents, required env, and any warnings. |

The output folder is rewritten from scratch on every compile, so never edit it by hand. `bot.py` carries only the imports and code your spec actually exercises: no tools means no HTTP client import, no tasks means no task machinery. The emitted pipeline stays clean.

The `compile-report.json` is worth reading after a compile. It lists every required environment variable and every binding that was forwarded, so you can always see what was sent to the platform, which matters because bindings are [never validated](../concepts/profiles-and-bindings.md).

## Binding models on Pipecat

All four roles are **open** on Pipecat: you choose the listen model, the voice, and the reasoning model freely, and the turn role runs on your machine. The accepted `provider:` values per role, their key envs, and what each choice installs and emits are in the [providers reference](../reference/providers.md). A full binding block:

```yaml
targets:
  pipecat-dev:
    provider: pipecat
    version: "1.5.0"
    transport: daily-sip
    models:
      listen: { provider: deepgram, model: nova-3 }
      turn:   { provider: local, model: silero }
      speak:
        front_desk: { provider: slng, model: "slng/deepgram/aura:2-en", voice: "aura-2-thalia-en" }
        specialist: { provider: slng, model: "slng/deepgram/aura:2-en", voice: "aura-2-orion-en" }
      reason:
        fast_reasoning:    { provider: openai, model: gpt-4o-mini, params: { temperature: 0.4 } }
        careful_reasoning: { provider: openai, model: gpt-4o }
```

### Which provider maps to which service

The `provider` in each binding selects the Pipecat service class. Unmute knows these:

| Role | `provider` | Service used | Key needed |
|---|---|---|---|
| listen | `deepgram` | Deepgram STT | `DEEPGRAM_API_KEY` |
| listen | `slng` | SLNG STT (via the `pipecat-slng` package) | `SLNG_API_KEY` |
| listen | `openai` or other | OpenAI-compatible STT | `OPENAI_API_KEY` |
| reason | any | OpenAI-compatible LLM | `<PROVIDER>_API_KEY` |
| speak | `elevenlabs` | ElevenLabs TTS | `ELEVENLABS_API_KEY` |
| speak | `cartesia` | Cartesia TTS | `CARTESIA_API_KEY` |
| speak | `slng` | SLNG TTS (via `pipecat-slng`) | `SLNG_API_KEY` |
| speak | `openai` or other | OpenAI-compatible TTS | `OPENAI_API_KEY` |

The API key variable is the provider name uppercased with `_API_KEY`, so `openai` needs `OPENAI_API_KEY`. Unmute lists the exact set in `.env.example` and in the compile report; you never guess.

The reasoning role always uses an OpenAI-compatible client. To point it at a non-OpenAI, OpenAI-compatible endpoint, your binding supplies an endpoint environment variable and Unmute passes it as `base_url`. (SLNG routes by key and region, not a `base_url`.)

### The turn role and Silero

`turn` binds to `{ provider: local, model: silero }`. End-of-turn detection runs on-device with a Silero voice-activity detector: no key, no network hop. The turn binding is **advisory** on Pipecat: it tells Unmute your intent, but the actual detection is the local VAD. Semantic-endpointing preferences are advisory too.

### Transport

- **WebRTC** is the default and is what `unmute dev` uses to serve a browser test client. You do not configure it.
- **`transport: daily-sip`** is needed for a cold human transfer (dialing a real phone number over SIP). Set it on the target when you use `human_transfer`.

### Version pin

`version` is required and must be in the range the driver's templates support: **`>= 1.5.0` and `< 2.0.0`.** Version 1.5.0 is the first release with the workers model and with Pipecat Flows shipped inside the core package. A version outside the range fails the compile with a clear message. This is a code check against the templates, not a guess about the platform.

### The SLNG plugin

`provider: slng` bindings use `pipecat-slng`, a small package that routes speech-to-text and voice through SLNG's hosted models with one `SLNG_API_KEY`. When your spec uses it, Unmute adds it to `pyproject.toml` automatically. Model ids look like `slng/deepgram/nova:3-en`.

## Feature support on Pipecat

This is Pipecat's column from the Unmute schema. `ok` means it works, with no failures.

| Feature | Pipecat |
|---|---|
| single agent (T0) | ok |
| agent handoff, `full` history + `all` variables | ok |
| fixed opening line (`greeting.text`) | ok |
| model-written opening (no `text`) | ok (generated) |
| user speaks first | ok |
| webhook tools | ok |
| tool `output` schema, `interruption`, `effect` | ok |
| task (delegate and return) | ok |
| per-task `model` | not yet (driver gate) |
| task group, any `then` (return / transfer / end) | ok |
| task group `context_scope` (shared / isolated) | ok |
| handoff guard (`requires`) | ok |
| interruption `minimum_words` and `ignore_phrases` | ok |
| `inactivity` nudge and end | ok |
| `max_duration` | ok |
| `placement: local` for listen and speak | ok |
| cold human transfer | ok, needs `transport: daily-sip` |

Everything in the [learn pages](../learn/01-one-agent.md), including the guarded handoff, the task, and the task group, runs here. The one hard `fail` is the per-task `model:` override; it sits with the driver gates below.

## What the driver does not emit yet

Some features are in the schema and Pipecat itself supports them, but this first version of the driver does not write them yet. These are **maturity gates on the driver, not limits of Pipecat.** Using one fails the compile today, and the gate lifts when the driver adds it. Right now these are not emitted:

- **Model fallback** (`fallback` on a model profile).
- **Per-task `model:`.** Pipecat's mechanism for switching models mid-call stalls the conversation in the current release, so there is nothing safe to emit yet. Drop the override and the task runs on the delegating agent's model.
- **`thinking_audio`.**
- **Outbound calls and voicemail** (`outbound: true`, `on_voicemail`).
- **`local` Python-handler tools and `mcp` tools.** Use `webhook` tools, which are emitted.
- **Warm human transfer.** Pipecat ships warm transfer, but the driver emits `cold` only.
- **Handoff and task context shaping beyond the defaults:** any `history` other than `full`, a subset `context.variables` list rather than `all`, and `include_tool_calls: false`. The handoff carries the running context; finer shaping is not written yet.

If you stay within the feature table above, you will not hit any of these. When you do use one, the failure names it, so you are never surprised.

## Warnings and notes

Two different things surface, and it helps to tell them apart.

**Validation warnings** are printed to standard error during `validate` and `compile`. They pass (exit 0) but flag a real behavior difference. For an agent that uses turn preferences, inactivity, and a max duration, Pipecat prints:

```text
warning: pipecat-dev: Pipecat semantic endpointing depends on the bound model
warning: pipecat-dev: Pipecat driver must range-check inactivity durations
warning: pipecat-dev: Pipecat driver must verify a max-duration cap
```

**Notes** are recorded in `compile-report.json` under `notes`, not printed as warnings. They describe how the driver lowered something. For example:

```json
"notes": [
  "interruption ignore_phrases emitted as IGNORE_PHRASES; short phrases are also suppressed by the min-words turn-start strategy",
  "turn role lowers to on-device VAD (Silero); its binding is advisory"
]
```

Read both, then test the exact behavior they point at.

## Running and deploying

**Local, while iterating:**

```sh
unmute dev acme
```

This compiles the first Pipecat target, runs `bot.py` with `uv`, and opens a browser client so you can talk to the agent. It reads your keys from a `.env` at the package root. Logs go to `build/<target>/bot.log`; add `--verbose` to stream them.

**Compile only, to inspect or deploy the project:**

```sh
unmute compile acme --target pipecat-dev
```

Then, in `acme/build/pipecat-dev/`, the generated `README.md` shows the quickstart the project supports directly:

```sh
cp .env.example .env    # fill in your keys
uv run bot.py           # open the URL it prints to talk to the agent
```

For hosting, the project ships a `Dockerfile` and a `pcc-deploy.toml` for Pipecat Cloud. Because the output is an ordinary Python project, you can also run it anywhere you run Python.

## Where to go next

- Build the agent step by step: the [learn pages](../learn/01-one-agent.md), 01 through 06.
- Understand the binding split: [profiles and bindings](../concepts/profiles-and-bindings.md).
- Understand why features are gated: [tags and gating](../concepts/tags-and-gating.md).
- The exact per-target contract for every field: `SCHEMA.md` in the repository.
