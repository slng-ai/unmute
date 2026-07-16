# Unmute CLI

Go CLI for authoring portable voice-agent directories and compiling them to
target-native artifacts. Current implemented commands: `unmute init`,
`unmute compile <agent-dir> pipecat`, `unmute compile <agent-dir> slng`,
`unmute apply <agent-dir> slng`, and `unmute dev`.

## Build the Package

Build the CLI binary:

```sh
make build
```

The binary is written to `bin/unmute`:

```sh
bin/unmute --version
bin/unmute --help
```

Install into your Go bin path:

```sh
make install
```

Direct equivalent for local iteration:

```sh
CGO_ENABLED=0 go build -o bin/unmute .
```

## Test Suite

The default gate is pure Go and needs zero Python:

```sh
make test
```

Equivalent direct command:

```sh
go test ./...
```

This runs:

- L1 unit tests for pure logic.
- L2 in-process command tests using the real Cobra tree.
- L3 golden tests for generated files.

Run focused packages:

```sh
go test ./internal/cli
go test ./internal/generate
go test ./internal/scaffold
go test ./internal/ir
go test ./internal/spec
```

Run focused command tests for the workflows documented below:

```sh
go test ./internal/cli -run TestInit
go test ./internal/cli -run TestCompilePipecat
go test ./internal/cli -run TestCompileSLNG
go test ./internal/cli -run TestApply
```

Run focused generator golden tests:

```sh
go test ./internal/generate -run TestGeneratePipecat_golden
go test ./internal/generate -run TestGenerateSLNG_golden
```

Regenerate golden files after an intentional output change:

```sh
go test ./internal/scaffold -update
go test ./internal/generate -update
```

Or regenerate only one generator golden:

```sh
go test ./internal/generate -run TestGenerateSLNG_golden -update
```

Run Pipecat smoke tests:

```sh
make smoke
```

Smoke tests are L4, opt-in, and not part of the default gate. Current smoke
coverage checks generated Pipecat `bot.py` with `uv run --no-project python -m
py_compile`; if `uv` is unavailable, the smoke test skips.

Run lint:

```sh
make lint
```

Format and vet:

```sh
make fmt
```

`make fmt` runs `gofmt -w .` and `go vet ./...`; it can modify Go files.

## Manual CLI Examples

Create a disposable agent root for manual checks:

```sh
tmpdir=$(mktemp -d)
agent="$tmpdir/readme-agent"
```

All examples below work with either `go run .` or the built binary. These use
the built binary:

```sh
make build
```

### Test `init`

Scaffold an agent directory:

```sh
bin/unmute init "$agent"
```

Or run `bin/unmute init` from a directory containing agents to open the interactive
menu. It lists the current-directory agent and immediate child agents from
`project.yaml`; select one to edit Agent prompt, LLM, TTS, STT, Greeting, or Language.
Each submenu has Back, and nothing is written until Create/Save is confirmed.

Inspect the files that `init` created:

```sh
find "$agent" -maxdepth 3 -type f | sort
```

Expected highlights:

```text
<agent>/.env.local
<agent>/project.yaml
<agent>/env/secrets.yaml
<agent>/agent/agent.yaml
<agent>/agent/models/stt.yaml
<agent>/agent/models/llm.yaml
<agent>/agent/models/tts.yaml
<agent>/agent/prompt/identity.md
<agent>/agent/tools/lookup_order.yaml
<agent>/targets/pipecat/pipecat.yaml
```

Automated equivalent:

```sh
go test ./internal/cli -run TestInit
```

### Test `compile ... pipecat`

Compile the agent to a Pipecat generated artifact set:

```sh
bin/unmute compile "$agent" pipecat
```

The scaffolded `lookup_order` tool is portable HTTP config, but Pipecat HTTP
tool invocation is not implemented yet, so this command currently prints a
warning and omits that tool from generated Pipecat code.

Inspect generated files:

```sh
find "$agent/targets/pipecat/generated" -maxdepth 3 -type f | sort
```

Expected highlights:

```text
<agent>/targets/pipecat/generated/.unmute-generated
<agent>/targets/pipecat/generated/bot.py
<agent>/targets/pipecat/generated/compile-report.json
<agent>/targets/pipecat/generated/Dockerfile
<agent>/targets/pipecat/generated/k8s/deployment.yaml
<agent>/targets/pipecat/generated/k8s/secret.yaml
<agent>/targets/pipecat/generated/pcc-deploy.toml
<agent>/targets/pipecat/generated/pyproject.toml
<agent>/targets/pipecat/generated/README.md
```

Inspect the deterministic compile report:

```sh
sed -n '1,120p' "$agent/targets/pipecat/generated/compile-report.json"
```

Automated equivalents:

```sh
go test ./internal/cli -run TestCompilePipecat
go test ./internal/generate -run TestGeneratePipecat_golden
make smoke
```

### Test `compile ... slng`

Compile the same agent to an offline SLNG Voice Agents API payload:

```sh
bin/unmute compile "$agent" slng
```

Inspect the generated payload:

```sh
ls "$agent/targets/slng/generated"
sed -n '1,120p' "$agent/targets/slng/generated/readme-agent.json"
```

`compile ... slng` writes exactly one JSON file named from `project.yaml.name`.
It does not POST to the SLNG API and does not require `SLNG_API_KEY`.

The scaffolded `lookup_order` tool uses `handler.ref: orders`, so SLNG compile
prints a warning and omits it. To test webhook tool rendering, set the ref to an
absolute URL:

```sh
perl -0pi -e 's/ref: orders/ref: https:\/\/tools.example.com\/orders\/lookup/' \
  "$agent/agent/tools/lookup_order.yaml"
bin/unmute compile "$agent" slng
sed -n '1,160p' "$agent/targets/slng/generated/readme-agent.json"
```

Automated equivalents:

```sh
go test ./internal/cli -run TestCompileSLNG
go test ./internal/generate -run TestGenerateSLNG_golden
```

### Test `apply ... slng`

Safe automated test, using a mocked HTTP client and no live API call:

```sh
go test ./internal/cli -run TestApply
```

Live `apply` creates an SLNG managed Voice Agent from the current local spec:

```sh
export SLNG_API_KEY=sk_...
bin/unmute apply "$agent" slng
```

`apply ... slng` renders the same payload as `compile ... slng` in memory, POSTs
it to `https://api.agents.slng.ai/v1/agents`, and prints the API response body.
It does not read, require, or write `targets/slng/generated/*.json`; use
`compile ... slng` first when you want to inspect the payload before creating a
remote agent.

Current `apply ... slng` limits:

```text
- requires SLNG_API_KEY in the process environment
- does not read VOICEAI_API_KEY or dotenv files
- create-only in this slice
- no live diff
- no PATCH/PUT reconciliation
- no runtime deploy
- no API URL flag
```

## Not Implemented Yet

These commands are planned in `SPEC.md`, but should not be used as test commands
until implemented:

```sh
unmute validate <dir>
unmute deploy <dir>
```
