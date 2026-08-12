# Implementation Plan: Pipecat Cloud telephony on the Daily route

**Branch**: `004-pipecat-cloud-telephony` | **Date**: 2026-08-12 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/004-pipecat-cloud-telephony/spec.md`

## Summary

Five pieces of work, only one of which is a new capability.

1. **Fix the Daily inbound defect.** The emitted `bot.py` registers a generic `TransportParams` factory under the `"daily"` key. Pipecat's `create_transport` fills inbound call details onto whatever that factory returns, and `TransportParams` is a Pydantic model that declares none of them and allows no extras. Emit `DailyParams` for that key instead. This is the only thing standing between the current code and a Daily call that answers.
2. **Surface route prerequisites.** Cold transfer on Daily dials out, which needs a Daily account feature granted on request. The fact has no home today. Put it in `internal/target` as route data, print it from `validate`, and carry it into the emitted README.
3. **Finish the region story.** The emitted README already gets this substantially right. What is missing is a refusal when a region cannot be honoured, and the same facts reaching `validate` rather than only the generated document.
4. **Correct two documents.** `docs/DEPLOYMENT.md` and `docs/TELEPHONY.md` state a deployment stance the project no longer holds, while keeping the local cloud-free claim true and clearly scoped to local.
5. **Guard the Daily transfer against a second attempt.** Found by `/speckit-analyze`, not by the original planning pass. `bot.py.tmpl:298` calls the Daily transfer primitive with no idempotency guard. The carrier routes guard it in the shared control store, which the Daily route does not have and must not gain: `contracts/artifacts.md` forbids that service here, and the constitution forbids an idle one. So the guard has to be in-process for the life of the call.
6. **Lock it down.** Offline tests for the inbound params shape, the prerequisite text, the region agreement, the transfer guard, the two route shapes staying distinct, the "no new authoring field" rules, and backward compatibility across every example.

The approach is deliberately conservative: no new authoring field, no new dependency, no new package. The one structural change is that route facts which currently only exist inside a telephony plan need a home for a route that has no telephony plan.

## Technical Context

**Language/Version**: Go 1.24, pinned in `go.mod`. Emitted Python is `text/template` output, never maintained here.

**Primary Dependencies**: no new ones. `spf13/cobra`, `goccy/go-yaml`, `google/jsonschema-go`, Charm stack. The emitted project pins `pipecat-ai==1.5.0`.

**Storage**: N/A. Route facts are Go literals in `internal/target`.

**Testing**: `go test ./...` covers L1 unit, L2 in-process command, L3 golden. L4 smoke (`make smoke`, build tag `smoke`, needs `uv`) proves the emitted Python against the real pinned package and is the only layer that can catch the `DailyParams` fix being wrong.

**Target Platform**: the CLI runs on macOS and Linux. The emitted project deploys to Pipecat Cloud and runs against Daily PSTN.

**Project Type**: compiler and CLI. `internal/`, not `pkg/`.

**Performance Goals**: N/A for the compiler. The one caller-facing budget is spec SC-011: the caller hears something within 2 seconds of asking for a person. It is met by the existing announce-then-transfer ordering, not by new work.

**Constraints**: `make test` must pass with zero Python. No secret value in any package, generated file, or report. Emitted projects carry no Unmute dependency. Existing packages must keep their meaning.

**Scale/Scope**: one Pipecat telephony route (Daily). Four Pipecat carrier websocket routes left untouched. Two documents corrected. One example kept green.

## Constitution Check

*GATE: evaluated before Phase 0, re-evaluated after Phase 1.*

| Principle | Verdict | Notes |
|---|---|---|
| I. Compile ahead of time | **Pass** | No runtime layer. The fix changes which parameter object the generated code constructs. The emitted project still runs with Unmute absent, and `build/` stays disposable. |
| II. Fail loud | **Pass, with a decision** | A region that cannot be honoured becomes a `gated` error. The account prerequisite is *not* a field failure, so it must not be forced into a tag. It is setup guidance and belongs in the validate report and the emitted README. Recorded in research.md as D3 so it is not mistaken for a silent downgrade. |
| III. One source of truth | **Pass, with a required test** | The prerequisite fact goes in `internal/target` only. The emitter and the docs read it. An agreement test must fail if the emitted README, the capability rulebook, and `docs/user/` disagree. No second telephony table. |
| IV. The document wins | **Pass, with amendments** | `docs/SCHEMA.md` gains a numbered dated amendment recording that Pipecat telephony is the Daily route. `docs/DEPLOYMENT.md` and `docs/TELEPHONY.md` are corrected. The `DailyParams` claim carries its verification date and source. |
| V. Whatever compiles can be spoken to | **Pass, with a documented boundary** | See below. This is the one place where a principle and a decision pull against each other. |

**The Principle V boundary.** Principle V says `unmute dev` reaches exactly the providers `compile` reaches, and that an author can speak to whatever compiles. A Daily package compiles, and an author can speak to it in the browser and in the terminal. What they cannot do is speak to it **over a real phone locally**, because Daily PSTN delivers calls through Daily's own infrastructure to a deployed agent, and the requester's decision keeps local runs free of any cloud account. So `unmute dev --telephony` has nothing to offer on the Daily route.

This does not violate Principle V, which promises a voice you can talk to, not a phone call from every mode. It does mean the limit has to be stated rather than discovered: `--telephony` on a Daily target must refuse with a message naming the two modes that do work and pointing at the deploy path. A silent no-op here would be exactly the "field that silently does nothing" Principle II forbids. Captured as task group E.

**No complexity violations.** No new dependency, no new abstraction, no interface with one implementation, no config knob for a value that never changes.

## Project Structure

### Documentation (this feature)

```text
specs/004-pipecat-cloud-telephony/
├── plan.md              # This file
├── research.md          # Phase 0: the six decisions and their verification
├── data-model.md        # Phase 1: the route-fact entities and their rules
├── quickstart.md        # Phase 1: how to prove this works, offline then live
├── contracts/
│   ├── authoring.md     # What the yaml does and does not gain
│   ├── cli-behaviour.md # validate, compile, and dev output contracts
│   └── artifacts.md     # What the emitted project must contain per route
└── tasks.md             # Phase 2, created by /speckit-tasks, not here
```

### Source Code (repository root)

Only files this feature touches. Everything else in the tree is unchanged.

```text
internal/
├── target/
│   ├── telephony.go          # route facts: add the Daily route's account
│   │                         #   prerequisites and its region constraint
│   ├── table.go              # the cold-transfer row already gates on
│   │                         #   transport daily-sip; confirm and leave alone
│   └── table_test.go         # prerequisite + region agreement tests
├── ir/
│   ├── validate.go           # emit the prerequisite notice and the region refusal
│   └── validate_test.go
├── generate/
│   ├── pipecat_v1.go         # pipecatData gains the params-class and
│   │                         #   prerequisite fields
│   ├── pipecat_v1_build.go   # choose DailyParams on the Daily route; carry
│   │                         #   prerequisites into the README model
│   ├── pipecat_v1_test.go    # L1/L3: params class, prerequisite text, region
│   ├── pipecat_v1_smoke_test.go  # L4: instantiate a Daily bot for real
│   ├── templates/pipecat_v1/
│   │   ├── bot.py.tmpl       # the "daily" transport_params entry + its import
│   │   └── README.md.tmpl    # prerequisites section; region wording
│   └── testdata/golden/      # regenerated with -update-pipecat
├── cli/
│   ├── dev.go                # refuse --telephony on the Daily route, by name
│   └── dev_test.go
docs/
├── DEPLOYMENT.md             # stance corrected, dated
├── TELEPHONY.md              # cloud-free claim scoped to local runs
├── SCHEMA.md                 # numbered dated amendment: Pipecat telephony is Daily
├── TRANSFERS.md              # recipe updated; Status records the dated run
└── user/                     # the pipecat target page follows the route facts
examples/
└── human-transfer-daily/     # exists and validates; kept green, README aligned
```

**Structure Decision**: no new packages and no new files outside the golden directory. The work lands in the four packages that already own these concerns: `internal/target` owns route facts, `internal/ir` owns validation, `internal/generate` owns emission, `internal/cli` owns command behaviour. `internal/target` stays a leaf package so `ir` and `generate` can both import it without a cycle, which the constitution requires.

## Phase 0: Research

See [research.md](research.md). Six decisions, each with its verification source and date:

- **D1** the exact parameter class and import path for the Daily fix, verified against the pinned version rather than the docs, because the docs show two different import spellings.
- **D2** how the fix stays scoped so the browser and terminal paths keep the generic parameters and no golden churns unnecessarily.
- **D3** where an account prerequisite belongs, given it is not a field and must not be forced into a capability tag.
- **D4** what "region reaches every reference" still needs, given the emitted README already does most of it.
- **D5** how to correct the two documents without deleting the local claim that stays true.
- **D6** whether a `docs/SCHEMA.md` amendment is required when no authoring field changes.

## Phase 1: Design

- [data-model.md](data-model.md): the route-fact entities, their fields, and the rules that must hold. No persistent storage; these are Go literals and derived report rows.
- [contracts/authoring.md](contracts/authoring.md): the authoring surface contract, which is mostly a statement of what does **not** change, so a later reader does not re-add a hosting field.
- [contracts/cli-behaviour.md](contracts/cli-behaviour.md): the exact output contracts for `validate`, `compile`, and `dev` on the Daily route, including the refusal wording rules.
- [contracts/artifacts.md](contracts/artifacts.md): what the emitted project must and must not contain, per route, so the two hosting shapes cannot leak into each other.
- [quickstart.md](quickstart.md): the offline proof anyone can run, then the credentialed live run that closes the transfer reference's Status section.

## Post-Design Constitution Re-Check

Re-evaluated after the Phase 1 artifacts were written, then again after `/speckit-analyze`. No verdict changed, but the analysis pass caught two things the design pass had not.

Two things the design surfaced that the pre-check had not:

1. **The prerequisite fact creates a third place a route is described** unless it lives in `internal/target`. The contracts pin it to the rulebook with an agreement test, which keeps Principle III intact. Had it been added to the emitter directly it would have been a second copy, which is exactly the failure mode the constitution's rationale names.
2. **The `--telephony` refusal is required, not optional.** The pre-check treated it as a documentation boundary. Writing `contracts/cli-behaviour.md` made it clear that leaving the flag as a silent no-op would breach Principle II, so it became a required refusal with named alternatives. It is now spec FR-028, so the scope lives in the spec rather than only here.

Two more the analysis pass caught, both corrected on 2026-08-12:

3. **A caller-identity field would have breached the compliance review.** User Story 5 originally let the author choose the caller identity an outbound recipient sees. No such field exists anywhere in the schema today, so it was a new authoring surface, and the constitution requires such a change to land in one commit with a numbered SCHEMA amendment, the derived schemas, a capability row, the agreement tests, the scaffold templates, the interactive console, the examples, and `docs/user/`. The three tasks planned for it covered none of that. Caller identity moved to Out of Scope as its own feature, and FR-003 now states the full checklist so the next attempt cannot under-scope it.
4. **The Daily transfer had no idempotency guard and could not use the usual one.** Spec FR-008 requires at most one transfer per call. The carrier routes satisfy it through the shared control store; the Daily route has no such store and must not gain one. This was a requirement with zero coverage sitting on top of an unimplemented behaviour, on a caller-facing path. It is now workstream 5 in the Summary, with an in-process guard and the reason recorded in `contracts/artifacts.md`.

## Complexity Tracking

No constitution violations require justification. The table is intentionally empty.

The one thing that came close was caller identity, and the resolution was to remove it from scope rather than to justify a partial implementation. A new authoring field carries a fixed cost the constitution sets, and there is no cheaper version of it.
