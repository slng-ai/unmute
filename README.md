# Unmute CLI

Unmute is SLNG's portability layer for voice agents: author the agent once as
a directory of declarative YAML, then compile or apply it to the orchestration
layer you choose. `unmute init` scaffolds an SLNG-bound project (one
`SLNG_API_KEY` covers hosted STT and TTS) unless you rebind; any provider a
framework integrates is a one-line binding change
(docs/user/reference/providers.md).

Commands: `init`, `validate`, `compile`, `apply`, `dev`
(docs/user/reference/cli.md). Drivers shipped today: **Pipecat** and
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

## Try it

```sh
bin/unmute init demo-agent          # SLNG-bound v1 package: agent.yaml,
                                    # instructions.md, targets.yaml, .env.example
bin/unmute validate demo-agent      # schema + capability + provider-matrix checks
bin/unmute compile demo-agent       # writes build/pipecat/ (bot.py, pyproject, ...)
bin/unmute dev demo-agent           # runs it with uv and opens a web client
```

`dev` needs `uv` on your PATH and keys in a `.env` at the package root; the
scaffold's `.env.example` lists exactly what the project reads.

The tracked examples exercise more of the schema:

```sh
bin/unmute validate examples/safe_core                       # five-target portable example
bin/unmute compile examples/safe_core --target pipecat
bin/unmute compile examples/remy --target livekit        # handoff + task groups on LiveKit
```

For a managed target, `apply` executes the plan against the provider
(ElevenLabs needs `ELEVENLABS_API_KEY`; it creates or PATCHes one agent
resource per Unmute agent). Run `compile` first if you want to inspect what
validate reports before touching a live account. The safe automated
equivalent is `go test ./internal/cli -run TestApplyElevenLabs` (mocked HTTP,
no live call).

## Not implemented yet

- Vapi and Deepgram drivers (specs exist in docs/spec/; both fail loud today).
- Per-driver maturity gates are listed on each target page in
  [docs/user/targets/](docs/user/targets/) and fail loud rather than silently
  dropping behavior.
