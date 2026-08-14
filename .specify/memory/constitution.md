<!--
Sync Impact Report
==================
Version change: 1.1.0 -> 2.0.0
Bump rationale: MAJOR. Requirements were removed from a principle, so a
contributor holding v1.1.0 would be wrong about what this document mandates.
Principle IV loses three bullets and Development Workflow loses one paragraph;
that whole area is now deliberately out of scope of this document and governed
elsewhere. Nothing was added, and no remaining rule changed meaning.

Modified in 2.0.0:
- Principle IV: three bullets removed. The document-ranking, amendment, and
  verification rules are unchanged.
- Development Workflow And Quality Gates: one paragraph removed; the wording of
  the maturity paragraph adjusted to drop a dependent reference.

--- 1.1.0 ---
Version change: 1.0.0 -> 1.1.0
Bump rationale: MINOR. Adds the "Targets and providers" section to Technology
And Boundary Constraints, fixing the implemented set in normative terms
(pipecat and livekit ship; vapi and deepgram validate only; elevenlabs is not a
target; slng never was; model vendors are catalogue entries under the two
shipped drivers). No principle was removed or redefined.

Modified in 1.1.0:
- Principle III: added the vendor-is-not-a-target bullet.
- Principle V: `validate` reach vs `compile`/`dev` reach made explicit.
- Technology And Boundary Constraints: new "Targets and providers" subsection.
- Governance, compliance review: reviewers check that a support claim states
  validation or generation.

--- 1.0.0 (initial ratification) ---
Version change: none (template placeholders) -> 1.0.0
Bump rationale: initial ratification. The prior file was the unfilled core
scaffold, so this is the first real adoption, not an amendment.

Principles defined (all new):
- I. Compile Ahead Of Time, Never Interpret At Runtime
- II. Fail Loud, Never Average
- III. One Source Of Truth Per Surface, Derived Not Copied
- IV. The Document Wins
- V. Whatever Compiles Can Be Spoken To

Sections added:
- Technology And Boundary Constraints (was [SECTION_2_NAME])
- Development Workflow And Quality Gates (was [SECTION_3_NAME])
- Governance (filled in)

Sections removed: none.

Sources read to derive this document: CLAUDE.md, docs/ARCHITECTURE.md,
docs/SCHEMA.md, docs/TESTING.md, docs/REPO_MAP.md, docs/PROVIDER_CATALOG.md,
docs/ORCHESTRATOR_SHARED_CONFIGURATION.md, docs/TELEPHONY.md,
docs/DEPLOYMENT.md, docs/PRODUCTION_ROADMAP.md, all of docs/user/,
Makefile, go.mod, .golangci.yml.

Follow-up TODOs: none. No placeholder tokens remain.
-->

# Unmute Constitution

Unmute is a Go command line tool that compiles one declarative voice agent
package into artifacts that a chosen voice orchestrator runs natively. The
author writes what the agent should do, once. Unmute writes the LiveKit or
Pipecat Python project that does it, then gets out of the way. This document
holds the rules that decision creates, and they bind every change to this
repository.

## Core Principles

### I. Compile Ahead Of Time, Never Interpret At Runtime

Unmute is a compiler, not a runtime layer. It MUST NOT sit between a generated
agent and its platform at call time.

- Maintained runtime code in this repository MUST be Go. Python exists only as
  `text/template` files under `internal/generate/templates`, as local tool
  handlers copied from a package, and as generated output. This repository
  MUST NOT gain a maintained Python package.
- Every command that touches a package MUST run the same four stages in the
  same order: `spec.Load` then `ir.Build` then `ir.Validate` then
  `generate.Generate`. Validation is a gate, never a skippable warning, and
  `Generate` MUST refuse to emit when any gated error is present.
- A generated project MUST carry no Unmute dependency. It MUST be readable,
  runnable, and deployable with Unmute absent.
- `build/<target>/` is disposable and rewritten from scratch on every compile.
  Authors change the source package and recompile; they never edit generated
  files. Nothing in the repository may depend on a hand edit surviving there.
- Portable behavior lives in the source package. Target mechanics live in one
  driver each. A driver MUST NOT leak its vocabulary into `internal/spec` or
  `internal/ir`.
- Media, transcripts, prompts, model context, task state, and agent handoff
  state MUST stay in the process running the call. Redis, where a route needs
  it, holds only bounded, expiring control records.

