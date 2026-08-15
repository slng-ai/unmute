# Phase 1 Data Model: Complexity Cleanup

**Date**: 2026-08-15 | **Plan**: [plan.md](./plan.md)

This feature adds no domain types to the compiler. The IR, the authoring spec
structs, and the capability table are all untouched — SC-007 requires it. The
entities below are the verification harness's own, and they live only in the
build-tagged test.

---

## Finding

One audited item of over-engineering. This is a planning entity, not a runtime
one: it exists in `tasks.md`, not in code.

| Field | Type | Notes |
|---|---|---|
| Location | file and symbol | Where the code is today |
| Removed | text | What goes |
| Replacement | text | What takes its place, or nothing |
| Story | P1–P5 | Which user story owns it |

**Rules**: every Finding belongs to exactly one Story. Sixteen findings in
scope; three refused ones are recorded in the spec's Out Of Scope and are not
Findings.

---

## Example

One package under `examples/` that the sweep knows how to run.

| Field | Type | Notes |
|---|---|---|
| Name | directory name | e.g. `salon-support` |
| Targets | list of target names | Each becomes one Session |
| Runnable | boolean | False when the example declares a telephony channel |
| Vars | map name → value | `--var` seeds; empty for five of seven |

**Rules**:

- Eleven Examples exist. Seven are runnable; four declare a telephony channel
  and are compile-only under FR-031.
- Runnable Examples contribute thirteen Sessions in total: six declare two
  targets, one declares a single target.
- `Vars` is non-empty only where a `call_start` variable feeds the greeting.
  `salon-support` is the confirmed case (R5).
- `Runnable` is derived by reading the package, never hand-maintained, so an
  example added later is classified correctly without editing the harness.

---

## Session

One runnable Example on one target, taken through browser dev mode.

| Field | Type | Notes |
|---|---|---|
| Example | Example | Owner |
| Target | target name | `pipecat` or `livekit` |
| Outcome | Passed / Failed / Blocked | See state rules below |
| Detail | text | Failure reason, or what was missing when blocked |
| Duration | duration | For spotting a sweep that is degrading |

**State rules**:

- **Passed** — the dev server started, the page connected, and an inbound audio
  track reported bytes received before the timeout. This is the whole of
  FR-030; spoken input is not part of it.
- **Failed** — anything reached and did not work. A missing secret is a Failure,
  never a skip (FR-035).
- **Blocked** — required tooling is absent: no Docker daemon, no Playwright.
  Distinct from Failed on purpose, because it says nothing about the code under
  test.
- Blocked and Passed must never be collapsed in a report. Collapsing them would
  turn an unrun sweep into a green one, which is the silent downgrade the
  constitution's second principle forbids.

---

## Sweep

One full pass of the thirteen Sessions, tied to the story that preceded it.

| Field | Type | Notes |
|---|---|---|
| Story | P1–P5, or `baseline` | Which story had just landed |
| Commit | git sha | What was tested |
| Sessions | list of Session | Thirteen entries |
| ByteDiff | Clean / Dirty | Result of the FR-001 tree comparison |
| Verdict | Green / Red / Incomplete | Derived, see below |

**Derivation**: `Green` when all thirteen Sessions Passed **and** ByteDiff is
Clean. `Incomplete` when any Session is Blocked and none Failed. `Red` otherwise.
Only a Green Sweep satisfies SC-008 and SC-010.

**Rules**: five Sweeps across the feature, or four if User Story 5 is dropped.
A baseline Sweep is taken before the first story so a pre-existing failure is
never attributed to the cleanup.

---

## Required secret set

The union of secret names the seven runnable Examples declare.

| Field | Type | Notes |
|---|---|---|
| Names | set of names | Twenty-five today |
| Present | set of names | Read from the root `.env` |
| Missing | set of names | `Names − Present` |

**Rules**:

- Names only. No field of this entity ever holds a value, and no report, log, or
  artifact derived from it may either (FR-036).
- Computed before the first container builds (FR-037).
- A non-empty `Missing` fails the Sweep before any Session runs. Today it holds
  exactly `FIRECRAWL_MCP_URL` (FR-038).

---

## Byte baseline

The compiled output of all eleven Examples, captured before the first edit.

| Field | Type | Notes |
|---|---|---|
| Tree | directory | One build directory per example and target |
| Commit | git sha | The pre-cleanup commit |
| RuffVersion | version string | Pinned; formatting depends on it (R3) |

**Rules**:

- Compared with `.env` and `livekit*.toml` excluded, because
  `preservedPatterns` restores them deliberately.
- A comparison is only valid when `RuffVersion` matches on both sides, or when
  neither side had `ruff` at all.
- Captured once. Re-capturing after a story would compare the change against
  itself and prove nothing.

---

## Relationships

```text
Finding ──belongs to──> Story (P1..P5)

Story ──is followed by──> Sweep
                            │
                            ├──contains 13──> Session ──runs one──> Example × Target
                            │
                            └──compares against──> Byte baseline

Required secret set ──gates──> Sweep   (checked before any Session)
```

**Cardinality**: 16 Findings → 5 Stories → 5 Sweeps → 13 Sessions each = 65
Sessions, against 1 Byte baseline and 1 Required secret set.
