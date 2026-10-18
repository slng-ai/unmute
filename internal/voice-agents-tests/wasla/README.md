# wasla

An outbound payment reminder, ported from unmute-core's `examples/wasla`. Lina rings the holder of one overdue buy-now-pay-later account. She checks the date of birth, says what is overdue, and records one outcome: a promise to pay, a callback, a wrong number, or a concern.

The scenarios in `scenarios/` are unmute-core's, unchanged. The e2e suite expects the same lines, the same `max_requests` and the same final state.

## What it proves

| Feature | Where |
|---|---|
| `call_start` required value | `variables: account_id`, typed `WA` and four digits in `state.py` |
| Prefetch before the greeting | `clock` (Dubai) and a `tool` (`open_account`) that saves the holder's name |
| Greeting with placeholders | `{{state.first_name}} {{state.last_name}}` |
| `start:` with `listen: true` and `say:` | `identify` asks the date of birth with no model request |
| `confirm` and `skip_when_confirmed` | `birth_date` is confirmed by `identify`; the group skips it later |
| An outcome with `then: done` per result | promise, concern, callback and wrong number each read back, then a receipt |
| `start: call:` with group arguments | a callback day and time the caller gave are read back with no request |
| Groups with `then: end` | `callback` and `wrong_number` end the call after the receipt |
| `with:` in the tool file | the account and the call id are never sent by the model |
| `precheck` and `outcomes` | a day in the past or after the deadline is refused in code's words |
| Nothing about the account before the check | details come only from a matching `verify_holder` |

## Gaps

| Core behaviour | What is written instead |
|---|---|
| One `FinishRule` per concern kind, each with its own receipt | One `note_concern` outcome with `then: done`. The tool returns the receipt in `said`, and the line is `{{state.said}}`. Same words |
| `good_time` false ends the step, then the main agent starts a callback | A reply says the same line, and the step stays open. The model then calls `schedule_callback` in the same step. Not covered by a scenario |
| `birth_date` saved from the verified result | Saved from the input once the check matches |
| `new_call` refuses an account the ledger does not hold | The `account` prefetch's `refuse_call:` says the same line, and the call does not start |
| A web call is `outbound` (`CallInfo(direction="outbound")`) | Not expressed: a web call has no direction. No scenario reads it |
| Ledger as a class | Module-level data and functions in `tools/ledger.py`, with the words helpers inlined |
| Core turn pace `balanced` on both targets | `targets.yaml` sets `balanced` on the LiveKit detector: v2 refuses a per-target pace |

Validate prints one warning on both targets: `talk_account` opens with `listen: true` and no `say:`. That is the same as core: the question before it is the `identify` receipt.
