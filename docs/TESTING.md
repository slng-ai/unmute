# Testing & verification

How to verify every build step of the Unmute CLI, from a clean checkout to a
Pipecat project that runs. Commands assume the repo root as the working
directory.

## TL;DR — the full gate

```sh
make fmt      # gofmt + go vet   (may modify files)
make lint     # golangci-lint
make build    # static bin/unmute
make test     # L1–L3, zero Python
make smoke    # L4, needs uv (opt-in)
```

If all five are clean, the tree is green. `make test` is the required gate;
`make smoke` is opt-in (needs Python via `uv`).

## Prerequisites

- Go 1.24 (pinned in `go.mod`).
- `golangci-lint` for `make lint`.
- `uv` for `make smoke` and any manual `py_compile` / `unmute dev` run
  (https://docs.astral.sh/uv/). The default `make test` gate needs no Python.

## Build

```sh
make build
```

Writes a static binary (`CGO_ENABLED=0`, version stamped at link time) to
`bin/unmute`:

```sh
bin/unmute --version
bin/unmute --help
```

Direct equivalent for local iteration:

```sh
CGO_ENABLED=0 go build -o bin/unmute .
```

Install into your Go bin path:

```sh
make install
```

## Format & vet

```sh
make fmt
```

Runs `gofmt -w .` then `go vet ./...`. It can modify Go files, so run it before
committing, not as a read-only check.

## Lint

```sh
make lint
```

Runs `golangci-lint run` (config in `.golangci.yml`: errcheck, govet,
staticcheck, ineffassign, unused). Expect `0 issues`.

## Test suite (L1–L3)

The default gate is pure Go and needs zero Python:

```sh
make test        # == go test ./...
```

It runs three layers:

- **L1 unit** — pure logic, table-driven (`internal/spec`, `internal/ir`,
  `internal/target`).
- **L2 in-process command** — the real Cobra tree, output captured
  (`internal/cli`, `internal/tui`).
- **L3 golden** — byte-for-byte checks of generated files
  (`internal/scaffold`, `internal/generate`, `internal/ir`).

Run focused packages:

```sh
go test ./internal/spec
go test ./internal/ir
go test ./internal/target
go test ./internal/generate
go test ./internal/cli
go test ./internal/scaffold
go test ./internal/tui
```

### What each layer proves

| Package | Layer | Proves |
|---|---|---|
| `internal/spec` | L1 | `spec.Load` parses the package and reports unknown fields with line/col. |
| `internal/ir` | L1 + L3 | `ir.Build` + `ir.Validate`: reference resolution, name rules, fallback cycles, capacity, maturity gates, and the resolved-IR golden (`TestCompilerGolden`). |
| `internal/target` | L1 | The capability table is complete and typed; telephony/MCP resolution. |
| `internal/generate` | L1 + L3 | Provider dispatch validates first (V17); the Pipecat golden + tasks golden; the table↔emitter and service-map agreement tests (below). |
| `internal/cli` | L2 | `init`, `validate`, `compile`, `apply`, `dev` against the real command tree. |
| `internal/scaffold` | L3 | `unmute init` writes exactly the v1 package (golden `init.txt`). |
| `internal/tui` | L2 | The interactive `init` wizard (scripted input, no TTY). |

### Command workflows (L2)

```sh
go test ./internal/cli -run TestInit         # scaffold a valid v1 package
go test ./internal/cli -run TestValidate     # per-target validate + warnings + gates
go test ./internal/cli -run TestApply        # managed targets say "not implemented yet"
go test ./internal/cli -run TestDev          # dev command wiring, dotenv parsing
```

### Agreement / invariant tests worth knowing

These are the checks that stop the compiler and the driver from drifting apart:

```sh
# The emitter's declared fields must equal the table's non-gated Pipecat rows
# (compiler T12 / V19): no field can be validate-green yet silently unemitted.
go test ./internal/generate -run TestPipecatEmitterMatchesCapabilityTable

# Every provider the service-map switches can emit must have a serviceInfo
# entry (import + one of extra/dep), so a provider can't fall through unmapped.
go test ./internal/generate -run TestServiceInfoCoversEveryMappedClass

# Pipecat driver-v1 maturity gates (fallback, warm transfer, voicemail, etc.)
go test ./internal/ir -run TestValidatePipecatMaturityGates
```

## Golden files

Regenerate goldens **only after an intentional output change**, then eyeball the
diff before committing. Three independent update flags:

```sh
go test ./internal/scaffold  -update           # init.txt (scaffolded package)
go test ./internal/ir        -update            # resolved-IR compiler golden
go test ./internal/generate  -update-pipecat    # pipecat_v1.txt + pipecat_v1_tasks_bot.py
```

The Pipecat goldens live at
`internal/generate/testdata/golden/pipecat_v1.txt` (the full internal
`safe_core` fixture) and `pipecat_v1_tasks_bot.py` (the tasks, task-group, and
delegate agency levels).

## Smoke tests (L4)

```sh
make smoke        # == go test -tags smoke ./...
```

Opt-in, not part of the default gate. It emits projects from the internal
`safe_core` and `remy` fixtures plus public task-group and local-tool cases.
Using `uv`, it resolves the generated dependencies, imports the generated
Python, and instantiates framework services and agents. If `uv` is unavailable,
the smoke test skips rather than fails.

Run it directly:

```sh
go test -tags smoke ./internal/generate -run TestSmokePipecatV1ServicesInstantiate
```

## End-to-end manual verification

The pipeline is `spec.Load → ir.Build → ir.Validate → generate.Generate`. This
exercises it through the CLI, ending in a `bot.py` that compiles.

```sh
make build
tmp=$(mktemp -d); agent="$tmp/demo"

# 1. Scaffold a v1 package (agent.yaml + instructions.md + targets.yaml + .env.example)
bin/unmute init "$agent"
find "$agent" -maxdepth 1 -type f | sort

# 2. Validate against its declared targets
bin/unmute validate "$agent"          # expect: pipecat-dev  pipecat  pass

# 3. Compile to the resolved target artifacts (writes build/<target>/)
bin/unmute compile "$agent"
find "$agent/build" -type f | sort

# 4. Prove the emitted Python is valid
uv run --no-project python -m py_compile "$agent/build/pipecat-dev/bot.py" \
  && echo "bot.py: valid Python"

# 5. Prove the dependency graph actually resolves (no bogus version pins)
( cd "$agent/build/pipecat-dev" && uv pip compile pyproject.toml >/dev/null )
```

Inspect the deterministic compile report:

```sh
cat "$agent/build/pipecat-dev/compile-report.json"
```

Run the agent locally in the browser (needs keys). The default scaffold uses
SLNG for speech (one `SLNG_API_KEY`) and OpenAI for the LLM:

```sh
cp "$agent/.env.example" "$agent/.env"     # then fill in SLNG_API_KEY + OPENAI_API_KEY
bin/unmute dev "$agent"                     # compiles, runs bot.py, opens the browser client
```

Compile a single target when a package declares several:

```sh
bin/unmute compile "$agent" --target pipecat-dev
```

## Current command surface

Implemented: `init`, `validate`, `compile`, `apply`, `dev`.

- `compile <package-dir> [--target ...]` — code targets (e.g. Pipecat); writes
  `build/<target>/`.
- `apply <package-dir> [--target ...]` — managed (config-plane) targets. Code
  targets are told to use `compile`; managed drivers other than the built-ins
  report "not implemented yet".
- `validate <package-dir> [--target ...]` — per-target pass/fail, warnings to
  stderr (exit 0), gated features fail (exit 1).

## Verification-step cheat sheet

| Step | Command | Passes when |
|---|---|---|
| Format/vet | `make fmt` | no vet errors; files gofmt-clean |
| Lint | `make lint` | `0 issues` |
| Build | `make build` | `bin/unmute --version` prints |
| Unit + golden | `make test` | all packages `ok` |
| Python validity | `make smoke` | `ok` (or skipped if no `uv`) |
| End-to-end | init → validate → compile → py_compile | `bot.py: valid Python` |
