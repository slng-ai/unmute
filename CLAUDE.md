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
- Make targets: `build test smoke contracts lint fmt install`. `contracts` re-fetches the published SLNG conformance fixtures and fails on a digest mismatch: network, no accounts.

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
- Telephony is verified on a **deployed** agent, against a real carrier. There is no local phone loop, and no test level stands in for one: `unmute dev` is the browser loop and it stops where the phone leg starts.
- [`docs/HARNESS_TEST.md`](docs/HARNESS_TEST.md) is the reusable prompt for real end-to-end conversations across the examples.
- [`docs/SELF_VERIFY.md`](docs/SELF_VERIFY.md) is how to check runtime behaviour **without** a person on the phone, and it is what to do before asking for a live call. Its first rule: find the layer the defect lives in and reproduce it there. A provider defect is usually one HTTP request, so it needs no audio, no tunnel and no simulated caller — [`scripts/replay_router_scopes.py`](scripts/replay_router_scopes.py) is the worked example. Coval evaluates conversations you push to it, so verification never waits on inbound reachability.

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
| every direct dependency is on the allowlist | `internal/cli/deps_test.go` |
| no declaration is reachable only from its own definition | `internal/cli/reachability_test.go` (`deadcode -test ./...`) |
| every declared capability `Field` constant has a row in the table | `internal/target/table_test.go` (`TestEveryFieldConstantHasARow`) |
| every symbol a template names still exists in Go | `internal/scaffold/template_symbols_test.go` |
| every capability row carries a deliberate value for a target `field()` does not seed, and every refusal names what to do instead | `internal/target/table_test.go` |
| the console offers every shipped target, names each correctly, and offers no option validation refuses | `internal/tui/default_target_test.go` |
| the slng target opens no socket to the SLNG agents API | `internal/ir/validate_slng_test.go` |
| every surface naming a `voiceai` command names the same one, agent id included | `internal/target/slng_target_test.go` |
| example READMEs name every transport; every `examples/` link resolves | `internal/generate/examples_test.go` |
| every emitted task prompt names its finish call and an escape, every finish takes the reserved `unserved_request`, and the owner is told to read it | `internal/generate/task_prompt_test.go`, `internal/ir/validate_test.go` |
| the skill's tool kinds, vendors, providers and doc pointers match the code | `internal/skill/agreement_test.go` |
| every command and flag the skill names exists | `internal/cli/skill_bundle_test.go` |
| the docs-site CLI pages quote each command's `Usage:` line, not just its flags | `internal/cli/help_capture_test.go` |
| a receiving agent's opening turn withholds its own handoffs, and no emitted tool carries the on-enter flag | `internal/generate/livekit_v1_test.go` |
| an authored `endpointing_delay` reaches the floor on both targets and never the ceiling, and a package that authors nothing still emits the balanced floor and ceiling rather than inheriting a framework default | `internal/generate/endpointing_delay_test.go`, `internal/ir/validate_test.go` |
| a `pace` reaches the ceiling on both targets, every pace has a complete per-target row, no Pipecat row varies the floor, and `patient` reproduces the framework default | `internal/target/pace_test.go`, `internal/generate/endpointing_delay_test.go` |
| `pace` is refused on a non-turn binding, on an unknown value naming all three, and in a per-target override | `internal/ir/validate_test.go` (`TestValidatePace*`) |
| each emitted runbook names the floor and the ceiling separately, says whether the wait adapts, and on Pipecat distinguishes the two fields both spelled `stop_secs` | `internal/generate/turn_runbook_test.go` |
| `semantic_endpointing: off` removes the turn model on LiveKit and the end-of-turn analyzer on Pipecat, every other value keeps it, and the analyzer imports follow the branch | `internal/generate/semantic_endpointing_test.go` |
| a Pipecat package setting `interruption.minimum_words` keeps its end-of-turn analyzer and its ceiling, so the silent downgrade cannot come back | `internal/generate/semantic_endpointing_test.go` (`TestPipecatEmitsNoSpeechTimeoutUnlessAsked`) |
| a turn field set on the base binding survives a per-target `models:` override, which replaces rather than merges | `internal/generate/semantic_endpointing_test.go` (`TestTurnFieldsSurviveAPerTargetOverride`, over `examples/salon-concierge`) |
| a turn binding carrying `params`, `agent_id` or `fallback` warns that the field reaches nothing, and the same fields on any other role stay quiet | `internal/ir/validate_test.go` (`TestValidateWarnsOnTurnFieldsThatReachNothing`) |
| a router binding's forwardable params ride the request body on both targets and reach no constructor kwarg, while a folded field stays in the construction | `internal/generate/slng_router_test.go` (`TestSlngRouterForwardableParamsRideTheBody`, `TestSlngRouterFoldedFieldsStayInTheConstruction`) |
| an authored `prompt_suffix` reaches every agent and task prompt on its own think profile, no other profile's, and the LiveKit summarizer's; and it moves no cache scope | `internal/ir/build_test.go`, `internal/generate/slng_router_test.go` (`TestSlngRouterSummarizerCarriesThePromptDirective`, over `internal/testdata/summary_core`) |
| every `prompt_suffix` refusal names what to do instead, an off-router think binding warns rather than refusing, and a per-target override cannot name a second value | `internal/ir/validate_test.go` (`TestValidatePromptSuffix*`) |
| the `reasoning_effort` advice fires only on an upstream serving OpenAI's own models, because elsewhere the param is what the host refuses | `internal/ir/validate_test.go` (`TestValidateSlngRouterWarnsOnToolsWithoutReasoningEffort`) |
| the console's target menus lead with the right target: create with `scaffold.DefaultTarget`, maintain with the package's own | `internal/tui/default_target_test.go` |
| every telephony route deploys to a managed platform: every Pipecat route emits a deploy manifest on the platform base image, and no LiveKit route emits one | `internal/generate/cloud_isolation_test.go` |
| an inbound LiveKit SIP route emits the trunk and dispatch-rule records a call needs, and the one command that creates them | `internal/generate/livekit_telephony_setup_test.go` |
| a route that hosts nothing says so everywhere: no tunnel, helper or hosting word in its emitted runbook | `internal/generate/pipecat_cloud_websocket_test.go` |
| the Pipecat image copies every emitted module `bot.py` imports (its COPY lines are named, so a new module is a `ModuleNotFoundError` at startup) | `internal/generate/pipecat_image_imports_test.go` |
| a Pipecat phone route protects the greeting, an authored `protect` overrides it, `[]` suppresses it, and every mute class named is imported | `internal/generate/pipecat_user_mute_test.go` |
| the emitted transfer document and the plane's reading of it stay in agreement | `internal/generate` (emitted shape) + `internal/cli` (the reading), one gate each |
| the docs-site transfer table matches the route table, both directions | `internal/docsite/transfer_table_test.go` |
| the docs-site tool tables match the capability table on all three targets, and the page's own count of execution blocks is right | `internal/docsite/tool_table_test.go` |
| an L4 smoke fixture still compiles the salon package, the emitted Python keeps the names its script calls, and every name a smoke script monkeypatches still exists and is still called the way the stub expects | `internal/generate/smoke_fixture_test.go` (`TestSmokeStubbedNamesExistInTheEmittedModule`) |
| every emitted tool body tags its `config` with its own `tool_type`, because an update PATCH strips `tool_type` and an untagged body then deploys once and 422s forever after | `internal/generate/slng_v1_test.go` (`TestSlngV1ToolConfigCarriesItsUnionTag`) |
| every `voiceai agents push --json` shape decodes into the fields deploy reads, blockers relay every item, detail and dashboard page, an update warns that a push replaces, and a run that printed a problem exits non-zero | `internal/cli/deploy_test.go` |
| a tool sample written into `build/<target>/samples/` survives the next compile | `internal/cli/deploy_test.go` (`TestCompilePreservesToolSamples`) |
| the docs-site states one version, in one snippet, and no page hardcodes a version literal that disagrees | `internal/docsite/version_test.go` |
| the docs-site declares its Markdown surface, and the coding-agents page names all three endpoints | `internal/skill/markdown_surface_test.go` |
| the changelog runs newest first, every entry carries a label, a version and its own release link, the newest matches the version snippet, and no entry keeps a heading, a dash or a commit hash | `internal/docsite/changelog_test.go` |
| checked-in Python is clean | CI `python`, `ruff check .` |
| no known-vulnerable dependency or stdlib | CI `vuln`, `govulncheck ./...` |
| release config still builds 6 platforms with version stamps | CI `release-config` |
| emitted Python is valid | `make smoke`, opt-in, never the PR gate |
| the vendored SLNG conformance fixtures still match what the backend publishes | `make contracts`, opt-in, needs network, never the PR gate |

A note on the third gate, because it looks redundant next to the second and is
not. `deadcode` walks the call graph and cannot see through `text/template`, so
a method called only from a template reads as dead to it, to `go vet`, to the
linter and to the compiler. Deleting one produces a green build and a broken
`unmute init`. An over-engineering audit of this tree proposed deleting six of
them at once. The two gates cover different things; neither replaces the other.

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
- The Coval skills for verifying runtime behaviour: `coval-resources` for the resource model, `configure-metrics` for writing a metric, `build-test-suite` and `distill-test-set` for scenarios, `setup-agent` + `launch-run`/`watch-run`/`get-results` (or `quick-eval`) for the dial-in path, `diagnose` and `debug-traces` for reading a failure. The `coval` CLI those last ones assume is not installed here, so the REST calls in [`docs/SELF_VERIFY.md`](docs/SELF_VERIFY.md) are the working path and the skills are the reference for what to call. Upstream: <https://github.com/coval-ai/coval-external-skills>.
