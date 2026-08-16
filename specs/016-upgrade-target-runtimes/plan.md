# Implementation Plan: Upgrade Target Runtimes and Make Version Support Scalable

**Branch**: `016-upgrade-target-runtimes` | **Date**: 2026-08-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/016-upgrade-target-runtimes/spec.md`

## Summary

Raise the supported framework versions to pipecat-ai 1.7.0 and livekit-agents
1.6.10, and make the next raise cheap by giving each framework's supported range
one recorded home that validation, generation, the scaffold, and the compile
report all derive from.

Three things carry the feature. First, **LiveKit starts honoring the author's
declared version** as an exact install pin, as Pipecat already does; today it is
validated and thrown away, which is why the repository's own version records
disagree with each other. Second, because an exact pin cannot silently rewrite
itself, the two **feature floors** that used to raise the pin behind the
author's back (warm transfer and MCP, both 1.6.0) become validation gates that
name what they need. Third, generated LiveKit projects **leave the deprecated
Python CLI entirely**: research found the deprecation covers not just
`agent.py dev` and `console` but `cli.run_app` itself, so projects move to
`python -m livekit.agents start agent.py`, which was confirmed working against
our exact emitted shape with no deprecation warning. Alongside that,
`unmute dev --console` is removed for every target, leaving Docker and compose
as the one local run path.

Nothing about the authoring surface changes: authors keep writing one `version:`
string in the same place. A human proves the result by talking to every example
on a live call before release.

## Technical Context

**Language/Version**: Go 1.26 (`go.mod`). Emitted Python targets 3.10+ (LiveKit)
and 3.11+ (Pipecat).

**Primary Dependencies**: No new Go dependency. The version comparison reuses
the repository's existing `ParseVersion` + `slices.Compare`; a semver module
would be a new dependency for a ten-line function (research R5).

**Storage**: N/A. The support window is compile-time data in the binary.

**Testing**: `make test` (`go test -race ./...`) for L1-L3, which must stay
zero-Python. `make smoke` (opt-in, build tag `smoke`) proves the emitted Python
installs and imports at the new ceilings. Plus the human live-call verification
FR-012 requires, recorded in `results.md`.

**Target Platform**: macOS and Linux developer machines; generated projects run
in Docker.

**Project Type**: Compiler CLI. One binary, `internal/` layout, no services.

**Performance Goals**: N/A. The one measurable target is process cost, not
runtime: a future ceiling bump that needs no template change should touch one
recorded home plus regenerated outputs (SC-002).

**Constraints**: No authoring-surface change (FR-010). No new dependency. Docker
becomes a hard prerequisite for local dev once `--console` is gone. Every
framework claim carries a verification date (Principle IV).

**Scale/Scope**: 2 frameworks, 11 examples, 3 fixtures, roughly 50 hand-written
version strings across docs, docs-site, skill, and examples, and about a dozen
Go tests and 5 goldens that assert on emitted version strings or run commands.

## Constitution Check

*GATE: checked before Phase 0 and re-checked after Phase 1 design.*

| Principle | Verdict | Reasoning |
|---|---|---|
| I. Compile ahead of time | **Pass** | The support window is data compiled into the binary. No runtime layer, no network lookup, no version check at call time. Generated projects keep carrying no Unmute dependency. |
| II. Fail loud, never average | **Pass, and strengthened** | The feature removes a silent downgrade: today a warm-transfer package declaring 1.5.2 has its version quietly rewritten at emission. After this, it is a gated error naming the feature and the floor. Out-of-range and above-ceiling versions fail before any artifact is written. |
| III. One source of truth | **Pass, and the point of the feature** | Five unsynchronized homes collapse into one; the twice-written silero floor gets one home; the feature-use predicate gets one home shared by validation and the driver, or an agreement test that fails on drift. |
| IV. The document wins | **Pass, with required amendments** | Ships a dated, numbered `docs/SCHEMA.md` amendment (emitted LiveKit dependency changes shape; feature floors become gates; versions must be `X.Y.Z`). Every framework claim in research.md carries its verification method and date, and the ceiling claims were verified by installing and running 1.6.10, not from memory. |
| V. Whatever compiles can be spoken to | **Amendment required — see Complexity Tracking** | Removing `--console` deletes a path Principle V names by name and makes Docker mandatory for hearing an agent locally. The four-command path survives (`init`, `validate`, `compile`, `dev`), but its wording must change in the same commit. |

**Other binding rules checked.** Targets and providers: unchanged, no target is
added or removed. Command rules: `--console` removal touches flag registration
only; `RunE`, output plumbing, and exit codes are untouched. Secrets: no new
secret surface. Generated Python: still ruff-clean and smoke-proven. Gates: the
five `make` targets stay as they are, and no gate is loosened — the new
agreement test is added to `make test`.

**Post-Phase 1 re-check.** No change. The Phase 1 design adds one data table,
one validation rule, and command-string changes. It introduces no interface with
one implementation, no config knob for a constant, and no new dependency.

## Project Structure

### Documentation (this feature)

```text
specs/016-upgrade-target-runtimes/
├── plan.md              # This file
├── spec.md              # Feature specification (with Clarifications)
├── research.md          # Phase 0: R1-R14 decisions, all verified
├── data-model.md        # Phase 1: support window, feature floor, verification record
├── quickstart.md        # Phase 1: how to prove the feature works
├── contracts/
│   ├── version-support.md   # Error messages, emitted pins, report fields
│   └── run-commands.md      # The exact commands emitted artifacts run
├── checklists/
│   └── requirements.md  # Spec quality checklist (16/16)
├── results.md           # FR-012 human live-call verification record (filled at verification time)
└── tasks.md             # Phase 2 output, created by /speckit-tasks
```

### Source Code (repository root)

Existing layout; no new packages or directories.

```text
internal/
├── target/              # THE recorded home
│   ├── driver.go            # driverVersions -> support window (floor, ceiling, verified date);
│   │                        # CheckVersion gains ceiling + patch comparison; silero floor stops
│   │                        # being a bare literal
│   ├── versions_test.go     # NEW: window invariants + the agreement sweep (imitates R12)
│   └── catalog*.go          # unchanged
├── ir/
│   └── validate.go          # NEW rule: feature floors (warm transfer, MCP) gate the declared version
├── generate/
│   ├── livekit_v1_build.go  # livekitDeps(): delete the constraint ladder, emit ==Version;
│   │                        # silero floor read from its home
│   ├── livekit_v1.go        # delete livekitVersion* / *VerifiedMinor constants
│   ├── pipecat_v1.go        # unchanged emission; range prose now points at the home
│   └── templates/
│       ├── livekit_v1/      # compose.dev.yaml (run command), Dockerfile (CMD + comment),
│       │                    # compose.telephony.connector.yaml, agent.py (drop cli.run_app block),
│       │                    # README.md (run instructions)
│       └── pipecat_v1/      # pyproject.toml (drop console extra), bot.py (drop console_main),
│                            # README.md, env.example
├── cli/
│   ├── dev.go               # delete --console, runDevConsole, consolePlan, execConsole,
│   │                        # requireInferenceCreds; fix two refusal messages
│   └── dev_web.go           # missing-Docker hint no longer offers --console
├── scaffold/
│   └── scaffold.go          # TargetVersion defaults derive from the ceiling
└── target/telephony.go      # connector process command stays in sync with its template
```

**Structure Decision**: no structural change. The feature is a data
consolidation plus deletions, and it lands in the packages that already own each
surface. `internal/target` is the home because it is the leaf package both
`internal/ir` and `internal/generate` already import, which is what lets one
table serve validation, both drivers, and the scaffold without a cycle.

## Phasing

The three user stories are independently shippable and ordered so each rests on
the one before.

**P1 — the pin becomes real.** The support window lands in `internal/target`,
`CheckVersion` gains ceiling and patch comparison, LiveKit emits `==Version`,
and the two feature floors become validation gates. At the end of this slice a
declared version means what it says on both targets. Goldens and fixtures move
to the new ceilings here.

**P2 — one home, derived everywhere.** The scaffold default, the compile report,
the version output, and every document, example, and skill reference derive from
or agree with the window, held by the agreement test from R12.

**P3 — run paths and human proof.** `--console` is removed, LiveKit projects
move to the thin CLI, the constitution and schema amendments land, and a human
verifies every example on a live call, recording the result in `results.md`.

The run-path work (P3) is separable from the version work (P1/P2) and could ship
first if preferred; it is placed last because the live-call verification should
exercise the final state of both.

## Risks

| Risk | Handling |
|---|---|
| The `"registered worker"` marker that gates the browser opening might not appear under the new run command | Verified present in the shared registration path and in probe runs; confirmed again by the live call. If it moves, `readyWatcher`'s marker is one string in `dev_web.go`. |
| `start` mode's `load_threshold` of 0.7 could make a CPU-starved container stop accepting jobs | Our compose sets no CPU limit. Named as a live-call watch item; the knob is `ServerOptions.load_threshold` if it bites. |
| Dropping `cli.run_app` means `python agent.py start` no longer works for anyone following LiveKit's own docs | Deliberate: it is deprecated and slated for removal. The emitted README documents the replacement command in the same commit. |
| A package using warm transfer or MCP at a version below 1.6.0 starts failing validation | Intended, and the point of R4. No shipped example is affected; the schema amendment states it. |
| The agreement sweep fights goldens and history specs, which legitimately contain old versions | Curated surface set with a documented carve-out, exactly as `TestOneModelIdEverywhere` does. |

## Complexity Tracking

One constitutional violation, stated rather than hidden.

| Violation | Why needed | Simpler alternative rejected because |
|---|---|---|
| Removing `unmute dev --console` narrows Principle V, which names `--console` as the "uv, no Docker" way to hear an agent, and makes Docker a hard prerequisite for local dev | The user's explicit decision, and it removes the last generated path that relies on upstream-deprecated modes. It also leaves one dev path that runs the deployable image, which is what "what you test is what you ship" asks for. | Keeping console on Pipecat only was considered and rejected: it leaves two local paths with different runtimes and different failure modes, and the uv path runs something production never runs. Keeping it on both was rejected because LiveKit's terminal mode prints a deprecation banner at 1.6.10 and its replacement is not a local-microphone session at all. |

The amendment is a **MAJOR** bump of the constitution (2.1.0 → 3.0.0) by its own
versioning policy: a capability a principle names is being removed, so a
contributor holding 2.1.0 would be wrong about what `unmute dev` offers. It must
land in the same change as the code, and it must state what an author without
Docker now does instead.
