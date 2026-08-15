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

**On clarification markers.** Three decisions could reasonably have been asked
about at drafting time. Two were resolved with a recorded reason in the
Assumptions section rather than blocking; the third was settled by the author in
the 2026-08-15 session recorded in the spec's Clarifications section.

1. **Severity of the unattached-control refusal** — error, because no finished
   package has a legitimate reason to declare a control nothing reaches. Falls
   back to a warning with identical wording if a legitimate case appears.
2. **The single model identifier** — **settled by the author**: `gpt-5.6-luna`,
   which supersedes the drafted `gpt-4.1-mini`. Two follow-on questions were
   asked because the new choice is a reasoning-family model rather than a
   straight swap, and both were answered: `reasoning_effort: minimal` via the
   existing `params:` pass-through, and `temperature` removed because OpenAI
   does not state that the model accepts it.
3. **The starting example** — `salon-support`, because the table already marks
   it "Start here" and it needs no third-party account.

**Three of the brief's premises were corrected by reproduction**, and the spec
follows the evidence:

- the Pipecat container break is a regression from 2026-08-13, not a permanent
  gap;
- the `UNMUTE_*` beginner path is already clean, and the real contradiction is
  in the generated files;
- the model identifier is not "scaffold versus docs". The split is front doors
  versus examples, and the doc site disagrees with itself. The author's choice
  of `gpt-5.6-luna` retires both incumbents, so the census now sizes the sweep
  rather than deciding it.

**One drafted decision was reversed by the author and the spec says so.**
Research D11 first concluded "no `UNMUTE_*` renames", on the argument that
renames are cosmetic once presentation is fixed. That held for the names the
generated agent owns and failed for the five a vendor owns:
`UNMUTE_DAILY_ROOM_GEO` configures a Daily room and claims two owners in one
name, and the three `UNMUTE_LIVEKIT_*` mappings exist only because a LiveKit
container runs. The stronger argument the first pass missed is Principle I: a
generated project must run with Unmute absent, so an `UNMUTE_` prefix inside it
is dependency-shaped regardless of whether anyone mistakes it for a secret. The
superseded reasoning is kept in D11 rather than deleted.

**One consequence of the strictest visibility choice is recorded, not hidden.**
Removing every non-author name from the emitted files removes the one document
that told a self-hosted operator what to supply. That information survives in
`compile-report.json`'s `required_env` and in the Compose file's interpolation
defaults, and FR-018b holds it there with a test. The author's own rule carries
the exemption that keeps a genuine developer note, such as what to do when host
port 5060 is taken, which FR-018c places in a troubleshooting section rather
than a to-do list.

**One new risk was opened by the model choice and is tracked rather than
assumed away.** `gpt-5.6-luna` is a reasoning-family model, so latency before
each spoken turn is a real failure mode for a voice agent, and OpenAI's own
reference says parameter support varies on newer reasoning models without
saying which parameters this one rejects. Both are in Edge Cases, both have a
recorded fallback, and both are judged by ear under SC-003 rather than by
assumption.

**Two success criteria were made honest rather than aspirational.** SC-006 is
recorded as already holding at baseline, so the deliverable there is the test,
not a cleanup; the measurable change moved to SC-006a. SC-005 is stated as a
re-measurement of a bar PR #80 missed at 7 of 10, with the instruction that a 7
is reported as a 7.
