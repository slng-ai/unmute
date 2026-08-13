# Contract: command behaviour on the Daily route

Output contracts for `validate`, `compile`, and `dev`. Written as behaviour rather than exact strings, except where the spec requires a fact to be present, because pinning wording in a contract makes every copy edit a contract change.

All output goes through `cmd.OutOrStdout()` and `cmd.ErrOrStderr()`. No `fmt.Println`. Exit codes stay 0 for success and 1 for error.

## `unmute validate`

**Today.** A Daily cold-transfer package prints one line, `✓ pipecat`, and exits 0. Verified by running it on `examples/human-transfer-daily`.

**After.**

| Condition | Output | Exit |
|---|---|---|
| Daily package using `cold_transfer` | The result line, plus the route's account prerequisite named on stderr as a prerequisite, not a warning. States what to ask the provider for. | 0 |
| Daily package using neither cold transfer nor outbound | The result line only. No prerequisite text. | 0 |
| Package declaring a capability the Daily route lacks | Failure naming the capability **and** the route, and naming where that capability does exist. | 1 |
| Region that cannot be honoured | Failure naming the region and what is wrong with it. | 1 |
| More than one region | Existing gated failure, unchanged. | 1 |

**Rules.**

- The prerequisite line is not a warning and must not be counted as one. It states a fact about the route. It must not appear for a package that does not use the capabilities that need it, which is the only thing keeping it from becoming a standing banner authors learn to ignore. Research D3.
- A refusal names the fix, not only the problem: which routes offer the capability, or what to change. This is the existing pattern in the route table's refusal notes and this feature does not invent a second style.
- Nothing here prints a route's provisional status. That stays internal maturity tracking in the compile report.

## `unmute compile`

**Today.** Emits a complete project for the Daily example, with bindings and sizing reported. Verified by running it.

**After.**

| Requirement | Detail |
|---|---|
| Everything `validate` prints | `compile` runs validate first, so the prerequisite line and every refusal apply here too. |
| The prerequisite reaches the artifact | Present in the emitted README and in `compile-report.json`. |
| The region reaches every reference | The deploy manifest region line and the credential-store command, both derived from the one declared value. |
| The forwarded region is inspectable | Named in the compile report, because it is forwarded without checking. |
| The Daily transport parameter class | The emitted `bot.py` constructs a parameter object that accepts inbound call fields for the Daily key. |
| Refusal to emit | Unchanged: no artifact is written when any gated error is present. |

## `unmute dev`

This is the one place a new refusal is required rather than optional.

| Mode | Daily route behaviour |
|---|---|
| default, browser | Works. Unchanged. |
| `--console` | Works. Unchanged. |
| `--telephony` | **Refuses**, naming the two modes that do work and pointing at the deploy path for a real phone call. Exit 1. |

**Why the refusal is required.** Daily PSTN delivers a call through Daily's own infrastructure to a deployed agent. The requester's decision keeps local runs free of any cloud account, so there is nothing for `--telephony` to stand up locally on this route. Leaving the flag accepted would either do nothing or start a graph that no call can reach, and Principle II forbids a field that silently does nothing. The refusal is the loud version.

**What the refusal must contain.** The route it is refusing for, the modes that do work on that route, and where to go for a real phone call. It must not suggest that telephony is unsupported on the route, which would be false: the route is the only Pipecat telephony route there is. The distinction it has to draw is local versus deployed, which is the same distinction the two corrected documents now carry.

## What none of these do

- No command provisions anything on the carrier or the platform side. The telephony boundary in the constitution holds: Unmute does not buy numbers, create carrier applications, or create trunks, and it automates only Unmute-owned local development state, restorably. Nothing in this feature touches a Daily account or a Pipecat Cloud deployment.
- No command reads a named environment value. The loader continues to handle env var names only.

## Test seams

- `validate` and `compile` output is asserted at L2, in process against the real cobra tree with output captured.
- The `--telephony` refusal is asserted at L2 with the message content checked, not just the exit code, because the content is the whole point of the refusal.
- Emitted bytes are pinned at L3 with `-update-pipecat`.
- The parameter class is proven for real at L4, which installs and instantiates against the pinned upstream package. That layer is the only one that catches a wrong import, and the import spelling is a known open question from research D1.
