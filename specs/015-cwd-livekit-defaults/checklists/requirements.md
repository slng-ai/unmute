# Specification Quality Checklist: Work Inside The Agent Folder, LiveKit By Default

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-16
**Revised**: 2026-08-16 after cross-artifact analysis
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

- Command names (`unmute validate`, `agent.yaml`, `targets.yaml`) appear
  because the CLI surface *is* the user-facing product here. Internal package
  names and code structure are absent.

## Revision log

The first draft passed this checklist and was still wrong in four places. All
four were found by cross-artifact analysis and are fixed:

1. **US2 acceptance scenario 3 was false.** It promised that choosing Pipecat
   "by editing `targets.yaml`" worked. The project's own research proved it
   fails with `turn model "silero" is not recognized`, because the turn block
   in `agent.yaml` must move too. Scenario 3 is narrowed and scenario 4 now
   states the real behaviour and requires it be documented.
2. **FR-008 was unsatisfiable as written.** It demanded the in-folder form on
   "every surface", including example READMEs that correctly run from the
   repository root. It now turns on where the reader is standing.
3. **Three requirements were missing.** FR-009 (no surface pins a `--target`
   a scaffolded package lacks), FR-010 (the wizard's telephony guidance must
   not degrade), and FR-011 (create preselects the default, maintain preselects
   the package's own target). Work was already planned for all three with no
   requirement authorizing it.
4. **An assumption was too narrow.** "The interactive console's initial
   selection" was read as covering both console flows, which led to a planned
   change that would have made editing a Pipecat package highlight LiveKit.

Lesson worth keeping: this checklist verifies that a spec is well-formed, not
that it is true. Items 1 and 4 were internally consistent and passed every box
above. Only checking the spec against the code caught them.
