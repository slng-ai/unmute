# Unmute CLI

Go CLI that compiles a declarative voice-agent spec into orchestrator-native artifacts. Full design in `docs/ARCHITECTURE.md`; the locked authoring contract in `docs/SCHEMA.md`; per-feature specs in `docs/spec/`; a get-around guide in `docs/REPO_MAP.md`. **When code and a doc disagree, the doc wins — fix the code or open an issue.**

## Voice contracts
While writing documents or speaking with the user, always use a simple language and simple wording. 

## The one rule
Unmute is written in Go, so you mantian **Go code** but you also write some python code snippets and examples, in python. When you  run python code, always check for ty or ruff issues. 

## Tooling
- Go 1.24 (pin in `go.mod`); `CGO_ENABLED=0` static binary; version stamped at link time, never hardcoded.
- Direct deps — `cobra`, `goccy/go-yaml` (gives line/col on parse errors), `google/jsonschema-go` (**v0.x — pin the exact version, bump deliberately**), and the Charm TUI stack: `charmbracelet/bubbletea` + `bubbles` + `lipgloss` power the interactive console (custom MVU styled with Lip Gloss), while `charmbracelet/huh` v1.0.0 is scoped to the accessible/headless renderer only. **The interactive path imports no `huh`; Lip Gloss is expected there. All color lives in `internal/style` — no color literal anywhere else** (docs/spec/tui.md C14). Everything else is stdlib. **No new dep for what a few lines of stdlib do — justify any addition in the PR.** No `viper` until a real global config file exists.
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

## Layout
`internal/` not `pkg/`. One file per command in `internal/cli/`. Hand-write cobra commands — **no `cobra-cli` generator**.

## Specs
Two kinds of spec, and only one of them is a file we keep.

- **Complex features get a kept spec.** One file per feature in `docs/spec/`, tracked and committed like any other doc. This is the folder for work a future reader has to understand later: a driver, the compiler, the TUI, a surface as wide as variables and secrets. Amend the file when the design changes, and remember the rule at the top of this document: when the code and the spec disagree, the spec wins.
- **Simple work gets no kept spec.** Write the working spec at the repo root as `SPEC.md`, build against it, then let it go. `SPEC.md` is gitignored and **must never be committed**. It is a scratch file, rewritten per feature, and a stale one in git history is worse than none.

If something you started as simple turns out to be worth keeping, move it to `docs/spec/<feature>.md` and commit it there. That move is the only way a spec enters the repo.

## Skills
- Ponytail for writing great code
- Spec to write specs before building
- Build for building out the specs
- find-docs skill to use context7 cli for searching docs
