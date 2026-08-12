# Implementation Plan: Dial out with the carrier's own SIP credentials

**Branch**: `feature/warm-cold-human-transfer` (feature dir `002-inline-sip-trunk`) | **Date**: 2026-08-12 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/002-inline-sip-trunk/spec.md`

## Summary

A generated LiveKit package cannot dial anybody until somebody runs
`lk sip outbound create` and pastes the returned id into `.env`. On 2026-08-12 a
live deploy proved it: the agent registered, held a conversation, fired the
transfer, and raised
``ValueError: `LIVEKIT_SIP_OUTBOUND_TRUNK` environment variable, `sip_trunk_id`, or `sip_connection` must be set``
from inside the prebuilt's constructor. The caller heard that the manager was
not available.

The platform's own message names the fix. This feature takes the third option:
pass the trunk's hostname and credentials **inline** with the call, using the
four values a Connection already declares. After it, the stored outbound trunk
is gone from the generated project entirely, not demoted to a fallback, so there
is one dial-out shape rather than two.

Three changes carry it. **The three dial-out sites** in `agent.py.tmpl` gain
`api.SIPOutboundConfig(...)` plus a from-number, which the inline form requires.
**The stored trunk leaves the repository**: its environment name drops out of the
capability table, the emitter stops writing `sip-outbound-trunk.json`, the
generated README loses the two commands that created it, and the local
development path stops registering one so local and deployed dial the same way.
**The four SIP values get carrier-neutral names**, `SIP_TRUNK_HOSTNAME`,
`SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD` and `SIP_FROM_NUMBER`, in the shipped
example, the scaffold and the documents, with the compiler staying entirely
ignorant of them.

Planning found two things the spec did not know. Passing `sip_connection`
explicitly makes the prebuilt ignore `LIVEKIT_SIP_OUTBOUND_TRUNK` by upstream
design, so the leftover-name requirement needs a test and no code. And
`from livekit import api` is emitted only for an outbound channel or a cold
transfer, so a **warm-only** package would meet `api.SIPOutboundConfig` with no
`api` in scope. That second one is the `httpx` blind spot again: a shape no
golden covers. Both are in [research.md](./research.md).

Cold transfer does not change. Inbound does not change. The Pipecat driver and
the connector route do not change.

## Technical Context

**Language/Version**: Go 1.24, pinned in `go.mod`, `CGO_ENABLED=0`. The emitted
artifact is a Python 3.12 project produced by `text/template`.

**Primary Dependencies**: `spf13/cobra`, `goccy/go-yaml v1.19.2`,
`google/jsonschema-go v0.4.3`, Charm stack for the TUI. **No new dependency**,
and none is needed: the change is a different argument to a call the emitted
project already makes.

**Verified upstream surface**: `livekit-agents 1.6.9`, read from the installed
package inside `unmute-lk-fixed:latest` on 2026-08-12, not from documentation
alone. `api.SIPOutboundConfig` exists on `livekit.api` and on
`livekit.protocol.sip`, and is the form the prebuilt's own signature uses.

**Storage**: N/A. `build/<target>/` stays disposable, with the two existing
preserved patterns (`.env`, `livekit*.toml`) unchanged.

**Testing**: `go test ./...` for L1 unit, L2 in-process command, L3 golden.
`make smoke` (build tag `smoke`, needs `uv`) proves the emitted Python parses
and imports. The end-to-end proof is a live warm transfer in the Agent Console,
which is manual and is what this unblocks rather than something the suite runs.

**Target Platform**: developer machines producing artifacts for LiveKit Cloud and
for self-hosted LiveKit.

**Project Type**: compiler CLI. `spec.Load` then `ir.Build` then `ir.Validate`
then `generate.Generate`.

**Performance Goals**: N/A. Nothing here is on a hot path. One note that is
adjacent: inline configuration sends trunk settings with each dial rather than
naming a cached object, and the documentation does not say whether that
participates in the platform's trunk caching. Nothing in this plan claims it
does or does not.

**Constraints**: compile stays offline and credential-free; Go templates only,
no post-generation string surgery; no secret value in any emitted file; the
authoring surface must not break a package already written; goldens are read,
never regenerated blind; every platform claim carries its source and the
2026-08-12 date.

**Scale/Scope**: 4 Go files, 2 templates, 1 golden, 1 shipped example
(3 files), 6 documents, and 1 docs-sync test. `LIVEKIT_SIP_OUTBOUND_TRUNK`
appears in 18 non-spec files and must reach zero on the dial-out path. No
NEEDS CLARIFICATION remain: five were asked and answered in the spec's
[Clarifications](./spec.md#clarifications) session, and the one item the
requirements checklist had flagged for implementation time (whether the
from-number is genuinely required) is settled in [research.md](./research.md#r3-the-from-number-is-not-optional-with-inline-configuration).

## Constitution Check

*GATE: passes. Checked against `.specify/memory/constitution.md` v2.0.0.*

| Principle | How this plan satisfies it |
|---|---|
| **I. Compile ahead of time** | Nothing is called at compile time and no runtime layer appears. The four values are lowered into `os.environ[...]` reads at generate time, so the deployed agent talks to the platform directly with Unmute absent. Python stays template-only. The change **removes** state that had to exist outside the artifact, which is the principle pointing the same way as the feature. |
| **II. Fail loud, never average** | No feature is dropped and nothing degrades. Two things fail loud where they used to pass: a Connection missing a SIP value already failed through the route's required-environment rule, and a warm transfer on a target with **no** Connection now fails too, where it used to validate green and read a trunk id the package never mentioned. A warm transfer that cannot dial still fails in the platform's own words. The one silent-failure risk this plan introduces is an empty from-number, which is why FR-003 makes the number explicit at every dial site instead of leaning on a fallback that ends at `""`. |
| **III. One source of truth** | The Connection stays the single home for the four values, and this feature is what makes them reach the dial-out path instead of being re-supplied in a platform-minted shape. The three dial-out sites must not each grow their own copy of the config literal, so they share one template fragment; that is what makes FR-002 structural rather than a promise. `internal/target/telephony.go` stays the only place that says which environment names a route requires. The compiler learns none of the four names, which is what keeps the same emitted code working for any carrier. |
| **IV. The document wins** | Every claim carries its page and the 2026-08-12 date. `docs/SCHEMA.md` gains amendment **N33**, not for an authoring-surface change, because there is none, but because the emitted required-environment list, the dial-out mechanism and the shipped example's environment names all move. N33 also retires one sentence of **N28**, which explained the connector route by saying `WarmTransferTask` "acts on a SIP participant reached through an outbound trunk": the participant is still needed, the stored trunk is not. N28's conclusion needs nothing, because **N31 already supersedes it** and agrees with the capability table, which the plan originally got wrong (see the withdrawn finding below). `docs/TRANSFERS.md` section 4 and the generated README stop presenting the trunk step as a prerequisite. |
| **V. Whatever compiles can be spoken to** | This is the whole point: a fresh compile becomes dialable with only the credentials the operator already holds. `validate` reach and `compile` reach are both unchanged. The local development rule that "what you test MUST be what you ship" is what forces the dev path to dial inline as well, rather than keeping its stored trunk. |
| **Derived, never declared** | No new authoring field, and no invented value. The from-number comes from the Connection. Destination country and transport are deliberately not emitted, with the reason recorded, rather than defaulted to a literal somebody would have to discover. |
| **Secrets** | The credentials continue to be referenced by `UPPER_SNAKE` name and read through `os.environ`. No secret value enters any emitted file, including the ones being deleted. Worth noting: deleting `sip-outbound-trunk.json` **removes** a file that carried the host and number as `${...}` placeholders, so the emitted surface that mentions credentials at all gets smaller. |
| **Telephony boundary** | Unmute still provisions nothing at the carrier. It now provisions one thing fewer on the platform side too. Inbound trunk and dispatch rule stay exactly as they are, because an unsolicited call cannot reach a project or a room without them. |
| **Gate** | `make fmt`, `make lint`, `make build`, `make test` must pass; `make smoke` stays opt in. The one affected golden is regenerated with `-update` and the diff is read before committing. |

**No violations to justify.** Complexity Tracking below records two deliberate
simplifications instead, each with its ceiling named.

**Re-checked after Phase 1 design: still passes.** The design added one thing
worth re-examining against Principle III, the shared template fragment for the
inline config literal, and it holds: it exists precisely so the same fact is not
written three times, which is the principle rather than an exception to it. It
also added one thing worth re-examining against Principle II, the decision to
write no code for the leftover trunk variable, and that holds too: the behaviour
is upstream's, verified in its source, and the plan pins it with a test rather
than trusting it.

## Project Structure

### Documentation (this feature)

```text
specs/002-inline-sip-trunk/
├── plan.md                      # This file
├── research.md                  # Phase 0: verified upstream facts and the two planning findings
├── data-model.md                # Phase 1: the entities this feature moves
├── quickstart.md                # Phase 1: how to prove it, offline then on a real call
├── contracts/
│   ├── emitted-dial-out.md      # What the generated dial-out code must look like
│   └── environment.md           # The environment-name contract and the migration table
├── checklists/
│   └── requirements.md          # Spec quality checklist, 16/16
└── tasks.md                     # Phase 2 output (/speckit-tasks, not created here)
```

### Source Code (repository root)

Only the files this feature touches, with what changes in each.

```text
internal/
├── target/
│   ├── telephony.go             # livekit/sip route: LIVEKIT_SIP_OUTBOUND_TRUNK leaves
│   │                            #   RuntimeEnvironment and DevSuppliedEnvironment;
│   │                            #   ManualSteps drop the outbound trunk and its id copy
│   └── user_docs_test.go        # docs sync: the three carrier SIP names become one
├── ir/
│   ├── validate.go              # (assert only) warm transfer already needs a resolved route
│   └── build_test.go            # fixture keeps carrier-prefixed names on purpose (SC-010)
├── generate/
│   ├── livekit_v1.go            # livekitSIPFiles: the sip-outbound-trunk.json block goes
│   ├── livekit_v1_build.go      # three env.add("LIVEKIT_SIP_OUTBOUND_TRUNK") sites go;
│   │                            #   livekitTelephony already carries the four env names
│   ├── livekit_v1_test.go       # inline assertions at all three dial sites
│   ├── livekit_deploy_test.go   # new: warm-only fixture, leftover-name fixture
│   ├── templates/livekit_v1/
│   │   ├── agent.py.tmpl        # shared inline-config fragment; three call sites;
│   │   │                        #   the `from livekit import api` condition widened
│   │   └── README.md.tmpl       # outbound trunk steps removed; migration note added
│   └── testdata/golden/
│       └── livekit_v1_telephony_compose.yaml   # regenerate, read the diff
└── cli/
    ├── dev_livekit_sip.go       # the needs("LIVEKIT_SIP_OUTBOUND_TRUNK") block is dead, delete it
    └── dev_livekit_sip_test.go  # expectations follow

