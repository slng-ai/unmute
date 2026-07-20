# Unmute CLI

Unmute is SLNG's portability layer for voice agents: author the agent once as
a directory of declarative YAML, then compile or apply it to the orchestration
layer you choose. `unmute init` scaffolds an SLNG-bound project (one
`SLNG_API_KEY` covers hosted STT and TTS) unless you change a model; any
provider a framework integrates is a one-line model change
([provider reference](docs/user/reference/providers.md)).

Commands: `init`, `validate`, `compile`, `apply`, `dev`
([CLI reference](docs/user/reference/cli.md)). Drivers shipped today:
**Pipecat** and
**LiveKit** (code targets, `compile` writes a runnable Python project) and
**ElevenLabs** (managed target, `apply` reconciles the provider config). Vapi
and Deepgram fail with `driver is not implemented` until theirs land.

Where things live: user docs in [docs/user/](docs/user/README.md), the locked
schema in [SCHEMA.md](SCHEMA.md), driver specs in [docs/spec/](docs/spec/),
the provider catalogue design and findings in
[PROVIDER_CATALOG.md](PROVIDER_CATALOG.md), vocabulary in
[CONTEXT.md](CONTEXT.md), engineering rules in [CLAUDE.md](CLAUDE.md).

## Build

```sh
make build        # writes bin/unmute
bin/unmute --help
make install      # into your Go bin path
```

Direct equivalent: `CGO_ENABLED=0 go build -o bin/unmute .`

## Docs

