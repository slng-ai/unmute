# Implementation Plan: The connection owns the phone route

**Branch**: `008-simplify-telephony-connections` | **Date**: 2026-08-14 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/008-simplify-telephony-connections/spec.md`

## Summary

A target stops describing how a call reaches it. It names one connection, and the
connection file declares the whole route: `transport`, `carrier`, and the account
environment names. `destinations` leaves the target for `agent.yaml` and narrows
to environment-variable names. The `secrets:` block stops exempting telephony
names. Every refusal along the way names the file, the key, and the fix.

The technical approach that makes this small: **only the authoring surface
changes.** `spec.Target` loses three fields and `spec.Connection` gains two;
`ir.Target` keeps its shape exactly, and `ir.buildTarget` fills `Transport`,
`Carrier`, and `Destinations` from their new homes. Every reader downstream —
both generators, `validate`, the `dev` commands, the golden files — sees the same
resolved struct it sees today and needs no edit. FR-003a mandates this for
transport and carrier; research extends the same rule to destinations for the same
reason.

**Two traps**, both found by reading the capability table and the fixtures rather
than the examples:

1. `carrier` is **not** only a route field. On `vapi` and `deepgram` — the two
   providers with no driver, no route rows, and no connection — it gates four
   control capabilities on its own. Removing it from the target therefore requires
   removing the condition from those four rows in the same change (FR-001a), or
   `internal/testdata/safe_core` fails with a refusal naming a fix no author can
   perform. Research R11 has the measurement.
2. The carrier-less Daily route has no row in the capability table, so FR-011
   cannot be a flat triple lookup without breaking
   `pipecat-human-transfer-daily`. Research R10 has the branch and why adding a
   row is the wrong fix.

## Technical Context

**Language/Version**: Go 1.24, pinned in `go.mod`. `CGO_ENABLED=0`.

**Primary Dependencies**: No new dependency. `goccy/go-yaml` (strict decode with
line/col), `google/jsonschema-go` (schema derived by reflection), `cobra`. The
Charm stack is touched only in `internal/tui` for the route picker.

**Storage**: Files. `targets.yaml`, `agent.yaml`, `connections/*.yaml` in a
package; `build/<target>/` is disposable output.

**Testing**: `go test ./...` covers L1 unit, L2 in-process command, and L3 golden.
`-update` regenerates goldens. `make smoke` (build tag `smoke`) is opt-in and
needs Python; it stays out of the default suite and the PR gate.

**Target Platform**: `unmute` CLI on darwin and linux.

**Project Type**: Compiler CLI. One declarative package in, orchestrator-native
Python project out.

**Performance Goals**: N/A. Nothing here is on a hot path; the work is decode,
validate, and template.

**Constraints**: No hand-authored `.json` schema — both schemas are derived from
Go structs. No second telephony capability table. Strict decode reports file,
line, and column. Emitted Python must pass `ruff` and `ty` in the smoke suite.

**Scale/Scope**: 5 telephony example packages, 13 catalog routes, 54 functional
requirements. Estimated blast radius: 2 authoring structs, 1 IR builder function,
3 validation sites, 2 documentation-agreement tests, 5 examples, 8 doc pages, 1
new doc page.

## Constitution Check

*GATE: passed before Phase 0, re-checked after Phase 1 design. Re-check result at
the bottom of this section.*

| Principle | Gate | Verdict |
|---|---|---|
| **I. Compile ahead of time** | The four stages stay `spec.Load` → `ir.Build` → `ir.Validate` → `generate.Generate`, in that order, with validation a gate rather than a warning. No runtime layer is added. Driver vocabulary must not leak into `internal/spec` or `internal/ir`. | **Pass.** The change is inside stages one and two. `transport` and `carrier` values are already driver vocabulary carried opaquely by `spec` and `ir`; moving which file they are read from does not change that. |
| **II. Fail loud** | Strict decoding with file, line, column. "A field the schema moved MUST report the new form and quote the offending line, never a bare *unknown field*." No silent downgrade. | **Pass, and the constitution is stricter than the spec here.** FR-007 is not merely allowed, it is required by this bullet: `transport`, `carrier`, and `destinations` on a target are moved fields and each must name its new home. Recorded in `contracts/errors.md`. |
| **III. One source of truth** | `internal/target` stays the only capability rulebook. The route stays the exact `(orchestrator, transport, carrier)` triple. Schemas derived from Go structs, never hand-authored. Existing agreement tests stay green and must not be weakened. | **Pass with two recorded deviations** (see Complexity Tracking). The capability table is untouched. FR-027 rewrites an agreement test so it keeps protecting the same claim rather than passing vacuously — a strengthening, not a weakening. FR-027a adds one. |
| **IV. The document wins** | A change to the authoring surface lands as a numbered, dated amendment in `docs/SCHEMA.md`, appended and never rewritten in place; superseded clauses stay as history. Provider claims carry a verification date. | **Pass.** FR-021 is that amendment and names all four superseded clauses. No provider claim changes: the route catalog, its evidence, and its dates are untouched. |
| **V. Whatever compiles can be spoken to** | `unmute dev` reaches what `compile` reaches; the browser path is the default; `unmute validate` MUST work for every declared provider whether or not that provider's driver ships. | **Pass, with the four-row change.** FR-018 and FR-018a protect the browser path. Validate keeps working on all four providers only because FR-001a removes the carrier condition from the four Vapi/Deepgram capability rows alongside removing the field. Without that half, `safe_core` fails on `deepgram` with a fix no author can perform. Analysis F1; the alternative that kept `carrier` on driverless targets was specified and withdrawn the same day. |

**Post-Phase-1 re-check**: unchanged. The design adds no package, no dependency,
and no second table. The two deviations below were both present in the spec before
design and neither grew.

## Project Structure

### Documentation (this feature)

```text
specs/008-simplify-telephony-connections/
├── plan.md              # This file
├── research.md          # Phase 0: decisions, with the code that forced each one
├── data-model.md        # Phase 1: the authoring structs, before and after
├── quickstart.md        # Phase 1: how to prove the feature works
├── contracts/
│   ├── authoring.md     # The file shapes a package author writes
│   ├── errors.md        # Every refusal, verbatim, with what triggers it
│   └── environment.md   # Where each environment name is declared and documented
├── checklists/
│   └── requirements.md  # Spec quality checklist (16/16)
└── tasks.md             # Phase 2 output — NOT created by /speckit-plan
```

### Source Code (repository root)

```text
internal/
├── spec/                    # authoring surface — the only structs that change shape
│   ├── package.go           # Target loses transport/carrier/destinations;
│   │                        # Connection loses kind, gains transport/carrier;
│   │                        # Agent gains destinations
│   ├── load.go              # readConnections: unchanged mechanics, new struct
│   ├── schema.go            # derived authoring schema — follows the structs
│   └── authoring_surface_test.go   # the two route-shape tests move to the new form
├── ir/                      # resolved surface — shape unchanged (FR-003a)
│   ├── build.go             # buildTarget: read route from the connection,
│   │                        # destinations from the agent; three guards collapse,
│   │                        # one widens; validateTelephonyEnvironment keeps its job
│   ├── validate.go          # referencedEnvNames: drop the connection exemption,
│   │                        # add destinations; transfer refusals name the connection
│   └── compiler.go          # ir.Target keeps Transport/Carrier/Destinations
├── target/
│   └── telephony.go         # capability table UNTOUCHED; add a selectable-routes
│                            # helper so error lists and the picker exclude
│                            # placeholder rows (FR-011a)
├── generate/                # no reader changes — the resolved struct is identical
│   ├── templates/           # emitted README template restated (FR-025)
│   └── examples_test.go     # FR-027 rewrite + FR-027a new check
├── scaffold/                # writes the new shape
├── tui/                     # route picked as one choice, not two free-text fields
└── cli/                     # unchanged; FR-018/018a are regression guards here

examples/                    # all five telephony packages rewritten
docs/                        # SCHEMA amendment + 6 user pages + 1 new page
```

**Structure Decision**: The existing `internal/` layout, unchanged. No new package
is created and no file moves between packages. The feature is deliberately shaped
so the blast radius stops at `internal/spec` and one function in `internal/ir`,
which is what keeps the golden files and both generators out of it.

## Complexity Tracking

Two places where this feature knowingly departs from Principle III's "one home, or
one test that fails". Both were decided with the author and are recorded in the
spec's Clarifications; they are repeated here because a gate deviation belongs in
the plan, not only in the spec.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| An environment **name** is written twice: in the connection (`account_sid: TWILIO_ACCOUNT_SID`) and in `secrets:` (FR-005b). Principle III wants one home or a failing test; the agreement here is the V10 cross-check, which **warns** rather than fails. **Resolution: FR-005i requires this to be recorded in `docs/SCHEMA.md` as a dated, numbered exception stating the cost**, in the same shape as the tool `output` exception (N22). A deviation recorded only here is one the next reader never finds. | The two lines answer different questions. The connection says which *role* the variable plays on the route, which `secrets:` cannot express. `secrets:` says the runtime requires it, which the connection cannot express for names no author writes. | Keeping the exemption (one home, no duplication) was the recommendation and was rejected by the author: it leaves no single list of what a package needs to run. Deriving `secrets:` from every site — Principle III's own preferred answer — was offered as option C and rejected as far larger than this feature. Raising the cross-check from warn to error was rejected in FR-005e because the severity is governed by the pre-existing §4.12 rule for *every* env name, and changing it for telephony alone gives an author two behaviours with no principle between them. Widening it for all names is deferred as its own change. |
| FR-005f's "nothing extra" half — a variable the reader never sets is not mentioned in the docs — is an **editorial rule with no test**. | A check strict enough to catch a stray mention would also fail deliberate teaching lines, such as `twilio-telephony-hello`'s use of `11LABS_API_KEY` to show a name that cannot be exported. | Scoping a two-way check to the README's env code block was offered and rejected by the author as machinery out of proportion to the risk. The "nothing missing" half — the half that costs a live call when it rots — *is* checked (FR-027a). |
