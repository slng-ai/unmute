# Phase 1 Data Model: Work Inside The Agent Folder, LiveKit By Default

**Feature**: 015-cwd-livekit-defaults

This feature adds no persisted data, no schema field, and no new authoring
construct. `docs/SCHEMA.md` is untouched (research.md D10). What follows is the
small amount of state the change actually introduces, written down so the
implementation has one description to work from.

## Entity: Resolved package directory

The single value the change produces. It exists only for the duration of one
command.

| Attribute | Description |
|---|---|
| Source | the positional argument, or the process working directory when absent |
| Value | a filesystem path that contains `agent.yaml` |
| Lifetime | one command invocation; never persisted |
| Consumers | `spec.Load`, the `build/<target>/` output path, the header line |

**Derivation rule** (one home, `packageDir` in `internal/cli`):

1. One positional argument present → that argument, verbatim. No existence
   check beyond what `spec.Load` already does, so today's error text survives.
2. No positional argument → `.`, and `agent.yaml` must exist in it. If it does
   not, fail with the guidance message in contracts/cli-surface.md.
3. Two or more arguments → cobra rejects before this runs.

**Invariant**: an explicit argument always wins. The working directory is a
fallback, never an override. This is what keeps the multi-agent parent-folder
workflow (spec User Story 3) working.

**Non-rule, recorded so nobody adds it later**: there is no upward search for a
package. The reasoning is in research.md D3, and the failure it prevents is a
user standing in `build/livekit/` silently recompiling over the directory they
are standing in.

## Entity: Scaffold target default

A package-level constant, already present, whose value changes.

| Attribute | Before | After |
|---|---|---|
| `scaffold.DefaultTarget` (`internal/scaffold/scaffold.go:80`) | `"pipecat"` | `"livekit"` |

It has three production readers: `Data.withDefaults` (`scaffold.go:352-354`),
which calls `SetTarget(DefaultTarget)` when `Data.Target` is empty; the wizard
seed (`tui.go:120`); and the constant itself. Everything downstream of
`SetTarget` is already target-aware:

| Derived value | Set by | LiveKit value |
|---|---|---|
| `TargetVersion` | `SetTarget` (`scaffold.go:326-328`) | `"1.5.2"` |
| `SDKLanguage` | `SetTarget` (`scaffold.go:328`) | `"python"` |
| `Listen` / `Reason` / `Speak` bindings | `SetTarget` (`scaffold.go:330-349`) | shared SLNG + OpenAI starter, unchanged |
| turn block in `agent.yaml` | `templates/agent.yaml.tmpl` branch | `turn.detector` / `turn-detector-mini` |
| `.env.example` keys | target env resolution | adds the three `LIVEKIT_*` names |

**Consequence for implementation**: for the plain `unmute init <name>` path
this is a value change, not a structural one — no new field, no new template,
and the LiveKit branch of every derivation above already exists and was proven
to produce a validating, compiling package (research.md D6).

**Two derivations that do not follow the constant**, and therefore are
structural work:

| Derived value | Where | Today | Needed |
|---|---|---|---|
| Console **create** menu preselect | `tui.go:231-233` via `selectOne` (`tui.go:3223, 3232`) | `options[0]`, positional, so Pipecat | `options[0]` ordered by `DefaultTarget` |
| Console **maintain** menu preselect | `maintain.go:554-563` via the same `selectOne` | `options[0]`, so Pipecat regardless of the package | the package's own `data.Target` (FR-011) |

Neither is derived from the constant today: `selectOne` preselects
`options[0].Value` and never reads `data.Target`. Ordering the create menu by
`DefaultTarget` states the default a second time, so it needs an agreement test
(research.md D12). The maintain menu must **not** follow the constant — the
author already chose, and ordering it by the default would make editing a
Pipecat package highlight LiveKit.

**Not a derivation gap, despite an earlier draft saying so**: the phone-route
`Transport` at `scaffold.go:383` and `tui.go:2170`. There is no LiveKit value
that would make that route resolve, because every registered route carries a
carrier and `SetTarget` clears `Carrier`. The wizard's phone path is gated on
carrier for both targets; only the error text differs (research.md D11,
corrected).

**Coupling to watch**: `agent.yaml`'s turn block and `targets.yaml`'s provider
must move together. A fixture that flips one without the other fails validation
with `turn model "silero" is not recognized`. That is correct behaviour, and it
is the trap that a naive edit falls into.

## State transitions

None. Neither entity has a lifecycle: the resolved directory is computed and
discarded within one command, and the scaffold default is a compile-time
constant.
