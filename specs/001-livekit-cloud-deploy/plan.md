# Implementation Plan: Cloud-deployable builds, region aware, on both shipped targets

**Branch**: `feature/warm-cold-human-transfer` (feature dir `001-livekit-cloud-deploy`) | **Date**: 2026-08-12 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/001-livekit-cloud-deploy/spec.md`

## Summary

Two generated artifacts cannot be deployed to the cloud each was written for, and the fix is the same shape on both: stop emitting what the platform assigns, start emitting what the platform reads, and put the real commands in the generated README. LiveKit stops emitting `livekit.toml` entirely (the platform writes it on first deploy) and gains a Deploy section that is no longer disclaimed. Pipecat stops setting `image` (which switches off the cloud build it depends on), starts naming its secret set, and gains the Deploy section it never had.

On top of that, `deployment_region` widens from one string to one-or-many. LiveKit fans a list out to one `lk agent create` per region with a per-region config file; Pipecat refuses a list of more than one, by name, quoting its own globally-unique-agent-name rule, and documents the extra command instead. Every declared region reaches the compile report, which the constitution already requires of forwarded values and which neither driver does today.

Nothing about transfers changes. The work is four Go packages, six templates, and eight documents, with the goldens on both drivers rewritten deliberately.

## Technical Context

**Language/Version**: Go 1.24 (pinned in `go.mod`), `CGO_ENABLED=0`. Emitted artifacts are Python 3.12 projects plus TOML/YAML/Dockerfiles produced by `text/template`.

**Primary Dependencies**: `spf13/cobra`, `goccy/go-yaml v1.19.2` (line and column on parse errors), `google/jsonschema-go v0.4.3` (schema derived from Go structs), Charm stack for the TUI. No new dependency is needed or proposed.

**Storage**: N/A. `build/<target>/` is a disposable output directory, with one deliberate exception today (`.env` survives a rewrite) and one added here (platform-written `livekit*.toml`).

**Testing**: `go test ./...` covers L1 unit, L2 in-process command, L3 golden. `make smoke` (build tag `smoke`, needs `uv`) proves emitted Python. The live cloud deploy is manual and is the T8 work this unblocks, not a test in the suite.

**Target Platform**: developer machines (macOS, Linux) producing artifacts for LiveKit Cloud and Pipecat Cloud.

**Project Type**: compiler CLI. `spec.Load` → `ir.Build` → `ir.Validate` → `generate.Generate`, one file per command in `internal/cli/`.

**Performance Goals**: N/A. Compile is already sub-second; nothing here is on a hot path.

**Constraints**: compile stays offline and credential-free, so no platform API is called at any stage; no secret value and no platform-assigned identity may appear in any emitted file; Go templates only, no post-generation string surgery; the authoring surface change must not break a package already written.

**Scale/Scope**: ~4 Go packages touched (`spec`, `ir`, `target`, `generate`, plus `cli` and `tui`/`scaffold` edges), 6 templates, 8 documents, 2 examples, and the golden set for both drivers. No NEEDS CLARIFICATION remain; the three that existed were resolved in the spec's Clarifications section.

## Constitution Check

*GATE: passes. Checked against `.specify/memory/constitution.md` v2.0.0.*

| Principle | How this plan satisfies it |
|---|---|
| **I. Compile ahead of time** | No runtime layer is added and no platform is called at compile time. Region values are lowered into commands and manifests at generate time. `build/` stays disposable except for files the *platform* wrote, which the compiler never authors. Python stays template-only. |
| **II. Fail loud, never average** | A Pipecat instance with more than one region is a **gated** error naming the target and quoting Pipecat's own words, before any artifact exists. A duplicate region is an error, never silently deduplicated. Nothing degrades to a warning, and no region is invented when none is declared: each README states what the platform does instead. |
| **III. One source of truth** | The region fact keeps one home (`deployment_region`); no plural twin field is added. The multi-region rule lands as one row in `internal/target/table.go` and is read by validation and by the emitters, never re-described. Both derived schemas stay derived; the one `TypeSchemas` override needed follows the existing IR precedent rather than hand-authoring JSON. Each generated README is the single home for its platform's commands, and the repository documents point at it. |
| **IV. The document wins** | Every platform claim carries its source and the 2026-08-12 verification date. `docs/SCHEMA.md` gains amendment **N32** in the same change as the code, and `docs/user/` is corrected where this change falsifies it. The per-driver invariants that used to hold these facts (`compiler.md` V35, `driver-livekit.md` V27, `driver-pipecat.md` V29) retired with `docs/spec/` in commit `063289c`, so their role passes to this feature's [contracts/](./contracts/) and to the tests that encode them. One thing those retired rows got wrong is not carried forward: V27 printed a fixed list of region codes, and a platform's code list is exactly what goes stale. |
| **V. Whatever compiles can be spoken to** | This closes the last gap between a green compile and a call you can hear: the artifact now reaches the cloud the transfers need. `validate` reach is unchanged (all four providers); `compile` reach is unchanged (two drivers). |
| **Secrets** | No secret value is written anywhere. LiveKit's flow passes the generated `.env` to the platform CLI; Pipecat's names a secret set the user creates from the same file. Names only, `UPPER_SNAKE`, as today. |
| **Gate** | `make fmt`, `make lint`, `make build`, `make test` must pass; `make smoke` stays opt in. Goldens are regenerated with their own `-update` flags and the diffs are read before committing. |

**No violations to justify**, so Complexity Tracking below is empty. Two deliberate simplifications are recorded there instead, because both have a stated ceiling.

**Re-checked after Phase 1 design: still passes.** The design added one thing worth re-examining against Principle III, the `TypeSchemas` override for the one-or-many field, and it holds: it is the library's documented hook for a type reflection cannot express, `internal/ir/schema.go` already uses it for the same reason, and no `.json` schema file is hand-authored. The design also added one fact that could have become a second copy, the Pipecat secret-set name, and it does not: the emitter writes it into the manifest and the README prints the command that creates it, both from the same generated value.

## Project Structure

### Documentation (this feature)

```text
specs/001-livekit-cloud-deploy/
├── plan.md              # This file
├── research.md          # Phase 0: the two platform contracts and the four mechanism decisions
├── data-model.md        # Phase 1: the region type through spec → ir → drivers, and the artifact file sets
├── quickstart.md        # Phase 1: how to prove it, offline then on both clouds
├── contracts/
│   ├── authoring.md     # what `deployment_region` accepts and what each shape means per target
│   ├── artifacts.md     # exact emitted file set and manifest keys per target
│   └── deploy-commands.md # the command sequences each generated README must print
├── checklists/
│   └── requirements.md  # spec quality checklist (already passing)
└── tasks.md             # Phase 2 output, created by /speckit-tasks — NOT by this command
```

### Source code (repository root)

```text
internal/
├── spec/
│   ├── regions.go              # NEW: type Regions []string, one-or-many YAML decode
│   ├── package.go              # Target.DeploymentRegion becomes Regions
│   └── schema.go               # TypeSchemas override so the derived schema shows oneOf
├── ir/
│   ├── compiler.go             # Target.DeploymentRegions []string (resolved: always a list)
│   ├── build.go                # normalise: trim, keep order, reject empties
│   └── validate.go             # duplicate check + multi-region capability gate
├── target/
│   └── table.go                # NEW field row: deployment_region.multiple, denied off LiveKit
├── generate/
│   ├── livekit_v1.go           # drop livekit.toml from the emitted set; regions in the report
│   ├── livekit_v1_build.go     # per-region deploy views for the template
│   ├── pipecat_v1.go           # regions in the report
│   ├── pipecat_v1_build.go     # secret-set name; region stays single
│   └── templates/
│       ├── livekit_v1/
│       │   ├── livekit.toml.tmpl   # DELETED
│       │   ├── README.md.tmpl      # Deploy section rewritten (Cloud first-class, per region)
│       │   └── Dockerfile.tmpl     # non-root run user
│       └── pipecat_v1/
│           ├── pcc-deploy.toml.tmpl # no image key; secret_set when secrets exist
│           └── README.md.tmpl       # NEW Deploy section
├── cli/
│   └── compile.go              # preserve platform-written livekit*.toml alongside .env
├── scaffold/
│   ├── scaffold.go             # Data.DeploymentRegions []string
│   └── templates/targets.yaml.tmpl # scalar when one, list when several
└── tui/
    ├── tui.go                  # region prompt: comma-separated, so a list round-trips
    └── maintain.go             # carry the list through packageData

