# Implementation Plan: Brief the manager, then hand the call over

**Branch**: `feature/warm-cold-human-transfer` (feature dir `003-warm-transfer-briefing`) | **Date**: 2026-08-12 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/003-warm-transfer-briefing/spec.md`

## Summary

Keep the platform's warm-transfer prebuilt. Change three things around it, all inside the LiveKit driver's template.

1. **Own the manager-facing identity.** Replace the deprecated extra-instructions parameter with the platform's supported instructions hook, `WorkflowInstructions`, and put an emitter-owned persona in it. The persona is where "open with the summary, never greet, here is what to say to take the call, and here is what to say when the transcript is thin" lives. The platform's own template keeps supplying the transcript, the context section and the confirm-tool sentence, so we replace the identity and nothing else.
2. **Make the transfer legible.** Three lines on any single transfer, warm or cold, all at info, all emitted by our own template, with every outcome named: the control fired, the dial or referral went out with how many conversation messages the briefing received, and how it ended with how long it took. No destination value is logged at all, which is how the no-secrets rule is met structurally rather than by careful wording.
3. **Reduce the unbounded hold using the platform's own surface.** The manager-facing model already has a decline tool. The persona tells it to use that tool when the manager goes quiet or never gives a clear answer, which routes the stuck case into the same unavailable path a failed dial already takes, with the platform's own cleanup. This is a mitigation, not a hard bound, and the plan says so plainly: see [Complexity Tracking](#complexity-tracking).

No authoring field is added. No new dependency. No Go beyond the emitter and its tests.

## Technical Context

**Language/Version**: Go 1.24 for the compiler. Emitted output is Python 3.10+ running `livekit-agents`.

**Primary Dependencies**: unchanged. `cobra`, `goccy/go-yaml`, `google/jsonschema-go`, the Charm stack. The emitted project gains one standard-library import (`time`) and one more name from a module it already imports.

**Storage**: N/A.

**Testing**: L1 unit and L3 golden in `internal/generate`. L4 smoke is environmentally broken on this machine (CPython 3.14 has no wheel for a transitive dependency, confirmed pre-existing on 2026-07-19 and again on 2026-08-12) and is not the gate. The end-to-end proof is a live LiveKit Cloud call, which cannot run in CI.

**Target Platform**: LiveKit Cloud, SIP route, deployed agent. The laptop routes have no transfer primitive and are out of scope.

**Project Type**: compiler. One driver template plus its tests and documents.

**Performance Goals**: N/A. The only timing that matters is that a normal consultation is never cut short, which is why no hard timeout is introduced.

**Constraints**: Go templates only. No secret and no destination value in any emitted file or log line. The authoring surface must not widen and must not break. Goldens are read, not regenerated blind.

**Scale/Scope**: one template file, one test file, two or three goldens, three documents. Roughly 40 lines of emitted Python moved or added.

**NEEDS CLARIFICATION**: none. The one verification item, [research R1](./research.md#r1-the-version-gap), **closed on 2026-08-12** against the real 1.6.9 package inside the image built from a compiled example. It found one break: the instructions hook class was renamed from `InstructionParts` to `WorkflowInstructions` with no alias, so the plan's first draft would have emitted a project that failed at import. Everything else in the platform file is byte-identical to the 1.6.4 reading.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Verdict | Reasoning |
|---|---|---|
| **I. Compile ahead of time, never interpret at runtime** | PASS | Everything lands as Go template text in `internal/generate/templates/livekit_v1/`. The generated project keeps carrying no Unmute dependency and stays readable and runnable without it. The briefing transcript never leaves the process running the call, which is what the media-and-transcripts rule requires. Nothing in the compiler learns a prompt at runtime. |
| **II. Fail loud, never average** | PASS with one recorded exception | Every failure path keeps ending in the package's declared unavailable behaviour, in the platform's own words. The exception is the consultation with no hard bound: the platform exposes no post-answer timeout and no public way to complete the task with cleanup, so the emitted code cannot guarantee it. Principle II requires a known exception to be a dated numbered amendment in `docs/SCHEMA.md` that states the cost, which is task-listed as amendment N34. Silence about it would be the violation. |
| **III. One source of truth per surface, derived not copied** | PASS | The persona text has exactly one home, a Go template constant. The message count we log is the count of what we hand over, and the log line says exactly that, so it is not a second copy of the platform's own transcript filter. No capability row moves. `internal/target` is untouched: no route gains or loses a feature. |
| **IV. The document wins** | PASS | Every platform claim in the spec and this plan cites a source file or a documentation page with the date 2026-08-12, and all of them are now verified against the 1.6.9 package the deployment actually runs, not the 1.6.4 checkout. R1 closed and earned its place: the rule that an unverified claim stays gated is what stopped an unimportable emitted project. `docs/SCHEMA.md` gains amendment N34; `docs/TRANSFERS.md` gains the two new facts. |
| **V. Whatever compiles can be spoken to** | PASS | No command surface change. No authoring field. A package written before this change compiles with no edit, which is FR-016 and gets its own test. |
| **Secrets boundary** | PASS, strengthened | No destination value and no credential reaches any log line, because no destination is logged at all. The control's own name already identifies which destination fired, so printing the value would add nothing and risk everything. |
| **Complexity must be justified** | PASS | No new dependency, no new abstraction, no knob. One standard-library import for elapsed time, justified by the one failure mode nothing else can see: a manager who answered and never decided. |
| **Generated Python discipline** | PASS | Emitted Python is checked with `ruff` and parsed against the real installed package before the feature is called done. |

## Project Structure

### Documentation (this feature)

```text
specs/003-warm-transfer-briefing/
├── spec.md
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   ├── emitted-briefing.md
│   └── transfer-log.md
├── checklists/
│   └── requirements.md
└── tasks.md             # Phase 2, not created here
```

### Source Code (repository root)

```text
internal/generate/
├── templates/livekit_v1/
│   ├── agent.py.tmpl              # the whole behaviour change lives here
│   └── README.md.tmpl             # what to expect on the manager's phone, how to read the log
├── livekit_v1_test.go             # assertions for the emitted shape
├── livekit_inline_trunk_test.go   # existing warm-only fixture, reused
└── testdata/golden/
    ├── livekit_v1_remy.txt        # read the diff, do not regenerate blind
    └── livekit_v1_sip*.json       # must not move

