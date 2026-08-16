# Implementation Plan: Spoken Agent Handoffs

**Branch**: `014-spoken-agent-handoffs` | **Date**: 2026-08-15 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/014-spoken-agent-handoffs/spec.md`

## Summary

Add one optional exact `announce:` sentence to `agent_transfer`. Both generators
send it directly to speech once before control changes, without another LLM
turn. LiveKit also marks the initial entry explicitly so
a later handoff back cannot replay the call-start greeting. The live-test defect
in Pipecat tool argument handling is fixed once at the generated worker boundary
so an extra model argument becomes a useful tool error instead of a silent gap.

## Technical Context

**Language/Version**: Go 1.26; generated Python 3.11+

**Primary Dependencies**: Existing `goccy/go-yaml`, `google/jsonschema-go`, LiveKit Agents 1.5.x, Pipecat 1.5.x; no new dependency

**Storage**: N/A

**Testing**: Go unit/build/golden tests, generated-Python smoke checks, and the human live harness on both targets

**Target Platform**: Generated LiveKit and Pipecat Linux containers; browser audio for the live harness

**Project Type**: Compiler CLI

**Performance Goals**: No unexplained handoff silence longer than two seconds after the handoff is selected

**Constraints**: Compile ahead only; no Unmute runtime; gates before speech; one cue before activation; existing silent handoffs remain valid

**Scale/Scope**: One optional field, two shipped generators, scaffold/TUI round-trip, one two-agent example, and the five required documentation surfaces

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Compile ahead**: PASS. The field lowers into generated Python; no Unmute
  process enters the call path.
- **Fail loud**: PASS. Blank or templated instructions fail validation, and the
  new capability row prevents an emitter from silently dropping the field.
- **One source of truth**: PASS. The spec and IR structs derive schemas; the
  target table owns support; emitter agreement tests cover both generators.
- **Document wins**: PASS, conditional on appending dated amendment N44 to
  `docs/SCHEMA.md` before implementation is complete.
- **Whatever compiles can be spoken to**: PASS, conditional on compile, smoke,
  and human calls on both shipped targets.
- **Python boundary**: PASS. Python remains generated template output and
  checked with `ruff`/smoke; no maintained Python package is added.
- **Five documentation surfaces**: PASS, conditional on updating emitted
  README templates, `examples/subagents/README.md`, `docs/SCHEMA.md`, the
  docs-site handoff page, and the bundled orchestration skill.

## Project Structure

### Documentation (this feature)

```text
specs/014-spoken-agent-handoffs/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
└── tasks.md
```

### Source Code (repository root)

```text
internal/spec/                 # strict authoring struct and derived schema
internal/ir/                   # resolved transfer and validation
internal/target/               # support row for the new field
internal/generate/             # lowering, tests, and golden artifacts
internal/generate/templates/   # LiveKit/Pipecat Python and README templates
internal/scaffold/             # init model and YAML emission
internal/tui/                  # handoff editor and maintain round-trip
examples/subagents/            # live proof and natural prompts
docs/                          # locked schema amendment and harness result
docs-site/                     # public handoff guide and field reference
internal/skill/assets/         # coding-agent handoff guidance
```

**Structure Decision**: Extend the existing compiler path in place. Reuse the
current agent-transfer IR and the native handoff APIs; add no service, package,
or abstraction.

## Complexity Tracking

No constitution violations.
