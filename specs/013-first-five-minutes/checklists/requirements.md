# Specification Quality Checklist: Make the first five minutes work, and stop lying quietly

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-15
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

**On "no implementation details".** This is a defect-fixing feature, so the
functional requirements name specific files, functions, and line numbers. That
is deliberate and it is evidence, not design: every one comes from
[reproduction.md](../reproduction.md), where an isolated agent observed it
running. A requirement that says "the check at `internal/ir/validate.go:1388`
guards on the declared list" is stating what is broken, not prescribing how to
fix it. The plan chooses the fix.

**On zero clarification markers.** Three decisions could reasonably have been
asked about. Each was resolved with a recorded reason in the Assumptions
section rather than blocking:

1. **Severity of the unattached-control refusal** — error, because no finished
   package has a legitimate reason to declare a control nothing reaches. Falls
   back to a warning with identical wording if a legitimate case appears.
2. **The single model identifier** — `gpt-4.1-mini`, because every front door
   already says it, against a raw count that favours the other. The count is
   recorded so the decision can be reversed cheaply.
3. **The starting example** — `salon-support`, because the table already marks
   it "Start here" and it needs no third-party account.

**Three of the brief's premises were corrected by reproduction**, and the spec
follows the evidence:

- the Pipecat container break is a regression from 2026-08-13, not a permanent
  gap;
- the `UNMUTE_*` beginner path is already clean, and the real contradiction is
  in the generated files;
- the model identifier is not "scaffold versus docs" — the split is front doors
  versus examples, and the doc site disagrees with itself.

**Two success criteria were made honest rather than aspirational.** SC-006 is
recorded as already holding at baseline, so the deliverable there is the test,
not a cleanup; the measurable change moved to SC-006a. SC-005 is stated as a
re-measurement of a bar PR #80 missed at 7 of 10, with the instruction that a 7
is reported as a 7.
