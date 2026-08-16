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

Green here means the compiler is correct on paper. To hear it, run an example
against the binary you just built: see
[Run an example from the local build](#run-an-example-from-the-local-build).

If all five are clean, the tree is green. `make test` is the required gate;
`make smoke` is opt-in (needs Python via `uv`).

## Prerequisites

- Go 1.26 (pinned in `go.mod`).
- `golangci-lint` for `make lint`.
- `uv` for `make smoke` and any manual `py_compile`
  (https://docs.astral.sh/uv/). The default `make test` gate needs no Python.

Only needed when you run an example for real (see
[Run an example from the local build](#run-an-example-from-the-local-build)):

- Docker with the Compose plugin. Local development runs in Docker Compose,
  both for `unmute dev` in the browser and for `unmute dev --telephony`.
- `cloudflared`, for `unmute dev --telephony` only. macOS: `brew install
  cloudflared`.
- Node 20 or newer, for the docs site preview (`make docs`).

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
| `internal/cli` | L2 | `init`, `validate`, `compile`, `dev` against the real command tree. |
| `internal/scaffold` | L3 | `unmute init` writes exactly the v1 package (golden `init.txt`). |
| `internal/tui` | L2 | The interactive `init` wizard (scripted input, no TTY). |

### Command workflows (L2)

```sh
go test ./internal/cli -run TestInit         # scaffold a valid v1 package
go test ./internal/cli -run TestValidate     # per-target validate + warnings + gates
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

# A warm transfer dials its destination, so it cannot be declared on a channel
# with outbound: false (livekit-human-transfer V15 / SCHEMA N30)
go test ./internal/ir -run TestV12_WarmTransferRequiresOutboundDirection
```

**Repo-hygiene checks read git, not the working tree.**
`TestPublicExamplePackages` forbids *committed* build artifacts under
`examples/`, and asks `git ls-files` to find them. It used to walk the
filesystem, which meant compiling an example locally, the normal way to check
that an example works, broke `go test ./...` until you deleted your own output.
A hygiene rule about what is committed must be written against what is
committed.

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
exercises it through the CLI, ending in an `agent.py` that compiles.

```sh
make build
tmp=$(mktemp -d); agent="$tmp/demo"

# 1. Scaffold a v1 package (agent.yaml + instructions.md + targets.yaml + .env.example)
bin/unmute init "$agent"
find "$agent" -maxdepth 1 -type f | sort

# 2. Validate against its declared targets
bin/unmute validate "$agent"          # expect: ✓ livekit (livekit), warning on stderr, exit 0

# 3. Compile to the resolved target artifacts (writes build/<target>/)
bin/unmute compile "$agent"
find "$agent/build" -type f | sort

# 4. Prove the emitted Python is valid
uv run --no-project python -m py_compile "$agent/build/livekit/agent.py" \
  && echo "agent.py: valid Python"

# 5. Prove the dependency graph actually resolves (no bogus version pins)
( cd "$agent/build/livekit" && uv pip compile pyproject.toml >/dev/null )
```

`unmute init` scaffolds a LiveKit package, and a scaffolded package declares
exactly one target instance named after its provider (SCHEMA N15), so the
instance is `livekit` and compile writes `build/livekit/` with `agent.py` as the
entry point. Pipecat is still fully supported: run `bin/unmute init` with no
name to pick it in the interactive picker, and the instance is then `pipecat`
with `bot.py` as the entry point.

Inspect the deterministic compile report:

```sh
cat "$agent/build/livekit/compile-report.json"
```

Run the agent locally in the browser (needs keys). The scaffolded `.env.example`
holds five names: `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `LIVEKIT_URL`,
`OPENAI_API_KEY` and `SLNG_API_KEY`. `unmute dev` supplies the three LiveKit
values itself for a local run, so only the two model keys need filling in: SLNG
for speech, OpenAI for the LLM.

```sh
cp "$agent/.env.example" "$agent/.env"     # then fill in SLNG_API_KEY + OPENAI_API_KEY
bin/unmute dev "$agent"                     # compiles, runs agent.py, opens the browser client
```

A scaffolded package declares one target, so step 3 needs no `--target`. Pass
`--target <name>` only against a package that declares several, like the ones
under `examples/`.

## Run an example from the local build

Every directory under `examples/` is a real package. Running one against the
binary you just built is how a compiler change becomes visible in a live call
instead of only in a golden file. The loop is: build, pick an example, give it
keys, validate, compile, talk to it.

### 1. Build and pick an example

```sh
make build
UNMUTE="$PWD/bin/unmute"        # absolute path, so it still works after `cd`
EXAMPLE=salon-support           # any directory under examples/
cd "examples/$EXAMPLE"
```

`bin/unmute` is a relative path, so it stops resolving the moment you `cd` into
the example. Capture it as an absolute path first, or stay at the repository
root and pass `examples/$EXAMPLE` as the argument to every command instead.

Start with `salon-support`: browser audio, local Python tools, no carrier and no
third service, so two model keys is the whole setup. `examples/README.md` says
what each of the other packages teaches.

### 2. Give the example its keys

`unmute dev` reads `.env` from the current directory first, then the package
root, and later files win. Working inside the example directory makes those the
same file, so one copy is enough:

```sh
cp ../../.env .env              # or ../../.env.example, then fill it in
```

The repository-root `.env.example` is a menu of names for every example. The
exact list for one target is written by compile to
`build/<target>/.env.example`. `.env` is gitignored at every level, so a
filled-in copy is never committed.

Validate and compile need no keys at all: a package stores environment variable
names, never values. Only `dev` reads real credentials.

### 3. Validate and compile

```sh
"$UNMUTE" validate .
"$UNMUTE" compile .
find build -type f | sort
```

`validate` prints one line per declared target and exits 0. Warnings go to
stderr and stay exit 0; a gated feature fails and exits 1. `compile` writes
`build/<target>/` for every target, one directory each, plus the deterministic
`build/<target>/compile-report.json`.

Compiling here is safe for git: `examples/*/build/` is gitignored, and the
hygiene test that forbids committed build output under `examples/` asks
`git ls-files`, not the filesystem.

Pick one target when the package declares several:

```sh
"$UNMUTE" compile . --target pipecat
```

### 4. Talk to it: web, telephony

Two ways to run, one flag apart. Both compile first, so there is no need to run
`compile` before `dev`, and both need Docker. Both take `--target <name>`,
which is required when the package declares more than one target and you have
no TTY to pick from.

**Web (the default).** Needs Docker.

```sh
"$UNMUTE" dev . --target pipecat
```

It builds and starts the deployable container, serves one WebRTC page on
`http://localhost:8765`, and opens the browser. Both providers use the same
page. Useful flags:

```sh
"$UNMUTE" dev . --target pipecat --port 8900 --bot-port 7900
"$UNMUTE" dev . --target pipecat --no-open      # print the URL, open nothing
"$UNMUTE" dev . --target pipecat --verbose      # follow container logs on stderr
```

Without `--verbose` the container and agent logs go to the log file only, and
the run prints where it is.

**Telephony.** Needs Docker, `cloudflared`, and real carrier credentials. Only
packages with a phone channel and a resolvable local route can run it.

```sh
# inbound: the CLI opens a tunnel, points the number's webhook at it, and
# prints `call +1...`. ctrl-c restores the number's previous configuration.
"$UNMUTE" dev . --telephony --target pipecat

# outbound: the agent dials you
"$UNMUTE" dev . --telephony --target livekit --to +15551234567

# supply the public origin yourself instead of the managed tunnel
"$UNMUTE" dev . --telephony --target pipecat --public-url https://your.host

# leave the carrier's webhook configuration untouched and point it yourself
"$UNMUTE" dev . --telephony --target pipecat --no-webhook
```

`--public-url`, `--to` and `--no-webhook` all require `--telephony`. Each of
those is rejected before any tunnel, Docker or carrier call happens.

Which example runs which telephony route:

| Example | Target | Route | Local `dev --telephony` |
|---|---|---|---|
| `twilio-telephony-hello` | `pipecat` | `cloud-websocket`, Twilio | inbound, yes. Outbound runs against the deployed agent, so `--to` refuses |
| `twilio-telephony-hello` | `livekit` | `sip`, Twilio | outbound with `--to`, yes. Inbound is a deploy exercise |
| `pipecat-human-transfer-twilio` | `pipecat` | `cloud-websocket`, Twilio | yes |
| `livekit-human-transfer` | `livekit` | `sip`, Twilio | yes |
| `outbound-reminder` | `pipecat` / `livekit` | `carrier-websocket` / `connector`, Twilio | yes |
| `pipecat-human-transfer-daily` | `pipecat` | `daily-sip` | no. Daily carries the call to a deployed agent, so the command refuses by name and points you at the browser mode or the emitted README |
| everything else | any | no phone channel | no. The command says the target has no resolved telephony route |

Every other example runs in web mode. Per-package detail, including
what to expect on a real call, is in each example's own `README.md`.

**Seed input variables** for a package that reads a dispatch payload, such as
`outbound-reminder`:

```sh
"$UNMUTE" dev . --target pipecat --var customer_id=cus_1042 --var name=Ada
```

`--var` is repeatable and is the local stand-in for the dispatch payload. It
works in all three modes.

### 5. Sweep every example

Validate and compile the whole set before claiming a compiler change is safe.
From the repository root:

```sh
make build
for d in examples/*/; do
  echo "== $d"
  bin/unmute validate "$d" || echo "FAILED: $d"
done
for d in examples/*/; do bin/unmute compile "$d" >/dev/null || echo "FAILED: $d"; done
```

There is also an automated version of this, one layer past the smoke tests.
`TestExampleSweep` (L5) compiles every example, compares the result against a
baseline tree byte for byte, then drives each browser-runnable example through
`unmute dev` until the agent's voice actually arrives. Like L4 it is opt-in,
behind its own build tag, and out of the PR gate:

```sh
go test -tags sweep ./internal/cli/... -run TestExampleSweep -timeout 90m
```

It needs `ruff`, a baseline tree in `UNMUTE_SWEEP_BASELINE`, and real keys; it
blocks rather than fails when its tooling is absent. Telephony examples are
compile-only there, so a carrier route is still tested by hand with the
`--telephony` commands above. Contract:
`specs/011-complexity-cleanup/contracts/sweep-report.md`.

## Preview the docs site

The public documentation in `docs-site/` is a Mintlify project. Preview it with
Node 20 or newer:

```sh
make docs        # == cd docs-site && npx --yes mint dev --no-open
```

It serves http://localhost:3000. The CLI is named `mint` now; `mintlify dev`
still works as an alias on older installs, but `mint` is what the Makefile and
`docs-site/README.md` use.

Run the two checks before committing a docs change:

```sh
cd docs-site
npx --yes mint validate        # configuration and page checks
npx --yes mint broken-links    # every internal link resolves
```

Install it once if you would rather not go through `npx` each time:

```sh
npm i -g mint
cd docs-site && mint dev
```

Pages are `.mdx` files; the sidebar order is `navigation.groups` in
`docs-site/docs.json`, and a new page ships only when it is listed there. The
rules the site is written under are in `docs-site/README.md`.

## Current command surface

Implemented: `init`, `validate`, `compile`, `dev`.

- `compile [package-dir] [--target ...]` — code targets (e.g. Pipecat); writes
  `build/<target>/`.
- `validate [package-dir] [--target ...]` — per-target pass/fail, warnings to
  stderr (exit 0), gated features fail (exit 1).

`validate`, `compile` and `dev` all take the package directory as an optional
argument. With no argument they use the current directory, so you can `cd` into
a package and run them bare. An explicit path still works and still wins, which
is what every command run from the repository root against `examples/` uses.

## Verification-step cheat sheet

| Step | Command | Passes when |
|---|---|---|
| Format/vet | `make fmt` | no vet errors; files gofmt-clean |
| Lint | `make lint` | `0 issues` |
| Build | `make build` | `bin/unmute --version` prints |
| Unit + golden | `make test` | all packages `ok` |
| Python validity | `make smoke` | `ok` (or skipped if no `uv`) |
| End-to-end | init → validate → compile → py_compile | `agent.py: valid Python` |
| Example, web | `bin/unmute dev examples/salon-support --target pipecat` | the page at `:8765` connects and the agent answers |
| Example, telephony | `bin/unmute dev --telephony examples/twilio-telephony-hello --target pipecat` | the printed number rings and the agent greets you |
| Docs site | `make docs` | http://localhost:3000 serves; `mint broken-links` is clean |
