# Unmute CLI

Go CLI that compiles a declarative voice-agent spec into orchestrator-native artifacts. Full design in `docs/ARCHITECTURE.md`; engineering detail in `docs/DEVELOPMENT.md`; glossary in `CONTEXT.md`; locked decisions in `docs/adr/`. **When code and a doc disagree, the doc wins — fix the code or open an issue.**

## The one rule
Unmute is written in Go, so you mantian **Go code** but you also write some python code snippets and examples, in python. When you  run python code, always check for ty or ruff issues. 

## Tooling
- Go 1.24 (pin in `go.mod`); `CGO_ENABLED=0` static binary; version stamped at link time, never hardcoded.
- Direct deps — `cobra`, `goccy/go-yaml` (gives line/col on parse errors), `google/jsonschema-go` (**v0.x — pin the exact version, bump deliberately**), and `charmbracelet/huh` v1.0.0 for the console. Huh intentionally brings its terminal UI graph; the only sanctioned direct Bubble Tea import is `internal/tui/shell.go`, whose single persistent `tea.Program` owns the console alt-screen and hosts the huh forms. Do not import Bubble Tea anywhere else or Lip Gloss directly. Everything else is stdlib. **No new dep for what a few lines of stdlib do — justify any addition in the PR.** No `viper` until a real global config file exists.
- `golangci-lint` from day one (`.golangci.yml`).
- Make targets: `build test smoke lint fmt install`.

## Command rules (cobra) — these are what make commands testable, not suggestions
1. Build the tree with a `newRootCmd()` constructor; **no package-level `var rootCmd`** (fresh tree per call = flag isolation between tests).
2. Write to `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`, **never `fmt.Println`** (a stray Println is invisible to tests — it's a bug).
3. `RunE`, never `Run`. **No `os.Exit` / `log.Fatal` inside a command** — return errors wrapped with `%w`.
4. `os.Exit` lives only in `main.go`. `Execute()` builds the root, prints any error once to stderr, returns the exit code.
- `SilenceUsage` + `SilenceErrors` on the root. Exit codes: `0` ok, `1` error — add more only when a consumer actually reads them. Warnings → stderr + exit 0 (never a silent downgrade).

## IR
Go structs are the schema source for their own surface: `internal/spec` derives the unresolved authoring schema, while `internal/ir` derives the resolved/debug schema.

## Layout
`internal/` not `pkg/`. One file per command in `internal/cli/`. Hand-write cobra commands — **no `cobra-cli` generator**.