Rationale: a runtime translation layer could only pass along what a platform
already exposes, and it would ship Unmute into production forever. Owning the
generated project is what lets a driver write a handoff guard, a typed task, or
a transfer bridge around a framework's native primitives. It is also what makes
the artifact the user's, not ours.

### II. Fail Loud, Never Average

When a target cannot honor something the package asked for, Unmute stops and
says so, in that platform's own words. It MUST NOT drop the feature, and it
MUST NOT substitute a weaker version to make things fit.

- Every field carries one of four tags, and the tag decides the behavior:
  `core` never fails; `warn` prints to stderr and exits 0 with the artifact
  still valid; `gated` is a hard error before any artifact is written or any
  request is sent; `provisional` fails on every target until a driver proves
  it.
- A gated error MUST name the failing target and quote that provider's own
  vocabulary. With several resolved targets, validation fails if any one of
  them rejects.
- A field MUST NEVER silently do nothing. A warning is never a silent
  downgrade, and a warning MUST NOT be promoted to a pass by hiding it.
- Strict decoding is part of this rule. An unknown or misspelled field is an
  error with file, line, and column. A field the schema moved MUST report the
  new form and quote the offending line, never a bare "unknown field".
- Values Unmute forwards without checking (model identities, `params`, a
  `deployment_region`) MUST all appear in the compile report, so what was sent
  is always inspectable.
- Any known exception to this principle MUST be recorded in `docs/SCHEMA.md`
  as a dated, numbered amendment that states the cost, rather than being left
  implicit in the code. Tool `output` is the current recorded exception (N22).

Rationale: what passes validation has to be real, or the tool is worse than no
tool. Averaging removes a feature everywhere so the agent is uniformly worse.
Silent downgrade keeps the feature on paper and breaks it exactly where nobody
is looking. Both trade a loud failure at `validate` for a quiet one in
production, on a live phone call.

### III. One Source Of Truth Per Surface, Derived Not Copied

Every fact that two places need MUST have one home and be derived from it.
Where derivation is impossible, an agreement test MUST fail on drift.

- Go structs are the schema source for their own surface. `internal/spec`
  decode structs derive the unresolved authoring schema; `internal/ir` structs
  derive the resolved and debug schema, both by reflection through
  `google/jsonschema-go`. Hand authoring a `.json` schema file is forbidden.
- `internal/target` is the single capability rulebook. Validation and every
  generator MUST read it and MUST NOT keep a second description of what a
  target supports. Telephony support resolves against the exact
  `(orchestrator, transport, carrier)` route and feature, never against an
  orchestrator or carrier brand alone. A second telephony capability table is
  forbidden.
- Provider integrations are typed Go `Entry` literals in
  `internal/target/catalog_*.go`, one per `(framework, role, vendor)`. An entry
  is a code slot, not an allowlist: it carries the class, import, install path,
  argument shape, key env, a `Verified` date, and a `Docs` URL. It MUST NOT
  carry model names, voice ids, or param schemas.
- A catalogue vendor is a model provider, never a target. A vendor name is only
  ever a `provider:` on a model entry, and adding one MUST NOT be read or
  described as adding a target. The two words are kept apart on purpose, and
  the current state of each is fixed below in Targets And Providers.
- All color lives in `internal/style`. No color literal may appear anywhere
  else, in the TUI or in the plain CLI output.
- Wherever one fact is stated twice, an agreement test is mandatory. The
  existing ones MUST stay green and MUST NOT be weakened: emitter against
  capability table, TUI pickers against capability table, provider reference
  docs against the catalogue, and the per-entry resolution golden.

Rationale: this repository has already been burned by duplication. Two merged
pull requests of provider breadth were silently reverted by a refactor because
nothing tied user visible options to `Catalog.Lookup`. A field can validate
green while no emitter has a code path for it. Every one of those bugs is a
second copy of a fact, and the fix is always the same shape: one home, or one
test that fails.

### IV. The Document Wins

The design documents are normative, not descriptive. When code and a doc
disagree, the doc wins: fix the code, or amend the doc deliberately.

- `docs/SCHEMA.md` is the locked v1 authoring contract and outranks every
  other document on what a field means and where it works. `docs/ARCHITECTURE.md`
  owns system boundaries and the compiler flow.
  `docs/ORCHESTRATOR_SHARED_CONFIGURATION.md` and `docs/PROVIDER_CATALOG.md`
  hold research and reasons and yield to `SCHEMA.md`.
