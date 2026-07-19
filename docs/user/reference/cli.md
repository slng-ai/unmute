# Reference: the CLI

`unmute` has five commands: `init`, `validate`, `compile`, `apply`, and `dev`.
All except `init` read a v1 package; see [agent.yaml](agent-yaml.md). Three
drivers are shipped: **Pipecat** and **LiveKit** are code targets, and
**ElevenLabs** is a managed target. Validation works for all five providers.
Generation, apply, or development commands report `driver is not implemented`
for Vapi and Deepgram until their drivers land.

**Exit codes:** `0` on success, `1` on error. Warnings go to standard error and still exit `0`; they never silently downgrade a result.

## init

```sh
unmute init [name]
```

Scaffolds a new v1 package. The noninteractive default is exactly `agent.yaml`, `instructions.md`, `targets.yaml`, and `.env.example`, with a ready-to-run `pipecat` target. Extra agents, tasks, and tools add their prompt or manifest files. Prints a `created <path>` line for each file.

- With a `name`, writes the package to that directory.
- With no argument on an interactive terminal, opens the creation wizard. Start
  with target, language, STT/LLM/TTS models, prompt, and greeting; optionally
  add variables, tools, agents, directional handoffs, typed tasks, ordered task
  groups, web/phone channels, and human transfers. Saved items remain visible
  in their section and open into one edit screen with a delete action; deleting
  also removes or resets dependent references. The required starter agent and
  default models offer **Reset** instead. Every reference picker lists the
  compatible items already created. Advanced conversation, fallback, capacity,
  and target settings stay under **Customize**.
- Provider choices come from the selected target's catalogue and each provider brand appears once. When a brand is available through more than one distributor, such as Cartesia directly or through SLNG, the wizard asks for the distributor next. Model ids, voice ids, and provider params remain forwarded text; the wizard does not invent an allowlist for them.
- A failed preflight opens a dedicated repair menu instead of printing above the creation menu. From there you can open the relevant saved-resource screens to edit or delete the failing configuration, then retry creation.
- Tools are execution-neutral in the menu. Webhook is available in shipped starters; Local Python remains visible with the selected driver's exact support limitation when that driver cannot emit a handler.
- Task results start as `{"result":"string"}`. Each key is one returned field whose value is a primitive type (`string`, `number`, `boolean`, or `integer`) or an enum; result-to-variable assignment uses pickers rather than handwritten JSON.
- Handoffs stay unavailable until two agents exist. Task groups stay unavailable until a task exists. Telephony declares required behavior and target transport/carrier only—it does not provision a phone number, SIP trunk, room, or carrier account.
- Before the review screen, the candidate is rendered in a temporary directory
  and sent through the real load, build, and selected-target generator. The
  review shows target warnings, required environment names, and forwarded
  models. A failing candidate is not written; its exact cause appears on a
  dedicated **Cannot create agent** screen with **Back**. Nothing reaches the
  destination until final confirmation.
- **Back** preserves earlier edits. In accessible mode, enter `:back`; in the keyboard UI, press Esc.
- It refuses to write into a directory that already exists and is non-empty, rather than overwrite an agent.

## validate

```sh
unmute validate <package-dir> [--target <name>]...
```

Loads, builds, and checks the package against its targets, without generating anything. Prints one row per target:

```text
TARGET        PROVIDER  RESULT
pipecat   pipecat   pass
```

Warnings and errors print to standard error, prefixed `warning:` and `error:`, each naming the target. Exits `1` if any target fails.

`validate` uses the schema's capability rules and the provider catalogue, not a driver, so it works for **all five providers** whether or not their driver is shipped. Use it to check portability across targets before you commit to one. Repeat `--target` to check specific instances, or omit it to check every declared target.

## compile

```sh
unmute compile <package-dir> [--target <name>]...
```

Runs validate, then generates. For a **code target** (Pipecat, LiveKit), writes the project to `<package-dir>/build/<target-name>/` and prints a `generated <path>` line per file. For a **managed target**, prints a line telling you to use `apply`. Omitting `--target` compiles every declared target.

Compiling a Vapi or Deepgram instance fails with `<provider> driver is not implemented` until those drivers ship.

## apply

```sh
unmute apply <package-dir> [--target <name>]...
```

For **managed** targets: reconcile the provider's live config to your spec. Run it against a code target and it tells you to use `unmute compile` instead. The **ElevenLabs** driver is shipped: it builds one Conversational-AI Agent resource per Unmute agent, creates them in dependency order (transfer targets first, captured ids wired into the callers), PATCHes agents pinned by `agent_id.<name>`, honors a `branch_id` pin, and authenticates with `ELEVENLABS_API_KEY`. Applying to a Vapi instance fails with `vapi driver is not implemented` until that driver ships.

## dev

```sh
unmute dev <agent-dir> [--target <name>] [--console] [--port 8765] [--bot-port 7860] [--no-open] [--verbose]
```

The fastest loop for a **Pipecat or LiveKit** instance: compiles the selected target to `build/<name>/`, runs it locally, and lets you talk to the agent — in the browser (default) or in your terminal (`--console`). Whatever you build, you can speak to.

- With exactly one declared target, `dev` selects it automatically.
- With multiple targets on an interactive terminal, `dev` always asks which instance to test and shows both instance name and provider. In a noninteractive shell, pass `--target <name>`; it never picks by YAML/map order or by preferring Pipecat.
- `--target` dispatches that exact instance. Pipecat and LiveKit both run; a selected ElevenLabs target points to `unmute apply`, and unshipped providers report their own missing dev runner.

**Web (default).** Opens a browser client:
- Pipecat: runs `uv run bot.py`, proxies WebRTC to the local runner, serves the client.
- LiveKit: runs `uv run agent.py dev`, waits for the worker to register, then serves a client that mints a token and joins the room (the agent joins the same room automatically). Zero-config by default: with no `LIVEKIT_URL` set, `unmute dev` uses the open-source dev server locally — reusing one already listening on `:7880`, or starting `livekit-server --dev` itself and stopping it when you quit. Install it once (`brew install livekit` on macOS; `curl -sSL https://get.livekit.io | bash` on Linux). Explicit `LIVEKIT_URL`/`LIVEKIT_API_KEY`/`LIVEKIT_API_SECRET` in the environment or `.env` (LiveKit Cloud or self-hosted) always win. With no creds and no binary, the error names the install command and points at `--console`.

**Console (`--console`).** Talk in the terminal over your local mic and speaker — no browser, no dev server:
- Pipecat: `uv run --extra console bot.py console`. The `console` extra pulls in pyaudio; on macOS run `brew install portaudio` first.
- LiveKit: `uv run agent.py console`. Needs **no** LiveKit credentials for a
  scaffold-default agent with native providers and local turn detection. It
  only needs `LIVEKIT_API_KEY` and `LIVEKIT_API_SECRET` if a model routes
  through LiveKit Inference, such as a think model with `provider: livekit` or
  the cloud `turn-detector`. The preflight tells you which.

- Requires `uv` on your `PATH` (see [install](../start/install.md)). Reads keys from a `.env` at the package root.
- `--port` sets the dev UI port (default 8765); `--bot-port` sets the Pipecat runner port (default 7860). Both are web-only — `--console` and `--no-open` ignore them.
- `--no-open` skips opening the browser; `--verbose` streams agent logs to your terminal.
- Web-mode agent logs are written to `build/<name>/bot.log` (Pipecat) or `agent.log` (LiveKit); console mode streams straight to your terminal. Press `ctrl-c` to stop.
- Fails clearly if no target is declared, the selected provider has no local runner, LiveKit creds are missing where required, or `uv` is not installed.