examples/human-transfer/
├── connections/twilio_sip.yaml  # the four names become carrier-neutral
├── targets.yaml                 # header comment stops naming the outbound trunk
└── README.md                    # setup drops the trunk step; migration table added

docs/
├── SCHEMA.md                    # amendment N33: restate N28's reasoning
├── TRANSFERS.md                 # section 4: the trunk is no longer a prerequisite
├── TELEPHONY.md                 # credentials section: renamed, trunk step removed
└── user/
    ├── learn/07-phone-calls.md  # LiveKit SIP walkthrough
    └── targets/livekit.md       # target page

README.md                        # repository-level mention of the SIP names
```

**Structure Decision**: no new package, no new file in `internal/`, and no new
directory. The change is subtraction in three of the four Go files and one
addition in the fourth (`agent.py.tmpl`). This matches the existing layout,
where `internal/target` owns what a route requires, `internal/generate` owns how
a driver lowers it, and `internal/cli` owns the local run. The two new test files
listed are additions to an existing test package, not new structure.

## Complexity Tracking

> No Constitution violations to justify. These are the two deliberate
> simplifications this plan makes, recorded with their ceilings as the
> constitution requires of a shortcut.

| Simplification | Ceiling it accepts | Upgrade path |
|---|---|---|
| **No stored-trunk override in emitted code.** One dial-out shape, no branch. | A package that needs a caller-ID number pool, trunk metadata on every participant, or credential changes without redeploying cannot have them from a generated project. | The platform feature still exists and the prebuilt still honours an explicit trunk id ahead of an inline connection, so restoring it is one template argument plus one capability row. It would need a new spec, because it reintroduces the two-shapes problem this feature removed. |
| **No destination country and no transport in the emitted inline config.** | Outbound region pinning by destination country is unreachable from a generated package, and a carrier that must be pinned to TCP or TLS is unsupported. | Region already follows `deployment_region` for origination, which covers the common case. A real compliance requirement reopens it, and the cost is one optional Connection key plus narrowing FR-005, both named in the spec. |

**One thing found while planning that belongs to another change**, recorded here
rather than lost: the `load_fnc` / `load_threshold` "not supported when hosting on
Cloud" warnings and the deprecated `metrics_collected` event that the same live
deploy printed are **ours**, from `agent.py.tmpl:706-710` and `:758`. They belong
with feature 001's live-deploy fixes.

**One finding withdrawn during implementation.** The plan reported that
`docs/SCHEMA.md` N28 claimed a connector-route transfer the capability table
denies, and pulled a rejection amendment into scope as FR-019. The premise was
wrong: **N31 supersedes N28**, says so in its first sentence, agrees with the
table, and records that those transfers were built, live-tested and deleted after
each design made the generated process own the call's audio path. FR-019 and
SC-013 are withdrawn in the spec. What N33 still owes N28 is one sentence about
the outbound trunk, which FR-011 and FR-014 already cover. Detail in
[research.md R8](./research.md#r8-a-finding-i-got-wrong-and-the-document-that-already-had-it-right).

**One requirement tightened during implementation, which the plan did not
foresee.** R7 assumed validation already rejected a warm transfer on a target
with no telephony Connection. It did not: it validated green and emitted an agent
that read `LIVEKIT_SIP_OUTBOUND_TRUNK`, a trunk id the package never mentioned.
With inline dialling there is nothing to dial with in that shape, so it is now a
gated error naming the four values it needs (FR-006). No shipped package or
example has that shape; two test fixtures constructed it in memory and now use a
telephony-backed target instead.

One consequence worth stating in the plan rather than discovering later: with no
laptop path to a transfer, the acceptance test for this feature is a manual live
call. That is why the offline layer has to carry the shapes a live call will never
reach, the warm-only package above being the one that matters.
