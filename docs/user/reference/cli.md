# Reference: the CLI

`unmute` has five commands: `init`, `validate`, `compile`, `apply`, and `dev`. All of them except `init` read a v1 package (see [agent.yaml](agent-yaml.md)). Only the Pipecat driver is implemented today, so `compile` and `dev` produce artifacts for Pipecat targets; other providers report that their driver is not implemented.

**Exit codes:** `0` on success, `1` on error. Warnings go to standard error and still exit `0`; they never silently downgrade a result.

## init

```sh
unmute init [name]
```

Scaffolds a new v1 package: `agent.yaml`, `instructions.md`, `targets.yaml`, and `.env.example`, with a ready-to-run `pipecat-dev` target. Prints a `created <path>` line for each file.

- With a `name`, writes the package to that directory.
- With no argument on an interactive terminal, opens a menu to set the prompt, models, greeting, and language before writing. Nothing is written until you confirm.
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

`validate` uses the schema's capability rules, not a driver, so it works for **all five providers** even though only Pipecat compiles. Use it to check portability across targets before you commit to one. Repeat `--target` to check specific instances, or omit it to check every declared target.

## compile

```sh
unmute compile <package-dir> [--target <name>]...
```

Runs validate, then generates. For a **code target** (Pipecat), writes the project to `<package-dir>/build/<target-name>/` and prints a `generated <path>` line per file. For a **managed target**, prints a line telling you to use `apply`. Omitting `--target` compiles every declared target.

Only Pipecat has a driver: compiling a LiveKit, Vapi, ElevenLabs, or Deepgram instance fails with `<provider> driver is not implemented`.

## apply

```sh
unmute apply <package-dir> [--target <name>]...
```

Intended for **managed** targets (Vapi, ElevenLabs): reconcile the provider's live config to your spec. Run it against a code target and it tells you to use `unmute compile` instead. Because no managed driver is implemented yet, applying to a managed instance currently fails with `<provider> driver is not implemented`. This command is a placeholder until the first managed driver ships.

## dev

```sh
unmute dev <agent-dir> [--port 8765] [--bot-port 7860] [--no-open] [--verbose]
```

The fastest loop: compiles the **first Pipecat target** in the package to `build/<name>/`, runs it with `uv run bot.py`, serves a small web client, and opens it in your browser so you can talk to the agent.

- Requires `uv` on your `PATH` (see [install](../start/install.md)). Reads keys from a `.env` at the package root.
- `--port` sets the dev UI port (default 8765); `--bot-port` sets the port the Pipecat runner listens on (default 7860).
- `--no-open` skips opening the browser; `--verbose` streams agent logs to your terminal.
- Agent logs are written to `build/<name>/bot.log`. Press `ctrl-c` to stop.
- Fails clearly if no Pipecat target is declared, or if `uv` is not installed.
