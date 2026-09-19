# Unmute CLI

Go CLI that compiles a declarative voice-agent spec into orchestrator-native artifacts. `docs/ARCHITECTURE.md` explains the design and points at the load-bearing code. Go structs and `internal/target` own machine behavior; `docs-site/` is the public user guide. Local feature work lives in ignored `specs/<nnn>-<slug>/` directories.

## Voice contracts
While writing documents or speaking with the user, always use a simple language and simple wording. 

## The one rule
Unmute is written in Go, so you maintain **Go code**, but you also write some Python code snippets and examples, in Python. Checked-in Python has to pass `ruff check .` (CI enforces it). Run `ty check` too when you have the provider SDKs installed, otherwise it only reports imports it cannot resolve.

## Tooling
- Go 1.26 (pin in `go.mod`, and keep it on a Go line that still gets security patches, which is the newest two); `CGO_ENABLED=0` static binary; version stamped at link time, never hardcoded.
- Direct deps — `cobra`, `goccy/go-yaml` (gives line/col on parse errors), `google/jsonschema-go` (**v0.x — pin the exact version, bump deliberately**), and the Charm TUI stack: `charmbracelet/bubbletea` + `bubbles` + `lipgloss` power the interactive console (custom MVU styled with Lip Gloss), while `charmbracelet/huh` v1.0.0 is scoped to the accessible/headless renderer only. **The interactive path imports no `huh`; Lip Gloss is expected there. All color lives in `internal/style` — no color literal anywhere else** (guarded by `internal/style/literal_test.go`, which walks every Go string literal in the tree). Everything else is stdlib. **No new dep for what a few lines of stdlib do — justify any addition in the PR.** No `viper` until a real global config file exists.
- `golangci-lint` from day one (`.golangci.yml`).
- Make targets: `build test smoke contracts lint fmt install`. `contracts` re-fetches the published SLNG conformance fixtures and fails on a digest mismatch: network, no accounts.

