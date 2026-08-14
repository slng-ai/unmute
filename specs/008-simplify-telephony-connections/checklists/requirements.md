# Specification Quality Checklist: The connection owns the phone route

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-13
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

- The three open decisions from `/speckit-specify` were resolved with the author
  on 2026-08-13 and are now written as requirements, not markers:
  - **FR-007**: the old shape fails to load, with a message naming the fix. No
    deprecation window.
  - **FR-009**: every route gets a connection file, including the two that need no
    credentials today. FR-009a records the consequence: a connection is legal with
    no `environment` and legal on a package with no phone channel.
  - **FR-006**: `kind:` is removed from the connection file and becomes an unknown
    key.
- Four more were resolved in the `/speckit-clarify` session of 2026-08-13 and are
  recorded under `## Clarifications`:
  - **FR-015 / FR-017**: an orphan connection file warns on stderr and exits 0. It
    is the one check here that is not a failure.
  - **FR-018a**: the browser dev path never requires `channels.web`, so the two
    phone-only examples stay untouched on that axis.
  - **FR-003a**: only the authoring surface changes; the resolved surface keeps
    `transport` and `carrier` on the target, which is what lets the existing
    golden files stand as SC-003's proof.
  - **FR-008 / FR-008a**: `outbound-reminder`'s shared connection splits into
    `twilio_websocket.yaml` and `twilio_connector.yaml`, and no naming convention
    is mandated.
- The spec names the file shape (`targets.yaml`, `connections/*.yaml`, field
  names) and the five example directories, because in this project the authoring
  contract *is* the product surface. It names no Go type, no package, and no code
  path.
- FR-027 is the non-obvious consequence found while clarifying: the existing
  example-agreement check reads transports off **targets**, which after this
  change declare none. Left alone it would pass vacuously and stop protecting
  anything.
- A second `/speckit-clarify` session on 2026-08-13 covered transfers,
  credentials, and dialled numbers, and **grew the scope on purpose**. The feature
  is now an `agent.yaml` change as well as a `targets.yaml` one:
  - **FR-004a to FR-004d**: `destinations` moves from the target to `agent.yaml`
    and narrows to environment-variable names only. The per-target override is
    removed with nothing replacing it — recorded as a removed capability, not as
    tidying, because that is what it is.
  - **FR-005a to FR-005e**: the `secrets:` exemption for telephony names is
    removed. Every author-written name is listed there; driver-supplied route
    variables are not. A missing name warns and exits 0, matching the rule that
    already governs every other env name.
  - **FR-016a, FR-023a, FR-023b, FR-024a**: the transfer refusal names the
    connection and its transport, and `docs/user/learn/07-phone-calls.md` becomes
    the one page that walks route → connection → secrets → destinations →
    transfers → browser-before-phone.
- Two answers went against the recommendation and are recorded as such, with their
  costs written into the spec rather than smoothed over: moving `destinations` to
  `agent.yaml` (costs the per-target override, FR-004b) and requiring telephony
  names in `secrets:` (costs a name appearing in two files, justified in FR-005b).
- A third `/speckit-clarify` session on 2026-08-14 covered documentation of
  required environment variables, prompted by `DAILY_API_KEY`:
  - **FR-005f / FR-005f0 / FR-005f1 / FR-005g**: every name in a telephony
    example's generated `.env.example` must be accounted for in that example's
    README and in `docs/user/learn/07-phone-calls.md`, whether the reader supplies
    it or not. `DAILY_API_KEY` is the case that forces this scoping: exempt from
    `secrets:` under FR-005c because no author writes it, and still required at
    runtime.
  - **FR-027a**: the rule is held by a check that reuses the shape of the existing
    `docs/TRANSFERS.md` one, so a route that grows a required variable fails in CI
    rather than on a live rig.
  - Scope was widened to all examples and then narrowed back to the five telephony
    ones in the same session, once it emerged that four non-telephony examples
    ship no README at all. FR-005f0 records the narrowing and the passing baseline
    for widening it later.
- FR-005f was rewritten later on 2026-08-14. Its first form said a variable the
  reader never sets should be documented as one they do not set; the author
  reversed that. The rule is now an equality with `.env.example` — nothing
  missing, nothing extra — with only the "nothing missing" half checked (FR-027a),
  because a check strict enough to catch the other half would fail deliberate
  teaching lines. FR-005h names the one paragraph this deletes today.
