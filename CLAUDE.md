# Unmute CLI

Go CLI that compiles a declarative voice-agent spec into orchestrator-native artifacts. Full design in `docs/ARCHITECTURE.md`; the locked authoring contract in `docs/SCHEMA.md`; per-feature specs in `specs/<nnn>-<slug>/` (GitHub Spec Kit); a get-around guide in `docs/REPO_MAP.md`. **When code and a doc disagree, the doc wins — fix the code or open an issue.**

## Voice contracts
While writing documents or speaking with the user, always use a simple language and simple wording. 

## The one rule
Unmute is written in Go, so you mantian **Go code** but you also write some python code snippets and examples, in python. When you  run python code, always check for ty or ruff issues. 

## Tooling
- Go 1.24 (pin in `go.mod`); `CGO_ENABLED=0` static binary; version stamped at link time, never hardcoded.
- Direct deps — `cobra`, `goccy/go-yaml` (gives line/col on parse errors), `google/jsonschema-go` (**v0.x — pin the exact version, bump deliberately**), and the Charm TUI stack: `charmbracelet/bubbletea` + `bubbles` + `lipgloss` power the interactive console (custom MVU styled with Lip Gloss), while `charmbracelet/huh` v1.0.0 is scoped to the accessible/headless renderer only. **The interactive path imports no `huh`; Lip Gloss is expected there. All color lives in `internal/style` — no color literal anywhere else** (guarded by `internal/style/style_test.go`). Everything else is stdlib. **No new dep for what a few lines of stdlib do — justify any addition in the PR.** No `viper` until a real global config file exists.
- `golangci-lint` from day one (`.golangci.yml`).
- Make targets: `build test smoke lint fmt install`.

## Command rules (cobra) — these are what make commands testable, not suggestions
1. Build the tree with a `newRootCmd()` constructor; **no package-level `var rootCmd`** (fresh tree per call = flag isolation between tests).
2. Write to `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`, **never `fmt.Println`** (a stray Println is invisible to tests — it's a bug).
3. `RunE`, never `Run`. **No `os.Exit` / `log.Fatal` inside a command** — return errors wrapped with `%w`.
4. `os.Exit` lives only in `main.go`. `Execute()` builds the root, prints any error once to stderr, returns the exit code.
- `SilenceUsage` + `SilenceErrors` on the root. Exit codes: `0` ok, `1` error — add more only when a consumer actually reads them. Warnings → stderr + exit 0 (never a silent downgrade).

## IR
Go structs are the schema source for their own surface: `internal/spec` derives the unresolved authoring schema, while `internal/ir` derives the resolved/debug schema. **Do not hand-author `.json` schema files.** Flow: `spec.Load` → `ir.Build` → `ir.Validate` → `generate.Generate`.

## Testing
`go test ./...` runs L1–L3 and needs **zero Python**:
- L1 unit (pure logic, table-driven) · L2 in-process command tests (real tree, capture output) · L3 golden files (`-update` to regenerate).
- L4 smoke (`make smoke`, build tag `smoke`) proves emitted Python is valid — opt-in, needs Python, never in the default suite or PR gate.

## Three places document a change, not one
The generated `build/<target>/README.md` is the runbook, and almost nobody reads it before they have already read the example's page and `docs/`. So **any change to emitted behaviour updates all three in the same commit**: the emitted README template, the source example's own `README.md` under `examples/`, and the relevant page in `docs/`. A fact that is only true in generated output is a fact the reader never sees. Two tests hold the parts that can be held (`internal/generate/examples_test.go`): every example README must name every `transport` its targets declare, and every link into `examples/` from `examples/` or `docs/` must resolve. Prose can still rot, so read the example page before you claim you are done.

## Layout
`internal/` not `pkg/`. One file per command in `internal/cli/`. Hand-write cobra commands — **no `cobra-cli` generator**.

## Skills
- Ponytail for writing great code
- GitHub Spec Kit for SDD (spec driven development) on any feature work: `/speckit-specify` → `specs/<nnn>-<slug>/spec.md`, then `/speckit-plan`, `/speckit-tasks`, `/speckit-implement`. Specs live in `specs/`, never in `docs/`.
- find-docs skill to use context7 cli for searching docs