The user guides in [docs/user/](docs/user/README.md) render as a searchable
site with no build step — [docsify](https://docsify.js.org) serves the Markdown
in place from a CDN (needs `npx` on your PATH).

```sh
make docs          # serves docs/user/ on http://localhost:3000
```

Then open `http://localhost:3000` and browse the Start / Learn / Concepts /
Reference / Targets sidebar. Edit any `docs/user/**/*.md` and the open page
live-reloads on save. Only `docs/user/` is served, so the engineering specs in
`docs/spec/` stay out of the site.

Direct equivalent: `npx --yes docsify-cli serve docs/user --port 3000`.

## Test

The default gate is pure Go and needs zero Python:

```sh
make test         # = go test ./...  (L1 unit, L2 command, L3 golden)
make lint
make fmt          # gofmt -w . && go vet ./...
```

Regenerate goldens after an intentional output change:

```sh
go test ./internal/scaffold -update
go test ./internal/generate -run TestPipecatV1 -update-pipecat
go test ./internal/generate -run TestLiveKitV1RemyGolden -update-livekit
go test ./internal/generate -run TestCatalogResolutionGolden -update-catalog
```

L4 smoke is opt-in (needs `uv` and network; skips without `uv`):

```sh
make smoke
```

Smoke resolves the emitted `pyproject.toml` into a real venv, imports the
generated `bot.py`, and instantiates every emitted service constructor against
the installed packages, so provider kwarg drift fails here rather than at a
user's first run.

## Try the CLI end to end

The following workflow covers the current architecture: author a portable
package, validate it, compile platform-native projects, and run those generated
projects either directly or through `unmute dev`.

### Initialize and compile a package

Run these commands from the repository root. The default scaffold declares one
Pipecat target named `pipecat`.

```sh
make build
bin/unmute init demo-agent
bin/unmute validate demo-agent
bin/unmute compile demo-agent
```

`init` writes `agent.yaml`, `instructions.md`, `targets.yaml`, and
`.env.example`. `compile` writes the native project to
`demo-agent/build/pipecat/`; the generated project does not import or require
Unmute at runtime.

### Validate and compile every example

The [example matrix](examples/README.md) covers one large prompt, one task,
ordered task groups, and sub-agent handoffs. Every package declares both
`pipecat` and `livekit`, so omitting `--target` validates and compiles both.

```sh
for EXAMPLE in simple-prompt single-task task-groups subagents; do
  bin/unmute validate "examples/$EXAMPLE"
  bin/unmute compile "examples/$EXAMPLE"
done
```

Compilation replaces each selected `build/<target>/` directory. Compile before
copying credentials or running `uv`; recompiling removes files such as `.env`,
`.env.local`, and `.venv` from the generated directory.

### Configure credentials for the examples

Keep shared credentials in the ignored repository-root `.env.local`. Use the
generated `.env.example` files as the exact list of variables, and fill in the
values before running an agent.

```sh
EXAMPLE=simple-prompt

sed -n '1,200p' "examples/$EXAMPLE/build/pipecat/.env.example"
sed -n '1,200p' "examples/$EXAMPLE/build/livekit/.env.example"
```

Set `EXAMPLE` to `simple-prompt`, `single-task`, `task-groups`, or `subagents`
and repeat the run commands below to test each orchestration structure.

### Run the generated Pipecat project

Pipecat loads `.env` from its generated project directory. The default command
starts a WebRTC test client and prints the URL to open.

```sh
EXAMPLE=simple-prompt
cp .env.local "examples/$EXAMPLE/build/pipecat/.env"
(
  cd "examples/$EXAMPLE/build/pipecat"
  uv run bot.py
)
```

To use your terminal microphone and speaker instead, install PortAudio first
and run the console extra.

```sh
EXAMPLE=simple-prompt
cp .env.local "examples/$EXAMPLE/build/pipecat/.env"
(
  cd "examples/$EXAMPLE/build/pipecat"
  uv run --extra console bot.py console
)
```

### Run the generated LiveKit project

LiveKit loads `.env.local` from its generated project directory. Console mode
is the shortest direct test of the emitted agent.

```sh
EXAMPLE=simple-prompt
cp .env.local "examples/$EXAMPLE/build/livekit/.env.local"
(
  cd "examples/$EXAMPLE/build/livekit"
  uv run agent.py console
)
```

To register the generated agent as a LiveKit development worker, run its
development command against the LiveKit server configured in `.env.local`.

```sh
EXAMPLE=simple-prompt
cp .env.local "examples/$EXAMPLE/build/livekit/.env.local"
(
  cd "examples/$EXAMPLE/build/livekit"
  uv run agent.py dev
)
```

### Run an example through `unmute dev`

`dev` is implemented in the current CLI. It recompiles the selected target,
runs the generated project with `uv`, and opens a browser client by default.
Unlike direct generated-project runs, it reads `.env` from the source package
root.

```sh
EXAMPLE=simple-prompt
cp .env.local "examples/$EXAMPLE/.env"

# Run one command at a time. Press ctrl-c before trying another mode.
bin/unmute dev "examples/$EXAMPLE" --target pipecat
bin/unmute dev "examples/$EXAMPLE" --target pipecat --console
bin/unmute dev "examples/$EXAMPLE" --target livekit
bin/unmute dev "examples/$EXAMPLE" --target livekit --console
```

`dev` needs `uv` on your `PATH`. Pipecat console mode also needs PortAudio.
LiveKit web mode uses configured LiveKit credentials or reuses or starts a
local `livekit-server --dev` process.

The public packages use one salon workflow to show increasing orchestration
structure. Five-target portability and the legacy combined handoff/task-group
case remain internal fixtures under `internal/testdata/`.

For a managed target, `apply` executes the plan against the provider
(ElevenLabs needs `ELEVENLABS_API_KEY`; it creates or PATCHes one agent
resource per Unmute agent). Run `compile` first if you want to inspect what
validate reports before touching a live account. The safe automated
equivalent is `go test ./internal/cli -run TestApplyElevenLabs` (mocked HTTP,
no live call).

## Not implemented yet

Validation covers all five targets. These drivers still lack an executable
generation or apply path:

- Vapi and Deepgram drivers (specs exist in `docs/spec/`; generation fails
  clearly today).
- Per-driver maturity gates are listed on each target page in
  [docs/user/targets/](docs/user/targets/) and fail loud rather than silently
  dropping behavior.
