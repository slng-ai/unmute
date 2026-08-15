# Specification Quality Checklist: Complexity Cleanup

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

## Validation Notes

**Iteration 1 findings and fixes**

- *No implementation details*: the first draft named Go functions, file paths,
  and standard-library calls directly. Rewritten so requirements describe the
  behavior being deduplicated ("the two terminal-detection helpers in the
  command package") rather than naming symbols. The spec stays precise enough to
  be testable without prescribing the edit.
- *Technology-agnostic success criteria*: SC-003 originally named a language.
  Reworded to a line-count reduction against a stated baseline, which is
  measurable without knowing the implementation.
- *Scope bounded*: three audit findings conflicted with the constitution. Rather
  than dropping them silently, an Out Of Scope section records each with the
  rule that protects it, so the same proposal is not re-made later. Two further
  items are marked deferred with the reason.

**Constitution alignment**

- Principle III (one source of truth per surface) is the basis for User Story 2.
- Governance ("complexity must be justified") is the basis for User Story 4.
- Governance ("deliberate simplifications are marked in code") is FR-027.
- The gate ordering in Development Workflow is FR-003.
- Targets And Providers, Principle III, and Principle IV each rule out one of
  the three refused findings.

**Known risk carried forward to planning**

User Story 5 touches the interactive console, whose behavior is pinned less
tightly than generated output. Its acceptance scenarios lean on the accessible
renderer, which the test layer can drive with no terminal attached. If planning
finds that coverage insufficient, dropping User Story 5 still satisfies SC-003.

**Iteration 2 — after clarification session 2026-08-15**

Four questions asked and answered; all sixteen items still pass. The spec grew a
live-verification surface it did not have before, which closed a real gap: the
original acceptance criteria rested entirely on golden files and the test suite,
and neither exercises the tool's own runtime paths.

Re-checked against the new material:

- *No implementation details*: the new requirements name behaviors ("the session
  establishes and the agent speaks its greeting") rather than commands or code.
  `FIRECRAWL_MCP_URL` and the root `.env` are named because they are concrete
  preconditions a reader must act on, not implementation choices.
- *Measurable success criteria*: SC-008 through SC-011 are counts and states —
  thirteen sessions, sixty-five green, zero skipped, zero secret values in any
  artifact.
- *Edge cases identified*: four new ones, covering the first-user-turn gap the
  greeting criterion accepts, stale credentials, Docker being unavailable, and
  the rule that no artifact may carry a secret value.
- *Scope bounded*: the four carrier examples are explicitly compile-only, with
  the governance rule that puts them there.
- *Assumptions identified*: four added, including the one that makes the cadence
  affordable (no person at a microphone) and the note that the credentials for a
  future live telephony sweep already exist.

**Open precondition, not a spec defect**

FR-038 records that `FIRECRAWL_MCP_URL` is declared by the MCP example and is
absent from the root `.env`. Under FR-035 that fails two of the thirteen
sessions until it is supplied. This is tracked as work to do before the first
sweep, not as an unresolved requirement.

**Accepted coverage gap**

The greeting-only pass criterion does not exercise a first user turn, and the
browser-only mode does not exercise the SIP admin token, the dispatch token, or
the Twirp client. Both gaps are deliberate and both are covered elsewhere:
byte-identical generated output for the former, and existing unit tests that
recompute the HS256 signature against the secret for the latter.

## Notes

- Items marked incomplete require spec updates before `/speckit-clarify` or `/speckit-plan`
- All 16 items pass as of 2026-08-15, before and after the clarification session
- The spec is ready for `/speckit-plan`
