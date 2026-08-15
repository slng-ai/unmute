# Implementation Plan: Make the first five minutes work, and stop lying quietly

**Branch**: `013-first-five-minutes` | **Date**: 2026-08-15 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/013-first-five-minutes/spec.md`, and
the measured evidence in [reproduction.md](./reproduction.md).

## Summary

Eight defects share one shape: the CLI exits 0 while what the author declared is
absent from the generated project. Each was reproduced against `16289f4` before
anything was designed, and three of the brief's premises were corrected by that
reproduction.

The technical approach is **place each check at the earliest stage that can
express it, and give every duplicated fact one home**:

- A declaration nothing reaches is a property of the package alone, not of any
  target, so it is checked in `ir.Build` where errors carry file and line — one
  firing, not one per driver.
- A transfer with no route is a property of the resolved target, so it is
  checked by making the LiveKit control row transport-conditional in the
  capability table, exactly as Pipecat's already is. No new code path.
- Eight value checks that live in generators move to `ir.Validate`, which is
  where the repository already decided sibling checks belong.
- Two files that answer one question stop being kept in sync by hand.

Nothing here needs a new dependency, a new abstraction, or a new command.

## Technical Context

**Language/Version**: Go 1.24, `CGO_ENABLED=0`, version stamped at link time.

**Primary Dependencies**: unchanged — `cobra`, `goccy/go-yaml`,
`google/jsonschema-go` (pinned v0.x), `charmbracelet/bubbletea` + `bubbles` +
`lipgloss`, `charmbracelet/huh` v1.0.0 (accessible renderer only). **No addition
is proposed.**

**Storage**: none. Packages are files on disk; `build/<target>/` is disposable.

**Testing**: `go test ./...` covers L1 unit, L2 in-process command, and L3
golden, with zero Python. L4 lives behind the `smoke` build tag and needs
Python and Docker.

**Target Platform**: macOS and Linux developer machines; generated projects run
in Docker.

**Project Type**: compiler CLI.

**Performance Goals**: not applicable. No hot path changes.

**Constraints**: every commit green on `make fmt && make lint && make build &&
make test`. Warnings to stderr with exit 0; errors are errors. Emitted behaviour
changes update five documentation surfaces in the same commit, per CLAUDE.md.

**Scale/Scope**: seven reproduced defect groups (A to G in reproduction.md),
eleven examples, five documentation surfaces, seventeen agents across three
verification waves. The groups are not one defect each: group A covers eight
declaration shapes, group E covers eight generator-only value checks.

## Constitution Check

*GATE: passed before Phase 0, re-checked after Phase 1 design.*

| Principle | How this feature stands against it |
|---|---|
| **I. Compile ahead of time** | No runtime layer added. All new checks run in `spec.Load` → `ir.Build` → `ir.Validate`, before any artifact. The generated project gains one `COPY` line and keeps zero Unmute dependencies. |
| **II. Fail loud, never average** | This is the principle the feature exists to restore. Eight places currently drop a declared field silently; each becomes a named refusal or a named warning. The one behaviour deliberately kept as a warning, the secrets cross-check, is the one `docs/SCHEMA.md` N24 already fixes as a warning. |
| **III. One source of truth, derived not copied** | Three duplications are removed rather than re-synced: the LiveKit turn-model set moves into `internal/target` so validate and generate read one copy; the two `.env.example` files get an agreement test instead of hand syncing; the model identifier gets one Go home that its test reads. The capability table stays the only rulebook — the transfer fix is a table row, not a second description. |
| **IV. The document wins** | Three code-versus-document conflicts are resolved in the document's favour, and the one place the document is wrong is amended deliberately: `docs/SCHEMA.md:317` offers `silero` as the turn-model example and §4.3 says identities are never validated, while the LiveKit driver rejects it. That needs a dated, numbered amendment, not a quiet code change. |
| **V. Whatever compiles can be spoken to** | The whole point. `unmute init` then `unmute dev` must reach a spoken greeting on two environment variables. `unmute skill` stays off that path and gains no new role. |

**No violations to justify.** The Complexity Tracking table below is empty.

### Gate re-check after Phase 1

Re-checked. One design decision needed recording rather than justifying: the
unattached-declaration check moves to `ir.Build` rather than `ir.Validate` as
FR-004 first proposed. That is *more* constitutional, not less — it fires once
for the package instead of once per target, and it is the only stage that can
carry the file and line FR-001 requires. Recorded in
[research.md](./research.md) as D1, and FR-004 in the spec has since been
amended to state the split, so no artifact still says `ir.Validate` for all
three.

## Project Structure

### Documentation (this feature)

```text
specs/013-first-five-minutes/
├── spec.md              # what and why
├── reproduction.md      # Wave A evidence, written before any fix
├── plan.md              # this file
├── research.md          # the decisions, with the alternatives rejected
├── data-model.md        # the reachability graph the new check walks
├── quickstart.md        # how to verify the whole feature by hand
├── contracts/
│   └── messages.md      # the exact refusal and warning strings
├── checklists/
│   └── requirements.md
├── tasks.md             # /speckit-tasks output
└── results.md           # the real numbers, written at the end
```

### Source code touched

```text
internal/
├── ir/
│   ├── build.go                 # NEW: unreachable-declaration check, beside checkToolRefs
│   ├── validate.go              # secrets guard removed; provider key envs added to the
│   │                            # reference set; eight generator checks mirrored in
│   └── build_test.go            # the unreachable-declaration table test
├── target/
│   ├── table.go                 # LiveKit cold-transfer row becomes transport-conditional
│   └── catalog_livekit.go       # NEW home for the recognised turn-detector model set
├── generate/
│   ├── livekit_v1_build.go      # reads the turn-model set from target/, stops owning it
│   ├── pipecat_v1_build.go      # REQUIRED_ENV derives from required env, not from secrets:
│   ├── pipecat_deploy_test.go   # fixture gains a local tool; asserts import reachability
│   └── templates/
│       ├── pipecat_v1/Dockerfile.tmpl        # conditional COPY tools/
│       ├── pipecat_v1/env.example.tmpl       # UNMUTE_* into the supplied-for-you block
│       ├── livekit_v1/env.example.tmpl       # same
│       └── */README.md.tmpl                  # same, plus the two orphan port knobs
├── scaffold/
│   ├── scaffold.go              # channel-true prompt and greeting; no phantom transport;
│   │                            # secrets block; one model-id constant
│   ├── templates/               # agent.yaml, env.example, instructions.md
│   └── scaffold_test.go         # golden regenerated from what the CLI actually passes
└── skill/
    ├── assets/references/       # transfers.md, models.md, package.md, examples.md
    └── agreement_test.go        # NEW: UNMUTE_ guard; NEW: one-model-id guard

