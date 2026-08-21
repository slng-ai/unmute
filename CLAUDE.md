# Unmute CLI

Go CLI that compiles a declarative voice-agent spec into orchestrator-native artifacts. `docs/ARCHITECTURE.md` explains the design and points at the load-bearing code. Go structs and `internal/target` own machine behavior; `docs-site/` is the public user guide. Local feature work lives in ignored `specs/<nnn>-<slug>/` directories.

## Voice contracts
While writing documents or speaking with the user, always use a simple language and simple wording. 

## The one rule
Unmute is written in Go, so you maintain **Go code**, but you also write some Python code snippets and examples, in Python. Checked-in Python has to pass `ruff check .` (CI enforces it). Run `ty check` too when you have the provider SDKs installed, otherwise it only reports imports it cannot resolve.

## Tooling
- Go 1.26 (pin in `go.mod`, and keep it on a Go line that still gets security patches, which is the newest two); `CGO_ENABLED=0` static binary; version stamped at link time, never hardcoded.
- Direct deps — `cobra`, `goccy/go-yaml` (gives line/col on parse errors), `google/jsonschema-go` (**v0.x — pin the exact version, bump deliberately**), and the Charm TUI stack: `charmbracelet/bubbletea` + `bubbles` + `lipgloss` power the interactive console (custom MVU styled with Lip Gloss), while `charmbracelet/huh` v1.0.0 is scoped to the accessible/headless renderer only. **The interactive path imports no `huh`; Lip Gloss is expected there. All color lives in `internal/style` — no color literal anywhere else** (guarded by `internal/style/style_test.go`). Everything else is stdlib. **No new dep for what a few lines of stdlib do — justify any addition in the PR.** No `viper` until a real global config file exists.
- `golangci-lint` from day one (`.golangci.yml`).
- Make targets: `build test smoke rig lint fmt install`. `rig` is the credential-free local telephony check: a container runtime and no accounts.

