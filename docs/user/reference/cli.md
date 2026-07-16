# Reference: the CLI

`unmute` has five commands: `init`, `validate`, `compile`, `apply`, and `dev`. All of them except `init` read a v1 package (see [agent.yaml](agent-yaml.md)). Three drivers are shipped: **Pipecat** and **LiveKit** (code targets, `compile` writes a runnable project) and **ElevenLabs** (managed target, `apply` reconciles the provider's config). Vapi and Deepgram report `driver is not implemented` until theirs land.

**Exit codes:** `0` on success, `1` on error. Warnings go to standard error and still exit `0`; they never silently downgrade a result.

## init

```sh
unmute init [name]
```

Scaffolds a new v1 package. The noninteractive default is exactly `agent.yaml`, `instructions.md`, `targets.yaml`, and `.env.example`, with a ready-to-run `pipecat-dev` target. Extra agents, tasks, and tools add their prompt or manifest files. Prints a `created <path>` line for each file.

- With a `name`, writes the package to that directory.
- With no argument on an interactive terminal, opens the creation wizard. Start with target, language, STT/LLM/TTS bindings, prompt, and greeting; optionally add variables, tools, agents, directional handoffs, typed tasks, ordered task groups, web/phone channels, and human transfers. Saved items remain visible in their section. Select a variable, tool, or agent to edit it; every reference picker lists the compatible items already created. Advanced conversation, fallback, capacity, and target settings stay under **Customize**.
- Provider choices come from the selected target's catalogue. Model ids, voice ids, and provider params remain forwarded text; the wizard does not invent an allowlist for them.
- Tools are execution-neutral in the menu. Webhook is available in shipped starters; Local Python remains visible with the selected driver's exact support limitation when that driver cannot emit a handler.
- Task results start as `{"result":"string"}`. Each key is one returned field whose value is a primitive type (`string`, `number`, `boolean`, or `integer`) or an enum; result-to-variable assignment uses pickers rather than handwritten JSON.
- Handoffs stay unavailable until two agents exist. Task groups stay unavailable until a task exists. Telephony declares required behavior and target transport/carrier only—it does not provision a phone number, SIP trunk, room, or carrier account.
- Before the review screen, the candidate is rendered in a temporary directory and sent through the real load, build, and selected-target generator. The review shows target warnings, required env names, and forwarded bindings. A failing candidate is not written; its exact cause appears on a dedicated **Cannot create agent** screen with Back, rather than above the editor. Nothing reaches the destination until final confirmation.
- **Back** preserves earlier edits. In accessible mode, enter `:back`; in the keyboard UI, press Esc.
- It refuses to write into a directory that already exists and is non-empty, rather than overwrite an agent.

## validate

```sh
unmute validate <package-dir> [--target <name>]...
```

Loads, builds, and checks the package against its targets, without generating anything. Prints one row per target:

```text
TARGET        PROVIDER  RESULT
pipecat-dev   pipecat   pass
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
unmute dev <agent-dir> [--target <name>] [--port 8765] [--bot-port 7860] [--no-open] [--verbose]
```

The fastest loop for a Pipecat instance: compiles the selected target to `build/<name>/`, runs it with `uv run bot.py`, serves a small web client, and opens it in your browser so you can talk to the agent.

- With exactly one declared target, `dev` selects it automatically.
- With multiple targets on an interactive terminal, `dev` always asks which instance to test and shows both instance name and provider. In a noninteractive shell, pass `--target <name>`; it never picks by YAML/map order or by preferring Pipecat.
- `--target` dispatches that exact instance. The local browser runner is currently Pipecat-only: a selected LiveKit target points to `unmute compile`, a selected ElevenLabs target points to `unmute apply`, and unshipped providers report their own missing dev runner.

- Requires `uv` on your `PATH` (see [install](../start/install.md)). Reads keys from a `.env` at the package root.
- `--port` sets the dev UI port (default 8765); `--bot-port` sets the port the Pipecat runner listens on (default 7860).
- `--no-open` skips opening the browser; `--verbose` streams agent logs to your terminal.
- Agent logs are written to `build/<name>/bot.log`. Press `ctrl-c` to stop.
- Fails clearly if no target is declared, the selected provider has no local runner, or `uv` is not installed.
