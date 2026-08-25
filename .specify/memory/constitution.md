<!--
Sync Impact Report
==================
Version change: 5.0.0 -> 6.0.0
Bump rationale: MAJOR. Principle V is redefined. `dev` no longer promises a
local stand-in for a carrier, and the bullet that governed such a stand-in
("local input stands in for production input") is removed rather than reworded,
because the thing it governed no longer exists.

Modified in 6.0.0:
- Principle V's `dev` bullet: `dev` is the browser loop and nothing else. The
  Docker-unless-it-breaks-the-test rule stays; its second exception (a route
  whose platform terminates the carrier's stream) goes with the local phone run.
- Principle V gains a bullet saying where telephony is verified: on a deployed
  agent, against a real carrier. Cold and warm transfer with it.
- Principle V drops "local input stands in for production input instead of
  creating another behavior path". It existed to bind the local telephony
  planes, and every plane is deleted.

Reason: the local telephony dev path created more complexity than it earned. A
carrier reaches an agent over publicly routable signalling and media ingress,
which a developer's machine does not have, so a local plane could confirm the
wiring on that machine and nothing about going live. The two Pipecat route
families that deployed nowhere are removed with it; every telephony route the
compiler emits now deploys to LiveKit Cloud or Pipecat Cloud.

Contributors do differently: do not add a local phone loop, a carrier stand-in,
or a test level that imitates one. Verify telephony by deploying. `make rig` and
the `rig` build tag are gone, so the test levels are L1 to L3 (required) and L4
smoke (opt-in).

Convention for the next amendment: put the new report at the top of this comment
and push the one it replaces down under "Previous report". Trim the oldest when
this comment reaches three reports.

Previous report
---------------
Version change: 4.0.1 -> 5.0.0
Bump rationale: MAJOR. Technology and Boundaries no longer permits validation to
cover more targets than generation. A provider with no driver is not a target.

Modified in 5.0.0:
- The bullet "Pipecat and LiveKit have shipped drivers; validation may cover more
  targets than generation" is replaced. It permitted `vapi` and `deepgram` to be
  accepted target values that validate and then fail at compile with "driver is
  not implemented". Both are retired.
- The targets-and-vendors sentence stays and gains a clause, because retiring
  the Deepgram *target* while keeping the Deepgram *model vendor* is exactly the
  distinction that bullet exists to protect.

Reason: a documented surface that never emitted a runnable project. An author
could write `provider: vapi`, watch `validate` pass, and only learn at compile
that nothing would be produced. The capability tables carried four columns to
describe two working drivers.

What contributors do differently: a capability row now supplies two values, not
four. A new target provider arrives with its driver, not ahead of it.
-->

# Unmute Constitution

Unmute is a Go compiler. It reads one voice-agent package and writes a native
LiveKit or Pipecat project. These principles bind every change. Day-to-day
commands and detailed gates live in `CLAUDE.md`.

## Core Principles

### I. Compile Ahead of Time

Unmute MUST NOT sit in the generated agent's production call path.

- Maintained runtime code in this repository is Go.
- Python exists only in templates, copied local handlers, examples, and
  generated output.
- Package commands use the same flow: `spec.Load` -> `ir.Build` ->
  `ir.Validate` -> `generate.Generate`.
- Generated projects carry no Unmute runtime dependency.
- `build/<target>/` is disposable. Authors edit the package and compile again.
- Portable behavior belongs in the package. Target mechanics belong in one
  driver per target.
- Redis, where a route needs it, holds bounded control records only. Media,
  transcripts, prompts, model context, task state, and handoff state stay in
  the active process.

### II. Fail Loud

Unsupported behavior MUST fail before generation. Unmute MUST NOT silently
drop it or replace it with a weaker behavior.

- Strict decoding rejects unknown fields with file, line, and column.
- A gated error names the failing target and uses that target's vocabulary.
- Warnings go to stderr, keep exit 0, and never hide a downgrade.
- Values forwarded without validation appear in the compile report.
- A route with no real adapter fails closed.
- Non-trivial behavior leaves one runnable check behind.

### III. One Owner per Surface

Every repeated fact has one owner. Where a fact cannot be derived, an
agreement test MUST fail on drift.

- `internal/spec` Go structs own the authoring schema.
- `internal/ir` Go structs own the resolved and debug schema.
- `internal/target` owns capabilities, route support, provider catalogue
  entries, verification dates, and documentation URLs.
- `internal/cli` owns commands, flags, usage, and exit behavior.
- `docs/ARCHITECTURE.md` owns system boundaries, compiler flow, runtime
  topology, and repository orientation.
- `docs-site/` is the only public user-documentation tree.
- `CLAUDE.md` owns contributor rules and required change surfaces.
- `internal/skill/assets/` is a shipped offline product surface for coding
  assistants building Unmute packages. Its agreement tests stay green.
- `specs/` holds ignored local work in progress. A local spec can guide a
  change, but it never outranks shipped code or tracked documentation.

Hand-authored JSON schemas and second capability tables are forbidden. All
terminal color lives in `internal/style`.

### IV. The Owner Wins

When two surfaces disagree, fix the surface that does not own the fact.

- Code and derived checks win on package fields, validation, capabilities,
  providers, commands, and emitted behavior.
- The architecture document wins on system boundaries and compiler flow.
- The documentation site wins on how users install, author, run, and deploy.
- `CLAUDE.md` wins on repository workflow and quality gates.
- Provider and framework claims use current official docs or upstream source
  and carry a verification date when the repository records them.

This hierarchy replaces amendment ledgers in a hand-written schema file. Git
history and local feature specs keep design history without making it a second
active contract.

### V. Whatever Compiles Can Be Spoken To

Four commands take an author from nothing to a voice they can talk to:
`init`, `validate`, `compile`, and `dev`.

- `init` refuses to overwrite a non-empty directory.
- `validate` loads, builds, and validates without writing an artifact.
- `compile` validates before writing one artifact directory per target.
- `dev` runs the generated project locally and serves one browser session to
  talk to it. That is its whole surface. It runs the project in Docker wherever
  the topology allows, and as a host process where Docker would break the thing
  under test: Pipecat browser WebRTC, which needs host-reachable ICE candidates.
- `dev` stands in for no carrier. A phone call reaches an agent that is
  deployed, so telephony, cold transfer and warm transfer are verified after
  deploy, against a real carrier. The compiler's job on that path is to emit
  everything the deployed agent and its operator need, and to say so in the
  runbook; it is not to imitate a carrier on the author's machine.
- `skill` is outside this path. It installs the offline coding-agent bundle
  and MUST NOT read, write, or validate an agent package.
- Local development restores borrowed external state on every exit path.

## Technology and Boundaries

- Go is pinned in `go.mod`; binaries are static with version data stamped at
  link time.
- Direct dependencies stay limited to the modules listed in `CLAUDE.md`.
  Standard library code wins when it is enough.
- Cobra commands use a fresh `newRootCmd()`, `RunE`, command output writers,
  wrapped errors, and no process exit outside `main.go`.
- `internal/`, not `pkg/`; one file per command under `internal/cli/`.
- Go structs derive schemas. Do not hand-write schema JSON.
- Secret values appear in no package, generated file, or report.
- Targets and model vendors are different concepts, and the distinction is
  load-bearing: a vendor name and a target name can be the same word.
- A provider is a target only when a driver emits a runnable project for it.
  Pipecat and LiveKit are the targets. A provider with no driver is not accepted
  as a target value, so `validate` and `compile` agree about what exists.
- Unmute does not buy phone numbers or provision carrier-side applications or
  SIP trunks.

## Development Workflow and Gates

- Feature work uses the Spec Kit flow: specify, plan, tasks, implement.
- Feature artifacts live under ignored `specs/`. Codex-managed worktrees copy
  the local snapshot through `.worktreeinclude`; worktrees do not synchronize
  it afterward.
- The required gate is `make test` (`go test -race ./...`) with zero Python.
- Also run `make fmt`, `make lint`, and `make build`. Run `make smoke` when the
  change touches emitted Python or pinned provider SDK behavior.
- Repository hygiene checks inspect `git ls-files`, not untracked build output.
- Golden files change only after an intentional output change and their diffs
  are read before completion.
- Checked-in Python passes `ruff check .`; run `ty check` when provider SDKs
  are installed.

A user-visible emitted behavior change updates the surfaces it reaches:

1. the generated README or emitted runbook;
2. the relevant example README;
3. the relevant `docs-site/` page;
4. the shipped coding-agent skill.

Update architecture only for a real boundary, compiler-stage, or runtime-
topology change.

## Governance

- Amendments state what changes, why, and what contributors must do
  differently.
- MAJOR removes or redefines a principle; MINOR adds one; PATCH clarifies.
- A new dependency, abstraction, interface with one implementation, or config
  knob needs a concrete reason. Without one, do less.
- Rules added to `CLAUDE.md` need a failing gate or the label `(advisory)`.

**Version**: 6.0.0 | **Ratified**: 2026-08-12 | **Last Amended**: 2026-08-25
