# Contract: the emitted warm-transfer briefing

What `unmute compile` must put in `build/<target>/agent.py` for a warm human transfer on
the LiveKit SIP route. Anything an offline test can check is listed as a rule with a
requirement beside it, because the end-to-end proof is a live call and cannot run in CI.

Platform shapes cited here are verified against `livekit-agents` **1.6.9**, the version the
deployment runs, read out of the image built from a compiled `examples/human-transfer` on
2026-08-12 and exercised inside it. See [research R1](../research.md#r1-the-version-gap),
including the class rename that a 1.6.4-only reading would have got wrong.

## 1. The call

```python
briefing_ctx = self.chat_ctx
logger.info(
    "warm transfer dialling out: handing over %d conversation messages",
    len(briefing_ctx.items),
)
started = time.monotonic()
try:
    result = await WarmTransferTask(
        sip_call_to=<destination expression>,
        sip_connection=_sip_trunk(),
        sip_number=_sip_number(),
        chat_ctx=briefing_ctx,
        instructions=WorkflowInstructions(
            persona=_BRIEFING_PERSONA,
            extra=<the authored briefing, when the package declares one>,
        ),
        ringing_timeout=<the authored ring timeout, when the package declares one>,
    )
```

Rules:

| # | Rule | Requirement |
|---|---|---|
| C1 | `instructions=WorkflowInstructions(...)` is present. `extra_instructions=` appears in **no** emitted file. | FR-002 |
| C2 | `persona=` is the module-level constant, never an inline string, so several warm transfers in one package share one text. | FR-001, principle III |
| C3 | `extra=` carries the package's authored briefing verbatim when there is one, and the argument is **absent** when there is not. An empty string would delete the platform's own section rather than leave its default. | FR-002 |
| C4 | The conversation is read into `briefing_ctx` **once** and both the log line and `chat_ctx=` use that local. | FR-003, FR-006 |
| C5 | The trunk and number arguments, the destination expression and the ring timeout are unchanged from what feature 002 emits. | FR-016 |

## 2. The persona constant

Emitted once per package that has any warm transfer, beside the other transfer helpers,
with a comment that says what it replaces and why each part of it is there.

It replaces **only** the platform's identity section. The platform's own template still
supplies the paragraph that says who is who, the caller transcript, the sentence naming
the tool that joins the calls, and the instruction to open with a summary. The authored
briefing still lands last.

The text must say four things. Each one is a defect observed on a live call or read from
source, not a preference:

| # | It must say | Because | Requirement |
|---|---|---|---|
| P1 | Open with the handover, not a greeting. No hello, no offer to help, do not wait to be asked what the call is about. State who is on hold, what they want, what was already tried, then ask one answerable question. | On 2026-08-12 a manager answered and got a generic greeting, and had to ask what the call was about. That is the one thing a warm transfer exists to prevent. | FR-001 |
| P2 | Tell the colleague that the caller will be put through as soon as they say they are ready. | Whether the calls join is the manager-facing model's tool call and nothing else can trigger it, so the manager has to be given the cue that leads to it. | FR-005 |
| P3 | When the conversation history is empty or has no caller words, say that someone is on hold asking for a person and that their details are not known yet, then ask whether the colleague can take it. Never fall back to greeting. | The most likely cause of the live failure is a thin transcript, and a model asked to summarise nothing improvises a greeting. A thin briefing is a degraded transfer; a greeting is a broken one. | FR-004 |
| P4 | Decline the transfer, with the colleague's reason, when they say they cannot take it, when they go quiet, or when the conversation moves on without an answer. Never leave the question open, because the caller is holding the whole time. | The platform has no timeout once the call is answered, and its decline tool is the only exit that stops the hold music and restores the caller. See [research R5](../research.md#r5-why-there-is-no-hard-bound). | FR-010 |

Rules:

| # | Rule | Requirement |
|---|---|---|
| C6 | The constant is emitted when and only when the package has at least one warm transfer. A cold-only package gains neither the constant nor the extra import. | principle III, and the warm-only fixture already in the suite |
| C7 | The comment above it names the version and date the persona was verified against. | FR-013 |
| C8 | The text contains no destination, no credential and no environment variable value. | FR-015 |

## 3. What must not change

| # | Rule | Requirement |
|---|---|---|
| C9 | The caller-facing lines are untouched: the spoken handoff before the hold, the spoken line after the merge, the session shutdown after it, and the return value being absent on every path that ends the session. All four were confirmed on a live call and are not reopened. | spec Assumptions |
| C10 | Hold music stays the platform's default. The caller side works and the author said so. | spec Assumptions |
| C11 | Cold transfer gains log lines and nothing else. Its request, its destination handling and its failure branches are byte-identical apart from logging. | FR-009 |
| C12 | The emitted inbound trunk and dispatch-rule files do not move. | feature 002 SC-011 |
| C13 | No new authoring field, and a package written before this change compiles with no edit. | FR-016 |