docs/            SCHEMA.md amendment, TELEPHONY.md, PRODUCTION_ROADMAP.md
docs-site/       start/quickstart.mdx, build/your-first-agent.mdx, telephony/*, reference/*
examples/        eleven packages: model id, tracing, README truth
README.md        model id, and the dead OPENAI_MODEL in the root .env.example
```

**Structure Decision**: no new package and no new file outside tests. Every
change lands in a file that already owns the concern. The one genuinely new
piece of logic — walking the package for declarations nothing reaches — goes
into `internal/ir/build.go` beside `checkToolRefs`, which already walks the same
graph in the opposite direction.

## Phasing

The order is forced by two facts: Parts 1 to 3 change generated output, so
Part 4 has to follow them; and no fix starts before its reproduction is red,
which Wave A has already delivered.

Phase numbers below are the same numbers `tasks.md` uses, so a task ID and a
phase row always agree. The two planning phases that produced this document are
listed as 0a and 0b because they are complete and carry no tasks.

| Phase | Content | Gate to leave it |
|---|---|---|
| 0a | Research and decisions (`research.md`) | Every open question answered or recorded as deferred. **Complete** |
| 0b | Design artifacts: message contracts, the reachability model, the quickstart | The exact strings exist to write tests against. **Complete** |
| 1 | Setup: land on the right base, prove the gate is green | `make fmt lint build test` green at `16289f4`. **Complete** |
| 2 | **Red tests**, from Wave A's evidence | Sixteen test tasks producing eleven new or changed test functions, every one failing for the reason its name claims |
| 3 | User Story 1 fixes, one commit per defect group | Groups A to E green; gate green on each commit |
| 4 | User Story 2, the scaffold | `init` then `dev` reaches a greeting on two variables |
| 5 | User Story 3, environment names | Guard tests green; generated files stop contradicting `secrets.mdx` |
| 6 | User Story 5, documentation of enforced rules | Every validator error string findable, or listed |
| 7 | User Story 4, every example | Eleven validate, compile, and run |
| 8 | Wave B and Wave C verification | Raw counts recorded in `results.md` |
| 9 | Polish: `make smoke`, ponytail comments, close the 011 defects | Final gate green, zero lint issues |

The five-surface rule applies inside every phase, not at the end: the skill
bundle moves in the same commit as the behaviour it describes.

## Complexity Tracking

No constitutional violations. Nothing to justify.