docs/
├── SCHEMA.md                   # amendment N32 (the one-or-many widening)
├── TRANSFERS.md                # section 4: both rigs, current CLI names, secret steps
├── user/reference/targets-yaml.md  # one-or-many, unset behaviour per platform
├── user/reference/secrets.md   # deploy-time secret injection, if it describes it today
└── user/learn/08-going-live.md # both platforms' deploy paragraphs corrected

# docs/spec/ is retired (commit 063289c). The invariants it held for these
# drivers now live in specs/001-livekit-cloud-deploy/contracts/ and in tests.

examples/
├── human-transfer/targets.yaml # declares a region so the T8 rig is non-interactive
├── human-transfer/README.md    # deploy step points at the generated README
└── human-transfer-daily/README.md # same, and current CLI names
```

**Structure Decision**: no new packages and no new layout. Every change lands in an existing file of the four-stage pipeline, plus the templates those stages render. `internal/target` stays a leaf package, so the new capability row is readable from both `ir` and `generate` with no cycle.

## Phase 0: research

See [research.md](./research.md). It records the two platform contracts with sources and the four mechanism decisions that were not obvious: how a one-or-many YAML field keeps positioned errors and a derived schema, where the multi-region refusal lives, how the platform-written config file survives a recompile, and why the Pipecat `image` key is the whole Pipecat bug.

## Phase 1: design

See [data-model.md](./data-model.md) for the region type as it moves through the pipeline and for the exact emitted file sets, and [contracts/](./contracts/) for the three contracts this feature is judged against: what authors may write, what each artifact must contain, and what commands each generated README must print. [quickstart.md](./quickstart.md) is the validation guide: what proves this offline in `go test`, and what proves it on the two clouds.

## Implementation phases

Ordered so that each phase leaves the suite green. The schema widening comes first because everything downstream reads it; the two emitters are independent of each other after that.

**A. Region type through the pipeline.** `spec.Regions` with a one-or-many decoder, the schema override, the IR field as a normalised list, the duplicate check. Nothing user-visible changes yet: a scalar still behaves exactly as before, which is what keeps the suite green mid-phase.

**B. The multi-region rule.** One row in the capability table, denied on Pipecat, Vapi, and Deepgram with each platform's own reason; applied in `ir.Validate` only when more than one region is declared; the LiveKit emitted-fields map gains the row so the agreement test stays honest.

**C. LiveKit artifact.** Delete the template and its entry in the emitted set, rewrite the Deploy section around real commands, add the per-region views, add the non-root user, widen the ignore list, put the regions in the report.

**D. Pipecat artifact.** Drop the `image` key, add `secret_set` when the package declares secrets, add the Deploy section, put the region in the report.

**E. Recompile safety.** Generalise the existing `.env` preservation in `writeArtifactFiles` to also keep any `livekit*.toml` the platform wrote, with the one test that fails if it stops working.

**F. Edges.** Scaffold data and template for the list form, the TUI prompt as a comma-separated value so a multi-region package survives a maintain round-trip, `maintain.go` carrying the list.

**G. Documents.** `SCHEMA.md` N32, the three driver/compiler spec docs, the two user docs, `TRANSFERS.md` section 4, both example READMEs, and the example's `targets.yaml`.

**H. Goldens and tests, read not regenerated.** Both drivers' goldens, the assertion that currently demands `livekit.toml` exists (inverted), the region test rewritten for lists, new tests for the decoder, the gate, the preservation, and both manifests.

## Complexity Tracking

No constitution violations. Two deliberate simplifications, each with its ceiling named, so that simple reads as intent:

| Simplification | Ceiling | Upgrade path |
|---|---|---|
| The TUI region prompt is one comma-separated text field rather than a list editor. | Fine for two or three regions; unpleasant beyond that. | A repeatable list widget in the target form, if anyone ever declares that many. |
| Preservation matches `livekit*.toml` by name pattern rather than tracking which files the platform wrote. | Correct as long as the emitter never emits a file matching that pattern, which FR-008 forbids. | Record the preserved set in the compile report if the emitter ever needs such a name. |