- A change to the authoring surface MUST land as a numbered, dated amendment in
  `docs/SCHEMA.md` that states whether the authoring shape changed and whether
  existing packages fail strict decode. Amendments are appended, never
  rewritten in place, and superseded ones stay as history.
- Provider and framework claims MUST be verified against official
  documentation or upstream source, never from model memory, and MUST carry the
  verification date. An unverified claim stays gated.

Rationale: the schema is a promise made to every package already written
against it. Treating the document as the authority is what makes a promise
auditable: a reader can see when a rule changed, why, and what it broke. Dated
verification is the same discipline applied to facts we do not own, in a
domain where four vendors ship breaking changes on their own schedule.

### V. Whatever Compiles Can Be Spoken To

The command surface is four commands, and together they MUST take an author
from nothing to a voice they can talk to.

- `unmute init` scaffolds a v1 package. It MUST refuse to write into a
  directory that already exists and is non-empty. Interactively it opens the
  console, previews the candidate through the real load, build, and generate
  path, and writes nothing until the author confirms.
- `unmute validate` runs load, build, and validate and stops. It MUST work for
  every declared provider whether or not that provider's driver ships, because
  it reads the capability table and the catalogue rather than a driver. It
  prints one result line per target and exits 1 if any target fails. This is
  the only command whose reach is all four providers.
- `unmute compile` runs validate then generates, writing each code target to
  `build/<target>/`. One target instance produces one artifact directory and,
  for telephony, exactly one carrier route. Generation reaches only the
  providers with a shipped driver; the rest MUST fail by name.
- `unmute dev` compiles and then runs the result so the author can speak to it:
  in the browser (the default, Docker, running the same image production
  deploys), in the terminal (`--console`, uv, no Docker), or over a real phone
  (`--telephony`, the generated Compose graph). It reaches exactly the
  providers `compile` reaches.
- What you test MUST be what you ship. A scaffolded instance is named after its
  provider with no environment suffix, and web dev mode runs the deployable
  image rather than a separate host path.
- Local input MUST stand in for production input rather than diverging from it.
  `--var name=value` seeds exactly the `source: call_start` variables that
  dispatch metadata carries in production, and every flag is checked against
  the declared name and type before anything compiles or starts.
- Local development MUST leave borrowed external state as it found it. Every
  outward facing change a dev run makes has an undo that runs on every exit
  path, `ctrl-c` included, and an opt out flag for state the run must not
  touch.

Rationale: a compiler for voice agents that cannot be heard is unfinished. The
gap this closes is the usual one between a green build and a working call, and
it closes only if the local path runs the real artifact with real input. The
undo rule exists because a dev run that rewrites a live phone number's webhook
and walks away breaks a real phone line, and the failure never reaches the logs
of the person who caused it.

## Technology And Boundary Constraints

**Targets and providers.** A **target** is an orchestrator a package can compile
to, named by `provider:` in `targets.yaml`. A **provider** on a model entry is a
**vendor**, a code slot in the catalogue. These are different things with an
overlapping word, and no document, error message, or commit may blur them.

There are exactly four target providers, and `ir.Provider` MUST hold exactly
these four values. Two of them have a driver:

| Target provider | Kind | State |
|---|---|---|
| `pipecat` | code target | Shipped. `bot.py` project, generated and runnable. |
| `livekit` | code target | Shipped. `agent.py` project, generated and runnable. |
| `vapi` | managed target | Validates only. No driver. |
| `deepgram` | code target (session bridge) | Validates only. No driver. |

Rules that follow, and none of them may be softened to make a doc read better:

- **Only Pipecat and LiveKit are implemented.** They are the only providers that
  `compile` and `dev` can serve. Vapi and Deepgram MUST fail with
  `<provider> driver is not implemented`, and that failure MUST stay loud rather
  than degrading to a warning or an empty artifact.
- **Validation is deliberately wider than generation.** All four providers
  validate, because portability has to be checkable before an author commits to
  a platform. A capability row, a schema tag, or a `docs/` table for Vapi
  or Deepgram is therefore normal and correct, and it MUST NOT be read as a
  claim that the provider compiles.
- **Any statement of support MUST say which of the two it means.** Writing that
  a feature "works on Deepgram" without saying it means validation only is a
  defect in the document, not a shorthand.
- **ElevenLabs is not a target.** It was removed from the target set on
  2026-07-20 (SCHEMA N17), along with its driver, its target catalogue, the
  `region` and `edition` instance fields, and the `unmute apply` command. A
  `targets.yaml` naming it MUST fail loudly. It survives only as a model vendor,
  below. Reintroducing it as a target requires a schema amendment, not a patch.
