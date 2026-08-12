# Contract: the authoring surface

This contract is mostly a statement of what does **not** change. That is deliberate. The requester's decision was "say nothing in the yaml", and a contract that only listed additions would leave nothing to stop a later reader re-proposing the fields that were just declined.

## What does not change

**No hosting field.** There is no `hosting:`, no `deployment:`, and no equivalent. One hosting model per driver means there is nothing to select. A test asserts the derived authoring schema gains no such field, so adding one later fails a check and forces the decision back into the open.

**No telephony channel on the Daily route.** A Daily package continues to declare `channels:` with no telephony entry. Direction, controls, and prerequisites for that route are derived by the compiler from the controls the package already declares.

This also avoids a side effect worth recording: `docs/SCHEMA.md` §4.10 requires `capacity` whenever `channels` carries a telephony entry. Adding a Daily phone channel would drag capacity in with it, for a route where the platform owns the concurrency.

**No change to the transfer shape.** `cold:` and `warm:` blocks, `destination`, `briefing`, `ring_timeout`, `on_unavailable` all stay exactly as the base branch defined them in §4.7. This feature does not touch them.

**No change to region syntax.** `deployment_region` keeps its current form and stays forwarded as declared.

**No caller-identity field.** Removed from User Story 5 on 2026-08-12. There is no `caller_id` anywhere in `docs/SCHEMA.md`, `internal/ir/compiler.go`, or `internal/spec/package.go` today, so adding one is a new authoring surface. The constitution's compliance review requires such a change to land in one commit together with a numbered dated SCHEMA amendment, the derived schemas, the capability table, the agreement tests, the scaffold templates, the interactive console, the in-repository examples, and `docs/user/`. That is a feature of its own. Until it lands, the outbound recipient sees whatever the provider picks and the emitted instructions say so.

**The net effect: this feature adds no authoring field at all.** Spec FR-003 records what the next attempt has to carry, and FR-030 requires a test asserting the derived authoring schema stays clean, so a field cannot arrive as a quiet patch.

## What the yaml already says, unchanged

A Daily cold-transfer package looks like this today and looks the same after this feature:

```yaml
# agent.yaml
controls:
  send_to_billing:
    kind: human_transfer
    when: The caller asks about an invoice, a refund, or a charge they do not recognise.
    cold:
      destination: billing_line

# No phone channel: on this route the telephony is Daily's own.
channels:
  # ... realtime_audio only
```

```yaml
# targets.yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    transport: daily-sip
    destinations:
      billing_line: BILLING_PHONE_NUMBER
```

The only thing an author may add that is new to this feature is nothing. The behaviour changes are in what the compiler reports and what it emits.

## What the documents must now state

Not yaml, but part of the authoring contract's job, because a reader has to be able to learn these without opening a generated project.

1. **`docs/SCHEMA.md` gains a numbered dated amendment** recording that Pipecat telephony is the Daily route, and narrowing the §4.9 statements that currently describe carrier-WebSocket inbound and outbound more broadly than the code supports. Appended, never rewritten in place. Research D6.
2. **The route's account prerequisites are documented as route facts**, derived from the rulebook rather than restated, with a sync test in both directions as the provider reference pages already have.
3. **The transfer reference records the dated result** of the run that proves the Daily cold transfer, and any step the run proved wrong lands in that document first.

## Backward compatibility

The binding rule: a package that validates today either behaves identically or fails with a message naming what to change. It never silently changes meaning.

| Existing package shape | After this feature |
|---|---|
| Pipecat Daily target, cold transfer, no phone channel | Identical validation result. The report gains a prerequisite line. The emitted `bot.py` changes one line, the Daily transport parameter class. |
| Pipecat carrier websocket target with a telephony channel | Untouched. Same services, same endpoints, same credentials, same goldens. |
| LiveKit SIP target, either transfer mode | Untouched. |
| Any package with `deployment_region` | Identical, plus a refusal if the region cannot be honoured. |
| Any package with no region | Identical. The emitted instructions state the default and that the credential store follows it. |

FR-025 keeps the Daily example in the public example set rather than moving it somewhere test coverage does not reach. The existing example tests give partial cover, but "partial" is not what FR-004 promises, so FR-031 requires an explicit before-and-after comparison across every in-repository example. That gap was found by `/speckit-analyze` on 2026-08-12: this table asserted the coverage existed, and no task actually produced it.