## Command rules (cobra) — these are what make commands testable, not suggestions
1. Build the tree with a `newRootCmd()` constructor; **no package-level `var rootCmd`** (fresh tree per call = flag isolation between tests).
2. Write to `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`, **never `fmt.Println`** (a stray Println is invisible to tests — it's a bug).
3. `RunE`, never `Run`. **No `os.Exit` / `log.Fatal` inside a command** — return errors wrapped with `%w`.
4. `os.Exit` lives only in `main.go`. `Execute()` builds the root, prints any error once to stderr, returns the exit code.
- `SilenceUsage` + `SilenceErrors` on the root. Exit codes: `0` ok, `1` error — add more only when a consumer actually reads them. Warnings → stderr + exit 0 (never a silent downgrade).

### A command says what it did, and what somebody has to fix
Nothing else. `unmute --version` is one line, `validate` is its result rows,
`compile` is the list of files it wrote, and `init` is the list of files it
created. A warning, a prerequisite or an error is added to that; a description
of the output is not, because the output is on disk and
`build/<target>/compile-report.json` is next to it.

This is not taste, it is what makes the warnings readable. Every forwarded
binding, derived worker count and resolved route used to print on every run
alongside eight advisory notes about how a framework works, so twenty-odd lines
of nothing-to-do buried the two lines that said a secret was undeclared and a
tool call would 400. **A new line on stdout has to name something the reader
did or has to do.** A new `Warn` row in the capability table has to name a
difference the author can act on, and the table is allowed exactly one
(`internal/target/table_test.go`), so adding a second is a deliberate decision
and not a place to park a note to self.

## IR
Go structs are the schema source for their own surface: `internal/spec` derives the unresolved authoring schema, while `internal/ir` derives the resolved/debug schema. **Do not hand-author `.json` schema files.** Flow: `spec.Load` → `ir.Build` → `ir.Validate` → `generate.Generate`.

### Typed session state
A variable's `type:` is a single-line Python type expression, and `shapes:` is
a top-level **list** of named field groups it can refer to. A task's `assign:`
derives its finish fields from the destination variables, so authors declare
each type once. The grammar and error column live in
`internal/spec/typeexpr.go`; the vocabulary and advice live in
`internal/ir/shapes.go`. The shared emitted Python lives in
`internal/generate/shapes.go` and is inserted into both code targets verbatim.

No saved value enters a prompt automatically. A prompt grants access by naming
`{{variable}}` or `{{variable.field}}`. Omitted task and handoff history means
spoken `messages`; `reset` receives no old conversation. Task return restores
the owner's earlier context and adds only a completed or unserved status.

### No dictionaries in the authoring surface
**A new authored field is a list, never a map.** An entry with a name carries the name as a field (`- name: caller`); a mapping from one name to one value is a list whose every item holds exactly one key (`- customer_phone: result.value`), decoded by an `UnmarshalYAML` into a struct so no `map[...]` reaches the Go type. Two reasons, and the second is the one that bites: a map has no order a reader can see, so a file that lists three things says nothing about which runs first; and a map field cannot carry a per-entry comment where anybody will find it.

This is a **ratchet**, not a migration. Every map field that exists today is on the allowlist in `internal/spec/no_dictionaries_test.go`, in two sections that mean different things:
- **Permanent.** `input:`, `output:` and `params:` are JSON Schema or provider passthrough. A JSON Schema object *is* a dictionary; converting it would stop it being one. These never move.
- **Debt.** The rest carry a one-line reason and a "migrate when". This section may shrink. It must never grow.

Adding a map-typed authored field fails `TestNoNewDictionaryInTheAuthoringSurface`, which names the field and says to write a list. `internal/ir` is out of scope: it is the resolved shape, not something a person writes.

## Testing
`make test` (`go test -race ./...`) runs L1–L3 and needs **zero Python**:

**Run the package you touched, not the tree.** `go test ./internal/generate/`
is 5s where `./...` is 37s and `make test` is 52s, and `internal/cli` alone is
47 of those seconds. Go caches a package whose inputs have not changed, so a
rerun of the whole tree is 3s, but a **failing** package is never cached and
pays full price on every run. `-race` is the pre-push check, not the inner
loop: it costs 15s and nothing in a golden diff is concurrent. Run a long one
in the background and read the result when it lands. What a test run costs a
reader is its output, not its duration: a passing tree is twelve lines and one
broken gate can be hundreds.

- L1 unit (pure logic, table-driven) · L2 in-process command tests (real tree, capture output) · L3 golden files (`-update` to regenerate).
- L4 smoke (`make smoke`, build tag `smoke`) proves emitted Python is valid — opt-in, needs Python, never in the default suite or PR gate.
- Telephony is verified on a **deployed** agent, against a real carrier. There is no local phone loop, and no test level stands in for one: `unmute dev` is the browser loop and it stops where the phone leg starts.
- Before asking for a live call, find the layer the defect lives in and reproduce it there. A provider defect is usually one HTTP request, so it needs no audio, no tunnel and no simulated caller: [`scripts/replay_router_scopes.py`](scripts/replay_router_scopes.py) is the worked example. A prompt or seam defect lives in the model's turns, and [`scripts/text_run_livekit.py`](scripts/text_run_livekit.py) drives the emitted LiveKit agent through a scripted text conversation with the real model and real local tools, no audio.
- After somebody runs `unmute dev` and talks to the agent, read the call back yourself with [`scripts/read_langfuse_trace.py`](scripts/read_langfuse_trace.py): transcript, tool calls and per-span latency, newest trace by default. Needs the package on `tracing.provider: langfuse`, which `examples/salon-concierge` is. Never describe a call from what you were told about it when the spans are one command away. A call is one trace and one session, with a `turn` span per exchange inside it. Add `--check-v4` after any change to tracing: it fails the run when the call splits into several traces, when its root carries no conversation, when a turn recorded the caller and not the reply, when a turn sits in the trace but outside its root, or when an observation is missing the session ID or trace name, none of which is visible in the Langfuse UI.

## Commits and pull requests (advisory)
Plain words, short. Write it the way you would say it out loud, not the way a
release note reads. A pull request body is two parts and nothing else:

- **What's in** — the shape an author writes, in a fence, and one paragraph on
  what it does. What was wrong before, if it takes a line.
- **How it works** — five to ten lines. The decisions somebody would otherwise
  ask about.

The reasoning that filled a body belongs where a reader finds it later: in the
code comment beside the thing it explains, or in the commit message. A body that lists every gate is a body nobody reads.

**No tool attribution, no co-author trailer and no session link**, in a commit
message or a pull request. Who wrote it is the author field's job.

## A rule with no gate is a wish
Standards here are not taste, they are things CI or a test can fail on. Writing
a new rule into this file means wiring its check in the same PR, or tagging it
`(advisory)` so it reads as guidance instead of law. Ratchet only: a gate that
starts failing gets the **code** fixed, not the gate loosened, and a disabled
check carries its reason inline, the way `.golangci.yml` explains every
`errcheck` exclusion and every `forbidigo` pattern.

There is no written list of the gates. The tests are the list: when one fails,
read the test that failed.

## Four places document emitted behaviour
The generated `build/<target>/README.md` is the runbook, and almost nobody reads it before they have already read the example's page or the public docs. So **a change to emitted behaviour updates every surface it reaches in the same commit**:
1. the emitted README template,
2. the source example's own `README.md` under `examples/`,
3. the relevant page in `docs-site/`, which is the public answer a reader lands on,
4. **the skill** in `internal/skill/assets/`, which is what a coding assistant reads before it writes a package.

A fact that is only true in generated output is a fact the reader never sees, and a feature the skill does not know about is a feature no coding agent will use. The reverse rots too, and now fails: `internal/docsite/retired_output_test.go` refuses a page, a skill reference or an example README that quotes a line the CLI has stopped printing, because a reader who copies a stale sample and waits for it cannot tell a stale doc from a broken install. `docs/ARCHITECTURE.md` changes only when a system boundary, compiler stage, or runtime topology changes. Tests hold the parts that can be held: `internal/generate/examples_test.go` (example routes and links), `internal/skill/agreement_test.go` (the skill's factual lists), and `internal/cli/skill_bundle_test.go` (the commands and flags the skill names). Prose can still rot, so read the example page before you claim you are done.

## Layout
`internal/` not `pkg/`. One file per command in `internal/cli/`. Hand-write cobra commands — **no `cobra-cli` generator**.

### Three places hold a package, for three different reasons
`examples/` is what a reader is sent to, so it carries every example gate: a
README naming its transports, resolving links, the model and framework pins, and
a hardcoded set in `internal/generate/examples_test.go` that a new one has to
join deliberately. `internal/testdata/` is the smallest package that makes one
unit assertion possible. `internal/voice-agents-tests/` is a whole agent we
compile, deploy and talk to, to find what only a real call finds: not shipped,
nobody pointed at it, and held to one bar (validates clean and generates on every
target it declares) so a test agent that stops running fails the suite. Putting a
test agent in `examples/` is what this split exists to stop, because it makes the
public set grow with work nobody outside is meant to read. Two of those agents
are a pair: `salon-concierge-v2` is frozen at the state that closed typed
session state and is the control, and `salon-concierge-v3` exercises explicit
saved-value sharing across reset tasks, so a regression is measured against a
package known to work.

## Feature work
Plan before editing. Plan mode (`Shift+Tab`) writes the plan to a file that is
re-injected after each compaction, which is what a change reaching across
`internal/spec`, `internal/ir` and `internal/generate` needs to stay coherent.
Notes and the written plan live in ignored `specs/<nnn>-<slug>/`, which
`.worktreeinclude` copies into a new worktree; copies do not synchronize
afterward.

## Subagents
Delegate bounded, independent work: codebase exploration, documentation
research, test and log analysis, reviews. Run read-only ones in parallel, give
each a concrete scope, and require a summary with file references. Never let
two agents edit the same files or tightly coupled code paths. Small or
inherently sequential work is faster done directly.

## Skills
- **ponytail** shapes what gets built: the laziest thing that works, stdlib before a dependency, deletion before addition.
- **find-docs** for any library, framework, SDK or CLI question, so an answer comes from current docs rather than model memory.
- **The Coval skills** for verifying runtime behaviour: `coval-resources` for the resource model, `configure-metrics` for writing a metric, `build-test-suite` and `distill-test-set` for scenarios, `setup-agent` + `launch-run`/`watch-run`/`get-results` (or `quick-eval`) for the dial-in path, `diagnose` and `debug-traces` for reading a failure. The `coval` CLI those last ones assume is not installed here, so call the REST API directly and read the skills as the reference for what to call. Upstream: <https://github.com/coval-ai/coval-external-skills>.
- **langfuse**, **livekit-agents**, **voice-agent-prompting** and **python-dev** are the rest of the kept set. Everything else is disabled on purpose: the skill listing has a character budget, and a long one drops descriptions until the skills you do use stop matching.