- **SLNG is not a target either.** It has never been a `Provider` value in v1.
  It is the model vendor the `unmute init` scaffold binds by default, through
  `pipecat-slng` and `livekit-plugins-slng`, which is a starting point and never
  a constraint.
- **Model vendors are catalogue entries only, and they are Pipecat and LiveKit
  only.** `slng`, `elevenlabs`, `deepgram`, `cartesia`, `openai`, and the rest
  live as `Entry` literals in `catalog_pipecat.go` and `catalog_livekit.go`.
  Their breadth belongs to the two shipped drivers. `catalog_deepgram.go` holds
  call-less allowlist rows that feed validation and emit no code, which is what
  a catalogue for a driverless target looks like. `deepgram` and `elevenlabs`
  each appear on both sides of the target-versus-vendor line, and that is not a
  contradiction: the vendor entry is a shipped code slot, the target is not
  shipped at all.
- **`apply` does not exist.** The managed-target apply path retired with
  ElevenLabs and returns with the next managed driver. Nothing may document,
  print, or reference it as an available command until then.
- **A driver landing is what changes this table**, and it changes it in one
  commit that includes the driver, its capability rows, its emitter agreement
  test, its goldens, its target page, and this section.

**Language and build.** Go 1.24, pinned in `go.mod`. Binaries are
`CGO_ENABLED=0` static builds with the version stamped at link time through
`-ldflags`, never hardcoded. Maintained Go code MUST use only standard library
APIs available at the module's `go` directive; the `go vet ./...` gate runs the
`stdversion` analyzer and fails on a newer API before merge.

**Dependencies.** Direct dependencies are `spf13/cobra`, `goccy/go-yaml` (for
line and column on parse errors), `google/jsonschema-go` (pin the exact v0.x
and bump deliberately), and the Charm stack: `bubbletea`, `bubbles`, and
`lipgloss` for the interactive console, with `charmbracelet/huh` scoped to the
accessible and headless renderer only. The interactive path MUST import no
`huh`. Everything else is standard library. No new dependency for what a few
lines of stdlib do, and any addition MUST be justified in the pull request. No
`viper` until a real global config file exists.

**Command rules.** These are what make commands testable, not style
preferences. Build the tree with a `newRootCmd()` constructor and no
package level `rootCmd`, so each call gets flag isolation. Use `RunE`, never
`Run`. Write through `cmd.OutOrStdout()` and `cmd.ErrOrStderr()`; a stray
`fmt.Println` is invisible to tests and is a bug. No `os.Exit` or `log.Fatal`
inside a command: return errors wrapped with `%w`. `os.Exit` lives only in
`main.go`. The root sets `SilenceUsage` and `SilenceErrors`. Exit codes are 0
for success and 1 for error; add another only when a consumer actually reads
it. Warnings go to stderr and still exit 0.

**Layout.** `internal/`, not `pkg/`. One file per command in `internal/cli/`.
Cobra commands are hand written; the `cobra-cli` generator is not used.
`internal/target` stays a leaf package so both `ir` and `generate` can import
it without a cycle.

**Secrets.** No secret value appears in any package file, any generated file,
or any report. Packages carry environment variable names only, in
`UPPER_SNAKE`, so a pasted URL or token fails validation instead of becoming a
lookup that fails at call time. A declared `secrets:` block drives each build's
`.env.example` and a startup check that names a missing value. Secrets MUST NOT
flow through `{{...}}` templates: every template site renders into something
spoken, prompted, traced, or logged. They reach a call only through `*_env`
slots, the generated auth helpers, and `os.environ` in a local handler. The
loader never reads a named environment value.

**Naming and types.** All names are lowercase `snake_case`; a leading
underscore is reserved by providers and rejected. Tools and controls share one
namespace. Durations use Go syntax (`90s`, `15m`, `1h30m`). Variables and task
result fields are the four primitives plus enums, because that is the real
cross provider common ground.

**Derived, never declared.** Machine sizes, replica counts, and GPU counts
appear in no package file. They are derived from declared `capacity` and
printed in the report, marked `unbenchmarked` until a dated coefficient exists.

**Generated Python.** Emitted Python is checked with `ruff` where available and
proven valid by the opt in smoke layer. Any Python written or edited by hand in
this repository, including examples and snippets, MUST be checked with `ty` and
`ruff`.

