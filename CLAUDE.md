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

### No dictionaries in the authoring surface
**A new authored field is a list, never a map.** An entry with a name carries the name as a field (`- name: caller`); a mapping from one name to one value is a list whose every item holds exactly one key (`- customer_phone: result.value`), decoded by an `UnmarshalYAML` into a struct so no `map[...]` reaches the Go type. Two reasons, and the second is the one that bites: a map has no order a reader can see, so a file that lists three things says nothing about which runs first; and a map field cannot carry a per-entry comment where anybody will find it.

This is a **ratchet**, not a migration. Every map field that exists today is on the allowlist in `internal/spec/no_dictionaries_test.go`, in two sections that mean different things:
- **Permanent.** `input:`, `output:`, `result:` and `params:` are JSON Schema or provider passthrough. A JSON Schema object *is* a dictionary; converting it would stop it being one. These never move.
- **Debt.** The rest carry a one-line reason and a "migrate when". This section may shrink. It must never grow.

Adding a map-typed authored field fails `TestNoNewDictionaryInTheAuthoringSurface`, which names the field and says to write a list. `internal/ir` is out of scope: it is the resolved shape, not something a person writes.

## Testing
`make test` (`go test -race ./...`) runs L1–L3 and needs **zero Python**:
- L1 unit (pure logic, table-driven) · L2 in-process command tests (real tree, capture output) · L3 golden files (`-update` to regenerate).
- L4 smoke (`make smoke`, build tag `smoke`) proves emitted Python is valid — opt-in, needs Python, never in the default suite or PR gate.
- Telephony is verified on a **deployed** agent, against a real carrier. There is no local phone loop, and no test level stands in for one: `unmute dev` is the browser loop and it stops where the phone leg starts.
- [`docs/HARNESS_TEST.md`](docs/HARNESS_TEST.md) is the reusable prompt for real end-to-end conversations across the examples.
- [`docs/SELF_VERIFY.md`](docs/SELF_VERIFY.md) is how to check runtime behaviour **without** a person on the phone, and it is what to do before asking for a live call. Its first rule: find the layer the defect lives in and reproduce it there. A provider defect is usually one HTTP request, so it needs no audio, no tunnel and no simulated caller — [`scripts/replay_router_scopes.py`](scripts/replay_router_scopes.py) is the worked example. Coval evaluates conversations you push to it, so verification never waits on inbound reachability.
- After somebody runs `unmute dev` and talks to the agent, read the call back yourself with [`scripts/read_langfuse_trace.py`](scripts/read_langfuse_trace.py): transcript, tool calls and per-span latency, newest trace by default. Needs the package on `tracing.provider: langfuse`, which `examples/salon-concierge` is. Never describe a call from what you were told about it when the spans are one command away.

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
| no new map-typed field in the authoring surface; a new field is a list, and every existing map is allowlisted as permanent (JSON Schema passthrough) or as debt that may shrink and never grow | `internal/spec/no_dictionaries_test.go` (`TestNoNewDictionaryInTheAuthoringSurface`) |
| a pair-list item holding two keys or none is refused at decode, with its line, because that is what a dropped indent produces and what a `map[string]string` swallowed | `internal/spec/pair_test.go` |
| a package declaring no `prefetch:`, no `confirm:` and no delegate `announce:` emits byte-identical output; the emitted block, its constants and the unconfirmed set appear only when authored | `internal/generate/prefetch_test.go` (`TestPrefetchEmitsNothingForAPackageThatDeclaresNone`) |
| a pre-fetch runs at the seam on both targets, in **authored order**, and never past its budget or into a raise | `internal/generate/prefetch_test.go`, proven at L4 by `TestSmokePrefetchOutcomes` driving resolved, skipped, timed-out and raised through one module |
| a shared emitted log line carries no `%s` and no `.format()`: LiveKit logs through stdlib `logging` and Pipecat through loguru, so either style prints literally on the other target | `internal/generate/prefetch_test.go` (`TestPrefetchLogsCarryNoLibrarySpecificPlaceholder`) |
| an entry reading a value only a later entry assigns is refused, naming both entries and which to move | `internal/ir/prefetch_test.go` (`TestBuildPrefetchRefusesABackwardsOrder`) |
| a value carrying `confirm:` satisfies no gate and renders in no prompt but its confirming step's; the mark is cleared through `getattr` so a path that skipped the pre-fetch cannot raise | `internal/ir/prefetch_test.go`, `internal/generate/prefetch_test.go`, `internal/generate/guard_test.go` |
| a `prefetch:` tool must declare `read_only: true`, and the refusal says a pre-fetch would write on every call including wrong numbers | `internal/ir/prefetch_test.go` (`TestBuildPrefetchRefusesTheSource`) |
| a clock with no `timezone:` is refused, and the message says a container clock is UTC | `internal/ir/prefetch_test.go`, and the zone resolving in the emitted image is `TestSmokePrefetchZoneResolves` |
| `--source` seeds a call fact and reaches the container; `--var` keeps its exact refusal | `internal/cli/dev_vars_test.go`, `internal/generate/prefetch_test.go` (`TestPrefetchSeedReachesTheContainer`) |
| every prompt naming a pre-fetched value reads as a whole sentence when that value is empty | `internal/generate/prefetch_test.go` (`TestPrefetchedPromptsReadWholeWithEveryValueEmpty`) |
| a delegate `announce:` is spoken after the guard, so a refused step stays silent, and a delegate with none emits nothing | `internal/generate/delegate_announce_test.go` |
| every direct dependency is on the allowlist | `internal/cli/deps_test.go` |
| no declaration is reachable only from its own definition | `internal/cli/reachability_test.go` (`deadcode -test ./...`) |
| every declared capability `Field` constant has a row in the table | `internal/target/table_test.go` (`TestEveryFieldConstantHasARow`) |
| every shipped package and the scaffold template is block style, ignoring `{{placeholders}}` and the template's own `[[ ]]` | `internal/generate/examples_test.go` (`TestAuthoredPackagesAreBlockStyle`) |
| nothing authored still speaks the retired `controls:` / `kind:` shape, while a bare `kind:` stays legal on tools, channels and models | `internal/generate/examples_test.go` (`TestNothingAuthoredSpeaksTheRetiredShape`) |
| no page, bundle reference, emitted runbook or package comment still teaches `agent_transfer` or `human_transfer` as a word an author writes; the changelog and generated-code comments are excluded with their reasons inline | `internal/skill/agreement_test.go` (`TestNoReaderFacingSurfaceTeachesARetiredKindName`) |
| every documented YAML example attaches a name under the key its own catalog declares, so a reader copying a page gets something that compiles; checked per file, because one page splits the agents block and the catalog across two fences | `internal/skill/agreement_test.go` (`TestDocumentedExamplesAttachUnderTheRightKey`) |
| a name listed under the wrong kind is refused, and the message names both what the name is and the list it belongs on | `internal/ir/build_test.go` (`TestBuildRefusesANameInTheWrongList`) |
| a task's `handoffs:` survives a console round-trip, because a key `scaffold.Data` does not carry is a key `unmute maintain` deletes at exit 0 | `internal/tui/agent_name_roundtrip_test.go` (`TestMaintainKeepsATasksHandoffs`) |
| every symbol a template names still exists in Go | `internal/scaffold/template_symbols_test.go` |
| every capability row carries a deliberate value for a target `field()` does not seed, and every refusal names what to do instead | `internal/target/table_test.go` |
| the console offers every shipped target, names each correctly, and offers no option validation refuses | `internal/tui/default_target_test.go` |
| the slng target opens no socket to the SLNG agents API | `internal/ir/validate_slng_test.go` |
| every surface naming a `voiceai` command names the same one, agent id included | `internal/target/slng_target_test.go` |
| every deployed identity on all three targets is the package's `name:` joined to its target, never the target alone, while the emitted project's own labels stay the bare name | `internal/generate/deploy_name_test.go` |
| a package with no `name:`, or one that cannot be deployed as written, is refused with the shape and an example | `internal/ir/package_name_test.go` |
| the console preserves an authored name and never renames a package to its folder | `internal/tui/agent_name_roundtrip_test.go` |
| the browser token `unmute dev` mints dispatches to the name the emitted worker registers | `internal/cli/dev_web_test.go` (`TestDevDispatchNameMatchesTheEmittedWorker`) |
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
| a Coval trace's correlation is per call, not per process, so a warm Pipecat Cloud container files its second call as its own conversation and never appends it to the first | `internal/generate/coval_tracing_test.go` (`TestCovalTracingResetsCorrelationPerCall`), proven at L4 by `TestSmokeCovalTracingPipecat`, which drives three sessions through one module and hand-resets nothing |
| a simulation still claims a call that is not the first its process handled, because the per-call reset clears the simulation ID and runs just before `activate_simulation` | `internal/generate/coval_tracing_smoke_test.go` (third session) |
| the OTLP exporter keeps Coval's required 30s while the shutdown-path call registration gets its own 5s, which fits LiveKit's 10s kill window, and a budget cut short is logged | `internal/generate/coval_tracing_test.go` (`TestCovalTracingSplitsTheSubmitBudgetFromTheExportTimeout`) |
| the local-run marker is one string across `generate.LocalRunEnv` and both tracing templates, a local run's Coval labels carry `-local`, a deployed run's do not, and both targets stay distinguishable | `internal/generate/coval_tracing_test.go` (`TestCovalTracingOwnsTheLocalRunMarker`, `TestCovalTracingMarksLocalRuns`, `TestCovalTracingLabelsEveryPlaceCovalFilters`) |
| `unmute dev` actually sets that marker, and a stale value in a `.env` cannot beat it | `internal/cli/dev_test.go` (`TestDevChildEnv_marksTheRunAsLocal`) |
| the agent logs one startup line naming provider, destination and whether a credential is present, one line per trace outcome, and no line claims a trace is not exported when the conversation route will export it | `internal/generate/coval_tracing_test.go` (`TestCovalTracingLogsWhatActuallyHappened`) |
| no docs-site page states a version, by a literal or by rendering the release automation's marker | `internal/docsite/version_test.go` |
| the docs-site declares its Markdown surface, and the coding-agents page names all three endpoints | `internal/skill/markdown_surface_test.go` |
| the changelog runs newest first, every entry carries a label, a version and its own release link, the newest matches the version snippet, and no entry keeps a heading, a dash or a commit hash | `internal/docsite/changelog_test.go` |
| the runbook's vault table names exactly the entries the preflight checks | `internal/generate/slng_requirements_test.go` (`TestSlngRequirementsIsWhatTheRunbookPrints`) |
| a `builtin:` reference is checked by its tool **file** name, because that is what the emitted ref carries, and a control is never checked because it emits no ref | `internal/cli/preflight_test.go`, `internal/generate/slng_requirements_test.go` |
| a code or webhook tool is never reported missing from the account, because the push creates it | `internal/cli/preflight_test.go` (`TestPreflightNeverReportsAToolThePushCreates`) |
| an account read that could not be made warns, never counts as satisfied, and never stops a deploy | `internal/cli/preflight_test.go`, `internal/cli/deploy_test.go` (`TestDeployPreflightDegrades`) |
| the preflight refuses before anything is written, `--dry-run` included | `internal/cli/deploy_test.go` (`TestDeployPreflightRefusesBeforeWritingAnything`, `TestDeployRunsThePreflightUnderDryRun`) |
| no secret value reaches argv, a written file, or either output stream | `internal/cli/preflight_test.go` (`TestFillNeverPutsAValueOnACommandLineOrInOutput`) |
| a secret create is the only write a deploy makes; no tool, MCP server or trunk is ever created, changed or deleted | `internal/cli/preflight_test.go` (`TestDeployWritesNothingButSecrets`) |
| each account resource kind is read at most once per deploy, the post-push trunk read included | `internal/cli/voiceai_test.go` (`TestVoiceaiReadsEachResourceKindOnce`) |
| every `voiceai` command any surface or constant names is one the CLI has | `internal/target/slng_target_test.go` (`TestEveryVoiceaiCommandNamedExists`) |
| attaching a trunk is a one-field PATCH on the agent, chosen by the operator, and never happens unasked or unattended | `internal/cli/deploy_test.go` (`TestAttachTrunkPatchesOneFieldByDirection`, `TestOfferTrunkAttachesNothingWhenDeclined`, `TestDeployAttachesTheChosenTrunk`) |
| the deployed agent name comes from the package, never read back from the push (which sends none), so a free trunk's empty `in_use_by` cannot match it and be reported as reaching the agent | `internal/cli/deploy_test.go` (`TestDeployNeverReadsTheAgentNameBackFromThePush`) |
| an `mcp:` package deploys to slng, and no surface says the push must reach the server | `internal/ir/validate_slng_test.go` (`TestSlngRequiresAnExplicitMCPToolList`), `internal/generate/slng_v1_test.go`, `internal/docsite/tool_table_test.go` |
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