docs/
├── SCHEMA.md                      # amendment N34
├── TRANSFERS.md                   # the warm transfer section
└── user/learn/07-phone-calls.md   # only if it describes what the manager hears

examples/human-transfer/
└── README.md                      # expected first sentence, expected log lines
```

**Structure Decision**: single driver change. `internal/spec`, `internal/ir` and `internal/target` are untouched, because nothing about the authoring surface or any capability row changes. That is the strongest evidence the design is right: a defect in what the generated agent says should be fixable in the template that says it.

## Design decisions

### D1. Keep the platform's prebuilt

Rejected: hand-building the documented manual workflow (consultation room, second session, token, participant move).

The manual route puts token minting, a second room, a second session, hold music, the participant move and every lifecycle edge inside every generated `agent.py`. `docs/SCHEMA.md` N31 already records what happens then: the custom transfer designs in this repository "were built, live-tested, and then deleted: every custom design made the generated process own the call's audio path", and each live test found a new lifecycle bug. Nothing in this feature needs that ownership. Every requirement is reachable through the prebuilt's public parameters, with one exception that the manual route would not fix either, because the same missing timeout would then be ours to invent.

### D2. Replace the persona, keep the platform's template

The instructions hook, `WorkflowInstructions`, takes a persona section and an extra section, and the platform interpolates both into its own template alongside the transcript, the context paragraph and the sentence that names the confirm tool. So passing a persona replaces the identity only. Verified by running it against the installed 1.6.9 package rather than by reading it: with a persona supplied, the persona replaced the default and no other section moved. That is exactly the surface this feature needs and no more: the transcript wiring stays the platform's, and our text is the part that was wrong.

Rejected: overriding the whole instruction string. It would take ownership of the transcript interpolation and the confirm-tool sentence, both of which work, and every upstream improvement to them would then pass us by.

Rejected: a stronger model for the briefing. The manager-facing session reuses the package's models by design. A per-transfer model would be a new authoring field, which FR-016 forbids and which the constitution's complexity rule would ask us to justify. A package that needs a better briefing changes its agent's model, which affects the caller too, and that tradeoff is the author's.

### D3. Three lines per transfer, no destination values

The phases the emitted code can actually observe are: the control fired, the dial was requested, and how it ended. **The moment the manager answers is not observable from outside the prebuilt**: it awaits the answer internally and exposes no callback, and its own line for it is at debug. Rather than turn on framework debug for the whole process, the emitted log records the dial request with the handed-over message count, and the outcome with elapsed seconds. Answered-and-decided, never-answered and answered-but-undecided are then distinguishable by outcome plus duration. This narrows one acceptance scenario in User Story 2, and [research R4](./research.md#r4-what-the-emitted-code-can-and-cannot-see) states the narrowing rather than leaving it to be discovered.

### D4. The unbounded consultation

The platform has no post-answer timeout, and completing the task from outside without its cleanup would leave the caller muted with music playing. Both are read from source in [research R5](./research.md#r5-why-there-is-no-hard-bound). So the emitted code uses the platform's own decline tool as the exit: the persona tells the manager-facing model to decline with a reason when the manager goes quiet or never answers the question. That routes the stuck case into the unavailable path the package already declares, with the platform's own restore.

It is a mitigation, not a bound, and a prompt is not a bound. **Decided 2026-08-12: the mitigation is accepted.** The spec was rewritten to match on the same day, so FR-010 now asks for the best-effort exit, FR-011 is withdrawn with its reasoning kept, FR-012 forbids emitting a timeout constant that nothing enforces, and SC-005 measures the exit over three live attempts instead of claiming a bound. The two ways to get a real bound stay in Complexity Tracking, and a live call where the exit does not hold is what reopens the choice.

## Complexity Tracking

| Item | Why it is here | The alternative, and why it is not taken |
|---|---|---|
| The consultation has no hard bound. Accepted 2026-08-12, and the spec was rewritten to say so rather than left claiming a bound | The platform exposes no post-answer timeout, and its cleanup entry point is private. Cancelling the awaited task does not cancel it, because it is shielded, so a naive timeout leaves a live consultation, hold music playing and the caller's audio still disabled. Source: [R5](./research.md#r5-why-there-is-no-hard-bound). | **(a) Call the private cleanup entry point on timeout.** One private attribute, deterministic cleanup, and a name upstream can rename in a patch release with no deprecation. It would also need a fallback for when the name is gone, and a fallback that silently does nothing is exactly what principle II forbids. **(b) Own the hold music so we can stop it, and orphan the task.** Leaves a live manager-facing session and room behind, which is the class of bug N31 records. Neither is taken without the user choosing it, and both stay on the record here rather than being quietly dropped. |
| One standard-library import added to emitted Python (`time`) | Elapsed seconds on the outcome line is the only signal that separates "nobody answered" from "answered and never decided", and those two have different fixes. | Omitting it leaves the single failure mode this feature most needs to see indistinguishable from another one. |

## Phase 0 output

[research.md](./research.md): R1 the version gap (closed, and it caught a rename), R2 the instructions hook, R3 what the transcript actually carries, R4 what the emitted code can and cannot see, R5 why there is no hard bound, R6 why the briefing failed on the live call, R7 the logging level question.

## Phase 1 output

- [data-model.md](./data-model.md): the template model fields this feature reads, all of them existing, and the emitted constructs it adds.
- [contracts/emitted-briefing.md](./contracts/emitted-briefing.md): the shape of the emitted warm-transfer call and what the persona must contain.
- [contracts/transfer-log.md](./contracts/transfer-log.md): every log line, its level, and what may never appear in one.
- [quickstart.md](./quickstart.md): the live validation run, with the expected first sentence and the expected log for each outcome.

## Post-design Constitution re-check

Re-read after Phase 1, and again after R1 closed. One verdict improved: principle IV went from conditional to unconditional once every claim was verified against 1.6.9. The design touches one template, one test file, three documents and no Go outside `internal/generate`, which is what a change to what a generated agent says should touch. The single recorded exception (no hard bound) carries its source, its cost, its two alternatives, and a required `docs/SCHEMA.md` amendment, which is the form principle II asks for. Nothing now gates a template edit.
