# Specification Quality Checklist: Pipecat Cloud telephony on the Daily route

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-12
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

All items pass. Ready for `/speckit-plan`.

### How the three open questions closed

The requester answered all three at once: "the only way this is supported for now is
either Pipecat Cloud or LiveKit Cloud. The only thing that can change is Pipecat
support for SIP, that now will be only Daily."

- **Hosting model field**: not added. One hosting model per driver means there is
  nothing for an author to select (FR-001).
- **Daily phone channel**: not added. The compiler derives the facts instead
  (FR-002). This also avoids dragging `capacity` in, which `docs/SCHEMA.md` §4.10
  requires whenever `channels` carries a telephony entry.
- **Self-hosted carrier websocket routes**: left in place, neither promoted nor
  deleted, recorded as an assumption rather than decided here.

### Two things the rewrite surfaced

1. **The decision reverses a recorded stance.** `docs/DEPLOYMENT.md` carries
   "Adopted stance, July 23, 2026 ... deployments do not use LiveKit Cloud or
   Pipecat Cloud", and `docs/TELEPHONY.md` promises deployment "without requiring
   Pipecat Cloud or LiveKit Cloud". The repository rule is that the document wins
   until changed, so correcting both is in scope (FR-016, FR-017), and SC-007 makes
   the absence of a stale stance measurable. The constitution says nothing about
   hosting, so no amendment is needed there.
2. **Two of the five linked documents dropped out of scope.** Regional websocket
   endpoints and websocket authentication are properties of the carrier websocket
   routes, and Daily uses neither. This is called out explicitly in Out of Scope
   rather than left as a silent omission, since the requester supplied both links.

### Iteration 3: post-analyze corrections (2026-08-12)

`/speckit-analyze` found 15 findings across the three artifacts, 2 CRITICAL. All were fixed
in the same session. The two that mattered:

- **A caller-identity field would have breached the compliance review.** User Story 5 let
  the author choose the identity an outbound recipient sees. No such field exists anywhere
  in the schema, so it was a new authoring surface, and the constitution requires one to
  land with a numbered SCHEMA amendment, derived schemas, a capability row, agreement
  tests, scaffold templates, the interactive console, examples, and `docs/user/` in the
  same change. Three tasks covered none of it. Caller identity is now Out of Scope as its
  own feature, and FR-003 states the full checklist so the next attempt cannot under-scope it.
- **The Daily transfer had no idempotency guard and could not use the usual one.** FR-008
  requires at most one attempt per call. `bot.py.tmpl:298` had no guard, and the shared
  control store the carrier routes use is forbidden on this route by `contracts/artifacts.md`
  and by the constitution's rule against an idle service. This was a requirement with zero
  coverage sitting on unimplemented behaviour, on a caller-facing path. Now an in-process
  guard, with tasks, a contract entry, a data-model entity, and a live double-request drill.

The rest were my own drafting drift, mostly from rewriting the spec after the "say nothing"
decision while the plan and contracts kept the older shape:

- `plan.md` cited a 2-second caller-facing budget "in the spec" that the rewrite had dropped.
  Restored as SC-011 rather than deleted, because it is a real budget.
- FR-001 and FR-002 promised an enforcing test in two documents with no requirement and no
  task behind it. Now FR-030 with task T007.
- FR-004 backward compatibility had zero tasks while `contracts/authoring.md` asserted the
  coverage existed. Now FR-031 with task T052.
- The `--telephony` refusal and the four route-shape invariants existed in the plan, the
  contracts, and the tasks but in no requirement. Now FR-028 and FR-027.
- FR-011 and FR-007 had no tasks. Now T033 and T015.
- `FR-017a` renumbered away; identifiers are sequential again.
- One pre-existing constitution violation recorded rather than silently worked around:
  `internal/generate/examples_test.go` enumerates directories on disk instead of asking
  `git`, which is what turned the suite red earlier in this work.

### Iteration 4: clarification session (2026-08-12)

Three questions asked and answered. All were high impact and all three changed the spec.

The trigger was a correction from the requester: **warm transfer is available on Daily.** They
were right. `docs.pipecat.ai/pipecat/telephony/daily-pstn` states "Daily supports two primary
transfer patterns", cold and warm. Both this spec and `docs/TRANSFERS.md` asserted that Pipecat
has no warm primitive on any route, which is false.

Worth separating two things the repository had merged into one claim. The base branch's
**engineering judgement** was sound and is confirmed by the documentation: warm on Daily needs
`TransferCoordinator`, `SoundfileMixer`, `MixerEnableFrame`, `CustomerHoldGate`, and
`BotAudioGate`, all application code, so the generated bot owns audio control. Its **factual
claim** was wrong. A decision not to build something is not the same as a platform limitation,
and writing it as the latter is a defect under Principle IV.

Resolutions:

1. Warm is admitted as a **Daily-only dated exception** to the audio-path rule. The rule still
   holds on the carrier websocket routes, where it was bought with real live-test failures.
   The distinction that makes this safe: Daily's room is the bridge, and the processors gate
   inside a pipeline we already own, rather than bridging two carrier sockets.
2. **N30's scope is amended** so its `outbound: true` requirement applies only to routes that
   have a phone channel. Daily has none, so the compiler derives the need and names the
   dial-out prerequisite. FR-002 stands and `capacity` stays out.
3. Warm becomes **feature 005**, not part of this one. It cannot be tested until a Daily call
   connects at all, which is what this feature fixes.

Added here: FR-032 and SC-013 (no document may claim a platform lacks a capability it
documents), tasks T057a and T057b. Emitting warm remains out of scope; only the false claim is
corrected. Also confirmed in passing: the `warm:` authoring block already exists and is complete
(`destination`, `briefing`, `ring_timeout`, `on_unavailable`), so feature 005 needs no new
authoring field either.

### Iteration history

Iteration 1 (before the requester's answer): three `[NEEDS CLARIFICATION]` markers
on the authoring surface, plus a hosting-model user story and six requirements built
on the assumption that two hosting models had to coexist.

Iteration 2 (after the answer): the hosting-model story and its requirements were
deleted rather than reworked, since the premise was gone. Requirements FR-001 and
FR-002 now state what must *not* be added, so a later reader does not re-propose it.
Fixed at the same time: implementation details (platform hostnames, config keys,
framework parameter class names) were moved out of the requirements into the
"Where We Stand Today" evidence table, and backward compatibility was added
(FR-004, SC-008), which the first draft omitted entirely.
