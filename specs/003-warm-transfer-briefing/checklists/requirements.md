# Specification Quality Checklist: Brief the manager, then hand the call over

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

Two items pass with a deliberate deviation on the record, the same one features 001
and 002 took.

**Platform facts are in the spec on purpose.** Constitution principle IV says the
document wins and that every platform claim carries its source and its verification
date. A specification that describes the behaviour to be fixed without naming where
that behaviour was read would leave the next reader unable to check it, which is the
failure the principle exists to prevent. So the contract table cites source files and
documentation pages. It is evidence about the world the feature lands in, not a design
for the feature. The requirements themselves name no function, module or API.

**Two success criteria name a platform artefact.** SC-002 names a deprecated
parameter and SC-003 names the log command an operator runs. Both are the only
honest way to state the outcome: "stop using the parameter the platform deprecated"
and "a reader of the deployment's own logs can tell what happened". Replacing either
with a technology-free paraphrase would make it unverifiable.

**No blocking ambiguity.** Zero clarification markers. The one decision left open at
specification time, whether the platform's prebuilt task stays the mechanism, was
settled in [plan D1](../plan.md#d1-keep-the-platforms-prebuilt): it stays, because the
manual alternative would put a second room, a second session and every lifecycle edge
inside every generated project, which `docs/SCHEMA.md` N31 records this repository
already trying and deleting. FR-001 stays written as an outcome, which is what let the
plan choose.

**Revalidated 2026-08-12 after `/speckit-analyze`.** Fifteen findings, two of them the
same decision: the spec asked for a hard bound on the consultation that the platform
cannot give without leaving the caller muted with hold music playing. FR-010 was
rewritten to a best-effort exit, FR-011 withdrawn with its reasoning kept, FR-012's
constant half withdrawn, and SC-005 and SC-006 restated. The other findings were
staleness after the version check closed, one unit drift (turns versus messages), two
coverage gaps that became tasks T031 and T032, and wording. Every checkbox above was
re-evaluated against the rewritten spec and none regressed.
