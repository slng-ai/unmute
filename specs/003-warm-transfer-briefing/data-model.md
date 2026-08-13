# Data Model: Brief the manager, then hand the call over

## Authoring surface

**No change.** No field is added, removed, renamed or reinterpreted. `internal/spec`,
`internal/ir` and `internal/target` are untouched, so the derived schemas, the capability
table and the agreement tests move by zero bytes. FR-016 is therefore satisfied by
construction, and one test pins it rather than one paragraph claiming it.

The reason it is worth saying out loud: the reported defect is about what a generated
agent **says**, and a defect in what a generated agent says should be fixable in the
template that says it. If this feature had needed a new field, the design would have been
wrong.

## Template model fields this feature reads

All of these exist today. The driver builds them in `internal/generate/livekit_v1_build.go`
and `agent.py.tmpl` already reads every one.

| Field | Scope | What it carries | Used for |
|---|---|---|---|
| `HasWarmTransfer` | package | any control in the package is a warm transfer | whether the persona constant is emitted at all |
| `HasColdTransfer` | package | any control is a cold transfer | the standard-library timing import, shared with warm |
| `Telephony` | package | the resolved route, carrier and connection | the inline trunk helpers the warm dial already uses |
| `Method` | per control | the emitted tool's name | the line that records which control fired |
| `Warm` | per control | warm or cold | which branch of the template runs |
| `Briefing` | per control | the author's briefing text, optional | the extra section of the manager-facing instructions |
| `RingTimeout` | per control | seconds to wait for an answer, optional | unchanged, passed straight through |
| `Hangup` | per control | whether the unavailable behaviour ends the call | unchanged, chooses the failure branch |
| `DialExpr` | per control | the expression for the number to dial | unchanged |
| `ToExpr` | per control | the cold destination expression | unchanged |

## Emitted constructs this feature adds

Everything below appears in `build/<target>/agent.py` and nowhere else.

| Construct | Where | Emitted when | Notes |
|---|---|---|---|
| `_BRIEFING_PERSONA` | module level, beside the other transfer helpers | the package has at least one warm transfer | One home for the text, so several warm transfers in one package cannot drift apart. Its contract is [contracts/emitted-briefing.md](./contracts/emitted-briefing.md). |
| `WorkflowInstructions` on the existing workflows import | import block | the package has at least one warm transfer | Same module the task already comes from, so this is one more name on a line that exists. The name is version sensitive: 1.6.9 renamed it from `InstructionParts` with no alias, see [research R1](./research.md#r1-the-version-gap). |
| `import time` | import block | the package has at least one transfer of either kind | Standard library. Carries the elapsed seconds on every outcome line. |
| `briefing_ctx` local | inside each warm transfer tool | per warm transfer | The conversation is read **once** into a local, so the number logged and the number handed over cannot differ. |
| `started` local | inside each transfer tool | per transfer | Monotonic, so a clock change cannot produce a negative duration in a log. |

## States a transfer moves through

The observable states, and which of them the emitted code can see. R4 explains why the
answer is not one of them.

```text
                    ┌──────────────┐
                    │ control fired│  logged
                    └──────┬───────┘
                           v
                  ┌────────────────┐
                  │ dial requested │  logged, with the handed-over message count
                  └───┬────────┬───┘
          not visible │        │ not visible
                      v        v
              ┌──────────┐  ┌──────────────┐
              │ answered │  │ no answer    │
              └────┬─────┘  └──────┬───────┘
     ┌─────────────┼─────────────┐ │
     v             v             v │
┌─────────┐  ┌──────────┐  ┌───────────┐
│ accepted│  │ declined │  │ undecided │
└────┬────┘  └────┬─────┘  └─────┬─────┘
     │            │              │ persona routes this to declined
     v            v              v
┌─────────┐  ┌──────────────────────────┐
│ merged  │  │ unavailable              │  logged, with the reason and the duration
└─────────┘  └──────────────────────────┘
```

`accepted`, `declined` and `undecided` are all decisions of the manager-facing model.
`undecided` is the state with no platform exit, which is why the persona pushes it into
`declined`, and why the plan records that as a mitigation rather than a bound.

## What the log record contains

One transfer produces one ordered set of lines. The full contract, including the exact
level and everything that may never appear, is
[contracts/transfer-log.md](./contracts/transfer-log.md). The two facts that belong here:

- The **count** of conversation messages handed to the briefing is recorded. The messages
  themselves are not: they are the caller's words, and an operator log is a new place for
  them to land with no diagnostic gain the count does not already give.
- **No destination value** is recorded, in either transfer kind. The control's own name
  already identifies which destination fired, so the value would add nothing and would put
  a phone number in a log for the sake of it.