**Telephony boundary.** Unmute never provisions carrier side resources: it does
not buy numbers and does not create carrier applications or carrier SIP trunks.
It automates only Unmute owned local development state, restorably. A route
ships only with a real generated adapter; a route with no adapter fails closed
before an artifact exists. Public ingress, TLS, Redis, scaling, and secret
storage stay with the operator.

**Voice.** Documents and replies use plain, simple wording.

## Development Workflow And Quality Gates

**The gate.** Five targets, in this order: `make fmt`, `make lint`,
`make build`, `make test`, `make smoke`. `make test` is the required gate and
MUST pass with zero Python. `make smoke` is opt in, needs `uv`, and MUST NOT
enter the default suite or the pull request gate; it skips rather than fails
when `uv` is absent. Credentialed carrier smokes are opt in and never enter the
pull request gate either.

**The four test layers.** L1 unit tests are pure, table driven logic. L2 runs
commands in process against the real cobra tree with output captured, and is
the seam that makes the interactive console testable through its accessible
renderer with no TTY. L3 pins generated bytes in golden files. L4 smoke proves
emitted Python against pinned upstream packages by installing, importing, and
instantiating it, which is the only layer that catches constructor drift in a
vendor SDK.

**Golden files.** Regenerate only after an intentional output change, then read
the diff before committing. Update flags are per package and deliberate
(`-update`, `-update-pipecat`, `-update-catalog`). A golden that iterates the
catalogue means a new entry cannot dodge coverage.

**Checks that must exist.** Non trivial logic leaves one runnable check behind:
the smallest thing that fails if the logic breaks. A repository hygiene rule
about what is committed MUST be written against `git`, not the working tree, so
that compiling an example locally cannot turn the suite red.

**Adding a provider.** Verify the upstream surface against current docs or
source. Append one catalogue entry with its `Verified` date and `Docs` URL.
Refresh the resolution golden and read the diff. Run the catalogue invariants.
Extend a smoke fixture when the entry introduces a new constructor shape.
Update the Models pages under `docs-site/models/`, which a sync test checks in
both directions. No driver code, no template edits, no validation edits.

**Maturity is tracked, not hidden.** A driver may lag the schema: a feature can
be real on a platform and not yet emitted by its driver. Those gates fail
validation by name and lift when the driver emits it. A telephony route
that runs but has no automated credentialed smoke is `provisional`, which is
internal maturity tracking recorded in `compile-report.json` and never printed
as a runtime warning. Neither state may be presented as more proven than it is.

## Governance

This constitution governs how work is done in this repository and supersedes
habit and precedent. It does not outrank the design documents on design facts:
where this document and a document named in Principle IV disagree about what a
field means or where it works, that document wins for that fact, and this
constitution wins on process.

**Amendment procedure.** An amendment is a pull request that states what
changes, why, and what existing packages or contributors must do differently.
It MUST update the version line and prepend the Sync Impact Report at the top
of this file. An amendment that changes a principle MUST also name the
documents, tests, or commands that now have to change with it.

**Versioning policy.** This document is versioned MAJOR.MINOR.PATCH.
MAJOR for removing or redefining a principle in a way that invalidates
existing practice. MINOR for adding a principle or materially expanding
guidance. PATCH for clarifications, wording, and non semantic refinement.

**Compliance review.** Every pull request MUST pass `make fmt`, `make lint`,
`make build`, and `make test`. A pull request that changes the authoring
surface MUST, in the same change, amend `docs/SCHEMA.md` with a numbered dated
amendment and update the derived schemas, the capability table, the agreement
tests, the scaffold templates, the interactive console, the in repository
examples and fixtures, and `docs-site/`. Reviewers check that a new field
cannot validate green on a target whose emitter has no code path for it, that a
new fact does not create a second copy of an existing one, and that any claim
of provider support says whether it means validation or generation, per Targets
And Providers.

**Complexity must be justified.** A new dependency, a new abstraction, an
interface with one implementation, or a config knob for a value that never
changes each need a stated reason in the pull request. Absent that reason, the
answer is no. Deliberate simplifications are marked in code so that simple
reads as intent rather than oversight, and a shortcut with a known ceiling
names the ceiling and the upgrade path.

**Runtime guidance.** `CLAUDE.md` holds the day to day engineering rules and
`docs/REPO_MAP.md` points at the load bearing files. Both are subordinate to
this document and to the documents in Principle IV.

**Version**: 2.0.0 | **Ratified**: 2026-08-12 | **Last Amended**: 2026-08-12
