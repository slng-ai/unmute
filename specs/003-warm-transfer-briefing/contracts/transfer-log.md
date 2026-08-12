# Contract: the transfer log

Every line a human transfer writes, at the level a deployed agent shows by default. This
is the primary evidence for every other requirement in the feature, because the
end-to-end behaviour lives in a room the operator cannot join.

All lines go to the logger the emitted package already creates, at **info**. Nothing here
requires framework debug output, and turning it on is explicitly rejected in
[research R4](../research.md#r4-what-the-emitted-code-can-and-cannot-see).

## Warm transfer

| Order | Line | When |
|---|---|---|
| 1 | `human transfer fired: <method> (warm)` | the model called the tool. Exists today. |
| 2 | `warm transfer dialling out: handing over <n> conversation messages` | immediately before the dial |
| 3a | `warm transfer merged after <n>s: <participant identity>` | the calls joined |
| 3b | `warm transfer unavailable after <n>s: <platform reason>` | every other ending: no answer, declined, voicemail, failed dial |

Exactly one of 3a and 3b appears. A transfer with line 2 and no line 3 is a consultation
still in progress or one that never ended, and that gap is itself the signal.

## Cold transfer

| Order | Line | When |
|---|---|---|
| 1 | `human transfer fired: <method> (cold)` | the model called the tool. Exists today. |
| 2 | `cold transfer referring the caller out` | immediately before the request |
| 2alt | `cold transfer skipped: no phone caller in the room` | the room holds no SIP leg, so the tool returns without referring |
| 3a | `cold transfer completed after <n>s` | the referral succeeded |
| 3b | `cold transfer failed after <n>s: <platform reason>` | the referral was refused or errored |

**Line 2alt reverses this contract's own first decision, on live evidence, 2026-08-12.**
It originally read: "when the room holds no phone caller, the tool returns and says so to
the model. That path writes no new line, because line 1 plus the absence of line 2 already
says it." That was wrong in practice. On the first live cold test the tool was called from
the Agent Console, where there is no SIP leg to refer, so it took exactly this branch. The
log showed the tool firing and then stopping, the agent said something vague, and the run
was read as a broken transfer rather than as a test run from a place cold cannot work.
**An absence is only a signal to a reader who already knows to look for one**, and the
person debugging did not. The line, plus the reason the returned message now names, cost
four lines of emitted Python between them.

This is also the one fact about cold worth repeating wherever cold is documented: it acts
on the caller's **existing** leg, so unlike warm it cannot be exercised from the Agent
Console at all. Warm dials out and needs no inbound leg, which is why the two have
different test rigs.

## Rules

| # | Rule | Requirement |
|---|---|---|
| L1 | Every line is info. None depends on framework debug being enabled. | FR-006 |
| L2 | The handed-over message count appears on every warm transfer, and it counts what was handed over, not what survived the platform's own transcript filter. The line says so in its own words. | FR-006, principle III |
| L3 | Elapsed seconds appear on every outcome line, from a monotonic clock, whole seconds. | FR-006 |
| L4 | A transfer that ended without a merge is distinguishable from one that merged by the line text alone, with no field to parse. | FR-007 |
| L5 | **No destination value ever appears**: no phone number, no SIP URI, no environment variable value. The control name on line 1 already identifies which destination fired. | FR-008, secrets boundary |
| L6 | **No credential ever appears**, in any form. | FR-008, secrets boundary |
| L7 | The caller's words never appear. The count is logged; the transcript is not. | secrets boundary, and the caller's privacy |
| L8 | The participant identity on the merge line is the platform's own value for the joined participant, which is not a phone number. | FR-008 |

## How a reader uses it

The four warm outcomes and what tells them apart, which is the whole point of the story:

| What happened | The log |
|---|---|
| Briefed, accepted, joined | line 2 with a healthy count, then merged, duration in the tens of seconds |
| Nobody answered | line 2, then unavailable, duration near the package's ring timeout |
| Answered and declined | line 2, then unavailable with the colleague's reason |
| Answered, never decided | line 2, then unavailable with a decline reason, duration well past the ring timeout |
| Briefing had nothing to say | line 2 with a count of zero or one. This is the case that needs no live listening to diagnose. |
| The model ignored its instructions | line 2 with a healthy count and a merge or decline that the caller's experience contradicts. The count is what separates this from the row above, and they have different fixes. |
