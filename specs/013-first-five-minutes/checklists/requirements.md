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

## Cross-artifact analysis, 2026-08-15

A `/speckit-analyze` pass over spec, plan, and tasks found twelve issues, zero
of them constitutional violations, and all twelve were remediated. The two that
mattered:

**A real design contradiction between two accepted clarifications.** FR-018
hides names from `build/<target>/.env.example` while FR-005b derives
`REQUIRED_ENV` from the names the runtime requires, which includes them. An
operator copying the env file and starting the container without `unmute dev`
would get a refusal naming a variable the file never mentioned. Pipecat made it
sharpest, because there the agent genuinely reads `REDIS_URL` and raises on it.
Resolved by research D14 and FR-005c: the check keeps every name, and the
message changes so a locally-supplied name says where the value comes from.
Dropping the names from `REQUIRED_ENV` was rejected because it would move the
failure to the moment a call arrives, which Principle II names as the worst
trade available.

**Eight parallel-marker collisions.** Twelve tasks carried `[P]` while sharing a
file with another `[P]` task, including a Parallel Opportunities line that
asserted "T043, T047, T048, and T051 touch different trees" which T043's own
description contradicted. The recurring shape was "add a case to the same test"
tasks marked parallel with the task that creates that test. All corrected, and
the remaining eight `[P]` tasks in Phase 2 are in eight distinct files.

The other ten were smaller: FR-011 forbade the resolution research D9 had
already chosen (now permits all three, and D9 records why the narrow reading was
wrong), three artifacts disagreed on the defect count (now seven reproduced
groups, eleven test functions, sixteen test tasks, each stated where it belongs),
FR-027's "teaches nothing" bar was unmeasurable (now row-uniqueness in
`examples/README.md`), T062 gave no method for enumerating validator errors (now
the Wave A method, because static enumeration alone found thirteen unreachable
sites), FR-007c's `docs-site/` half had no task (now T032a), the success criteria
were out of order, and the plan's phase numbers did not line up with the tasks'.

## Cross-artifact analysis, round 2, 2026-08-15

A second `/speckit-analyze` pass, focused on the repository-root files, the
subagent verification, the `unmute init` path, and CLAUDE.md alignment, found
five issues. Zero constitutional violations, and all five were remediated.

- **The spec quoted a CLAUDE.md that no longer exists.** It said the rule was
  "three places document a change" and called the bundle a fourth place, while
  CLAUDE.md on this branch's own base (commit cb4ff13, PR #80, same day the
  spec was written) says "five places document a change, not one" and already
  lists the skill as the fifth. Every "four surfaces" count across spec, plan,
  and tasks now says five, with `docs/` and `docs-site/` counted separately.
  The substance was never at risk, since FR-031 named both trees all along;
  only the count and the quote were wrong.
- **FR-004 contradicted research D1, T016, and the message contracts.** It
  still demanded `ir.Validate` for checks whose messages carry the file and
  line only `ir.Build` can provide, a contradiction with the spec's own
  FR-007b tier table. The plan's gate re-check had recorded the deviation, but
  the spec was never amended. FR-004 now states the tier split and names D1 as
  the record of the correction.
- **The repository-root `.env.example` had no owner for two claims this
  feature falsifies**: its Langfuse comment ("needed for the examples") stops
  being true when T067 leaves tracing in exactly one example, and its header
  names an agent that does not exist at the repository root. Both are now
  inside FR-016e and T047, with T068 confirming the cross-reference after the
  tracing example is named.
- **T077 cited SC-002 for the ten-agent authoring re-run**, which is SC-005's
  bar; SC-002 belongs to T078's adversarial agents. Corrected, so Wave B and C
  verifiers measure against the right criterion.
- **User Story 3 had two acceptance scenarios numbered 3.** Renumbered to 6.

Coverage after remediation is unchanged: 66 of 66 functional requirements and
15 of 15 success criteria carry tasks, and no task is unmapped. One loose end
lives outside the tracked tree and is recorded here so it is not lost: the
private `.env` files carried a key named `11LABS_API_KEY`, which no shell can
export and the CLI never reads (it wants `ELEVENLABS_API_KEY`). Both copies
were renamed on 2026-08-15; rotating the key value itself stays with the
author, on the ElevenLabs dashboard.

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
Removing every non-author name from the emitted files removes the block that
told a self-hosted operator what to supply. That information survives in a
better form: `route.ManualSteps` already emits "get `LIVEKIT_URL` and the API
key pair from the LiveKit Cloud project settings, or from a self-hosted LiveKit
Server configuration", which names the source rather than only the variable, and
`compile-report.json` keeps the machine-readable list. FR-018e holds both. The
author's own rule carries the exemption that keeps a genuine developer note,
such as what to do when host port 5060 is taken, which FR-018f places in a
troubleshooting section rather than a to-do list.

**An adversarial pass over the compiled files found the classification already
exists.** `internal/target/telephony.go` carries `LocallySuppliedEnvironment`
per route, already correct about four names, and three things ignore it: the
Pipecat env template does not read it at all, the LiveKit one labels instead of
excluding, and `UNMUTE_PUBLIC_URL` and `UNMUTE_OUTBOUND_TOKEN` are missing from
it despite `unmute dev` minting both. So the requirements name a data fix and
two template fixes, not a new abstraction. The sharpest single finding is that
LiveKit's generated file asks for `REDIS_URL` under a comment that ends "which
this agent never reads", and the comment is right.

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
