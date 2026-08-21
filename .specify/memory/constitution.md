<!--
Sync Impact Report
==================
Version change: 4.0.0 -> 4.0.1
Bump rationale: PATCH. Clarifies one bullet under Principle V to match shipped
behavior. The principle itself is unchanged: whatever compiles can still be
spoken to, through the same four commands.

Modified in 4.0.1:
- Principle V's `dev` bullet said the generated project runs "in Docker". That
  was already false in two shipped cases: the Pipecat browser loop runs the bot
  as a host process so browser WebRTC can reach host-reachable ICE candidates
  (see internal/generate/templates/pipecat_v1/compose.dev.yaml.tmpl), and the
  cloud-websocket route runs it under `uv` because that route's platform
  terminates the carrier's stream. The bullet now states the rule actually
  followed: Docker unless containerizing would break the thing under test.
- The same bullet now says a local stand-in for a carrier is not the generated
  project. Local telephony planes stand in for a carrier, so they are governed
  by "local input stands in for production input" rather than by this bullet.

Reason: found by `/speckit-analyze` on specs/015-local-telephony-rigs, which
correctly flagged the Docker bullet as a constitution conflict. The conflict
predated that feature. Fixing the text rather than diluting the principle.

Contributors do nothing differently. No gate changes.

Convention for the next amendment: put the new report at the top of this comment
and push the one it replaces down under "Previous report". The file used to hold
only the current report, so history lived in git alone; keeping the previous one
inline costs four lines and answers "when did this bullet change" without a
blame walk. Trim the oldest when this comment reaches three reports.

Previous report
---------------
Version change: 3.0.0 -> 4.0.0
Bump rationale: MAJOR. Principle IV no longer makes a hand-written schema
document the highest authority. Feature specs become ignored local work and
the public documentation site becomes the only user-doc tree.

Modified in 4.0.0:
- Principle III defines one owner for code facts, architecture, public usage,
  contributor rules, and local planning.
- Principle IV changes from "the document wins" to "the owner wins".
- Workflow makes Spec Kit artifacts local and reduces the maintained emitted
  behavior surfaces from five to four.

Reason: the old hierarchy required every behavior change to update both
`docs/` and `docs-site/`. Those copies drifted. The repository now derives
machine contracts from Go, keeps one architecture document, and writes public
guidance once.
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
- `dev` runs the generated project locally, in Docker wherever the topology
  allows. It runs as a host process where Docker would break the thing under
  test: Pipecat browser WebRTC, which needs host-reachable ICE candidates, and
  a route whose platform terminates the carrier's stream. A local stand-in for
  a carrier is not the generated project and is not bound by this bullet.
- `skill` is outside this path. It installs the offline coding-agent bundle
  and MUST NOT read, write, or validate an agent package.
- Local input stands in for production input instead of creating another
  behavior path.
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
- Targets and model vendors are different concepts. Pipecat and LiveKit have
  shipped drivers; validation may cover more targets than generation.
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

**Version**: 4.0.1 | **Ratified**: 2026-08-12 | **Last Amended**: 2026-08-19
