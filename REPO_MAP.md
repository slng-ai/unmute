# Repo map: the files you actually need

A get-around guide to `unmute_cli`. Not exhaustive — the load-bearing files,
grouped by what you're trying to do. For the *why* behind the design, see
[CONTEXT.md](CONTEXT.md) (vocabulary), [SCHEMA.md](SCHEMA.md) (the locked spec),
and [CLAUDE.md](CLAUDE.md) (engineering rules).

## The one you asked about: provider integrations

**Provider integrations (Deepgram, AssemblyAI, ElevenLabs, Cartesia, SLNG, …)
live in Go, not YAML.** One entry per `(framework, role, provider)`:

| File | What it holds |
|---|---|
| [internal/target/catalog.go](internal/target/catalog.go) | The `Entry` / `CallSpec` types, `Lookup`, `Vendors`, and `CheckVendor` (the one vendor/endpoint rulebook shared by validation and codegen) |
| [internal/target/catalog_pipecat.go](internal/target/catalog_pipecat.go) | Pipecat entries: deepgram, assemblyai, openai, slng (listen); elevenlabs, cartesia, openai, slng (speak); openai + wildcard (reason) |
| [internal/target/catalog_livekit.go](internal/target/catalog_livekit.go) | LiveKit entries: deepgram, slng (listen); elevenlabs, cartesia, slng (speak); Inference wildcard (reason) |
| [internal/target/catalog_deepgram.go](internal/target/catalog_deepgram.go), [catalog_elevenlabs.go](internal/target/catalog_elevenlabs.go) | Managed-target matrix rows (allowlists only, no code injection) |

**Why Go and not YAML:** CLAUDE.md locks "Go structs are the schema source";
an entry is only valid against a driver's templates and pinned version range,
so data and templates version together in one repo. A user-supplied
`providers.yaml` overlay is designed but not built (see
[PROVIDER_CATALOG.md](PROVIDER_CATALOG.md) §5.2).

**To add a provider:** append one entry, run
`go test ./internal/generate -run TestCatalogResolutionGolden -update-catalog`,
eyeball the diff. Full recipe in [PROVIDER_CATALOG.md](PROVIDER_CATALOG.md) §5.1.
The user-facing table of what each provider accepts and emits is
[docs/user/reference/providers.md](docs/user/reference/providers.md).

## The pipeline (how a spec becomes an artifact)

The flow is `spec.Load → ir.Build → ir.Validate → generate.Generate`:

| Stage | File | Role |
|---|---|---|
| entry | [main.go](main.go) | `os.Exit(cli.Execute(version))`, nothing else |
| commands | [internal/cli/root.go](internal/cli/root.go) | builds the cobra tree; `init`, `validate`, `compile`, `apply`, `dev` |
| load | [internal/spec/load.go](internal/spec/load.go), [schema.go](internal/spec/schema.go) | read the package dir → unresolved authoring structs |
| build | [internal/ir/build.go](internal/ir/build.go), [compiler.go](internal/ir/compiler.go) | resolve into the IR (`ir.Agent`, `ir.Target`, `ir.Binding`) |
| validate | [internal/ir/validate.go](internal/ir/validate.go) | check spec against the capability table + provider catalogue |
| generate | [internal/generate/artifact.go](internal/generate/artifact.go) | `Generate` validates once, then dispatches to one driver |

## The drivers (IR → runnable output)

Each driver is a `build.go` (IR → template model), a thin `.go` (types +
entry point + version check), and `templates/`:

| Target | Build | Templates | Notes |
|---|---|---|---|
| Pipecat | [pipecat_v1_build.go](internal/generate/pipecat_v1_build.go) | [templates/pipecat_v1/](internal/generate/templates/pipecat_v1/) (`bot.py.tmpl`) | code target |
| LiveKit | [livekit_v1_build.go](internal/generate/livekit_v1_build.go) | [templates/livekit_v1/](internal/generate/templates/livekit_v1/) (`agent.py.tmpl`) | code target |
| ElevenLabs | [elevenlabs.go](internal/generate/elevenlabs.go) | none (builds an `ApplyPlan`, JSON to the API) | managed target |

**Shared codegen glue:** [internal/generate/service_call.go](internal/generate/service_call.go)
turns `catalogue entry + binding` into a rendered constructor (`ServiceCall`),
so the templates never branch on provider. This is where Deepgram-vs-SLNG-vs-
ElevenLabs constructor differences get resolved. Vapi and Deepgram drivers are
not built yet (they fail loud).

## The capability system (what compiles where)

| File | Role |
|---|---|
| [internal/target/table.go](internal/target/table.go) | the core/warn/gated/provisional matrix: per-field, per-provider support + history/roles/fallback |
| [SCHEMA.md](SCHEMA.md) | the human-readable truth the table encodes; **when code and this disagree, the doc wins** |
| [docs/spec/driver-*.md](docs/spec/) | per-driver contracts (`§C` constraints, `§V` invariants, `§T` tasks, `§B` bugs) |

## Tests worth knowing

| File | What it guards |
|---|---|
| [internal/target/catalog_test.go](internal/target/catalog_test.go) | catalogue invariants; `TestSlngEverywhere`; `TestCheckVendor` |
| [internal/generate/catalog_golden_test.go](internal/generate/catalog_golden_test.go) | every entry's emitted call, pinned in `testdata/golden/catalog_resolution.txt` |
| [internal/generate/pipecat_v1_test.go](internal/generate/pipecat_v1_test.go), [livekit_v1_test.go](internal/generate/livekit_v1_test.go) | per-driver goldens + provider assertions |
| [internal/generate/pipecat_v1_smoke_test.go](internal/generate/pipecat_v1_smoke_test.go) | L4 smoke (build tag `smoke`): real `uv` install, import, instantiate every service |

Golden outputs live in [internal/generate/testdata/golden/](internal/generate/testdata/golden/).
`catalog_resolution.txt` is the single best file to *read* to see exactly what
every provider binding emits.

## Examples (real specs to compile)

- [examples/README.md](examples/README.md) — runnable LiveKit/Pipecat matrix
  for a simple prompt, one task, a task group, and sub-agent handoffs.
- [examples/safe_core/](examples/safe_core/) — the five-target portable
  example; every primary validates.
- [examples/remy/](examples/remy/) — handoff + task groups on LiveKit (the
  driver golden fixture).

## Docs

- [docs/user/](docs/user/README.md) — user-facing reference, one page per `agent.yaml` block + targets + providers + CLI.
- [PROVIDER_CATALOG.md](PROVIDER_CATALOG.md) — the catalogue's design, findings, and how-to-extend recipe.