## Command rules (cobra) — these are what make commands testable, not suggestions
1. Build the tree with a `newRootCmd()` constructor; **no package-level `var rootCmd`** (fresh tree per call = flag isolation between tests).
2. Write to `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`, **never `fmt.Println`** (a stray Println is invisible to tests — it's a bug).
3. `RunE`, never `Run`. **No `os.Exit` / `log.Fatal` inside a command** — return errors wrapped with `%w`.
4. `os.Exit` lives only in `main.go`. `Execute()` builds the root, prints any error once to stderr, returns the exit code.
- `SilenceUsage` + `SilenceErrors` on the root. Exit codes: `0` ok, `1` error — add more only when a consumer actually reads them. Warnings → stderr + exit 0 (never a silent downgrade).

## IR
Go structs are the schema source for their own surface: `internal/spec` derives the unresolved authoring schema, while `internal/ir` derives the resolved/debug schema. **Do not hand-author `.json` schema files.** Flow: `spec.Load` → `ir.Build` → `ir.Validate` → `generate.Generate`.

## Testing
`make test` (`go test -race ./...`) runs L1–L3 and needs **zero Python**:
- L1 unit (pure logic, table-driven) · L2 in-process command tests (real tree, capture output) · L3 golden files (`-update` to regenerate).
- L4 smoke (`make smoke`, build tag `smoke`) proves emitted Python is valid — opt-in, needs Python, never in the default suite or PR gate.
- L5 rig (`make rig`, build tag `rig`) runs the local telephony planes end to end against the real emitted agent — opt-in, needs a container runtime and **no accounts**, never in the default suite or PR gate. Deliberately separate from smoke: folding a credential-free check into a target that needs credentials would defeat the reason it exists.
- [`docs/HARNESS_TEST.md`](docs/HARNESS_TEST.md) is the reusable prompt for real end-to-end conversations across the examples.

## A rule with no gate is a wish
Standards here are not taste, they are things CI or a test can fail on. Writing a new rule into this file means wiring its check in the same PR, or tagging it `(advisory)` so it reads as guidance instead of law.

| Rule | What fails |
|---|---|
| gofmt clean, `go vet` clean, `go.mod` tidy | CI `format` |
| `errcheck` `govet` `staticcheck` `ineffassign` `unused` `errorlint` `forbidigo` | `make lint` |
| no `fmt.Print*` in a command, no `os.Exit`/`log.Fatal` outside `main.go` | `forbidigo`, patterns and reasons in `.golangci.yml` |
| errors wrapped with `%w` and matched with `errors.Is`/`As`, never `==` | `errorlint` |
| L1–L3 green, zero Python, no data races | CI `test`, `go test -race ./...` |
| no color literal outside `internal/style` | `internal/style/style_test.go` |
| example READMEs name every transport; every `examples/` link resolves | `internal/generate/examples_test.go` |
| every emitted task prompt names its finish call and an escape, every finish takes the reserved `unserved_request`, and the owner is told to read it | `internal/generate/task_prompt_test.go`, `internal/ir/validate_test.go` |
| every emitted task prompt names its finish call and an escape, every finish takes the reserved `unserved_request`, and the owner is told to read it | `internal/generate/task_prompt_test.go`, `internal/ir/validate_test.go` |
| the skill's tool kinds, vendors, providers and doc pointers match the code | `internal/skill/agreement_test.go` |
| every command and flag the skill names exists | `internal/cli/skill_bundle_test.go` |
| the docs-site CLI pages quote each command's `Usage:` line, not just its flags | `internal/cli/help_capture_test.go` |
| a receiving agent's opening turn withholds its own handoffs, and no emitted tool carries the on-enter flag | `internal/generate/livekit_v1_test.go` |
| an authored `endpointing_delay` reaches LiveKit's `min_delay` and Pipecat's `stop_secs`, and stays absent when unset | `internal/generate/endpointing_delay_test.go`, `internal/ir/validate_test.go` |
| the console's target menus lead with the right target: create with `scaffold.DefaultTarget`, maintain with the package's own | `internal/tui/default_target_test.go` |
| every route is assigned a local plane, and a managed-platform route's manifest, image and env carry nothing plane-only | `internal/target/telephony_test.go`, `internal/generate/cloud_isolation_test.go` |
| a telephony route emits the module its own container command imports, and the records an inbound call needs | `internal/generate/pipecat_sip_test.go` |
| an agent that a dispatch rule cannot dispatch is told about the call instead: the rule names no agent and the plane's server is pointed at the app, on that driver only | `internal/cli/dev_livekit_sip_test.go`, `internal/generate/pipecat_sip_test.go` |
| a local telephony run performs no write outside the machine, on any exit path, transfers included | `internal/generate/pipecat_local_plane_test.go`, `internal/generate/pipecat_cloud_websocket_test.go` |
| the emitted transfer document and the plane's reading of it stay in agreement | `internal/generate` (emitted shape) + `internal/cli` (the reading), one gate each |
| the docs-site transfer table matches the route table, both directions | `internal/docsite/transfer_table_test.go` |
| checked-in Python is clean | CI `python`, `ruff check .` |
| no known-vulnerable dependency or stdlib | CI `vuln`, `govulncheck ./...` |
| release config still builds 6 platforms with version stamps | CI `release-config` |
| emitted Python is valid | `make smoke`, opt-in, never the PR gate |
| the local telephony planes work end to end with no accounts | `make rig`, opt-in, needs a container runtime, never the PR gate |

Ratchet only: a gate that starts failing gets the **code** fixed, not the gate loosened. A disabled check carries its reason inline, the way `.golangci.yml` explains every `errcheck` exclusion and every `forbidigo` pattern, and the single `//nolint` in the tree explains itself at the call site in `main.go`. One rule is still advisory: `ty check` over the examples, because every finding it has today is an unresolved `pipecat`/`livekit` import, and installing those SDKs is `make smoke`'s job.

## Four places document emitted behaviour
The generated `build/<target>/README.md` is the runbook, and almost nobody reads it before they have already read the example's page or the public docs. So **a change to emitted behaviour updates every surface it reaches in the same commit**:
1. the emitted README template,
2. the source example's own `README.md` under `examples/`,
3. the relevant page in `docs-site/`, which is the public answer a reader lands on,
4. **the skill** in `internal/skill/assets/`, which is what a coding assistant reads before it writes a package.

A fact that is only true in generated output is a fact the reader never sees, and a feature the skill does not know about is a feature no coding agent will use. `docs/ARCHITECTURE.md` changes only when a system boundary, compiler stage, or runtime topology changes. Tests hold the parts that can be held: `internal/generate/examples_test.go` (example routes and links), `internal/skill/agreement_test.go` (the skill's factual lists), and `internal/cli/skill_bundle_test.go` (the commands and flags the skill names). Prose can still rot, so read the example page before you claim you are done.

## Layout
`internal/` not `pkg/`. One file per command in `internal/cli/`. Hand-write cobra commands — **no `cobra-cli` generator**.

## Subagent-driven development
For complex or long-running tasks, use subagents by default when the work can be split into independent, bounded subtasks.

- Keep the main agent focused on requirements, decisions, coordination, and final integration.
- Delegate codebase exploration, documentation research, test and log analysis, reviews, and non-overlapping implementation.
- Run independent read-only tasks in parallel.
- Give each subagent a concrete scope and require a concise summary with file references, findings, and verification results.
- Wait for all required subagents before integrating their work.
- Avoid parallel edits to the same files or tightly coupled code paths.
- Handle small or inherently sequential tasks directly.
- If a suitable long-running task is not delegated, briefly state why.

## Skills
- Ponytail for writing great code
- GitHub Spec Kit for SDD (spec driven development) on any feature work: `/speckit-specify` → local `specs/<nnn>-<slug>/spec.md`, then `/speckit-plan`, `/speckit-tasks`, `/speckit-implement`. `specs/` is ignored. `.worktreeinclude` copies the local snapshot into new Codex-managed worktrees; copies do not synchronize afterward.
- find-docs skill to use context7 cli for searching docs
