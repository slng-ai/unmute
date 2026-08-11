# Reference: the safe core

The **safe core** is the subset of the schema that passes validation on every
one of the four targets. Write inside it and the same package runs on LiveKit,
Pipecat, Vapi, and Deepgram, with warnings at most, never a failure.
The compiler's [safe-core regression fixture](../../../internal/testdata/safe_core/)
follows these rules exactly. This page is the authoritative list from
`SCHEMA.md` section 7.

Exact Pipecat and LiveKit telephony routes are temporarily outside this core:
all remain provisional until their credentialed route smokes pass.

## The rules

1. Any number of agents with `agent_transfer` between them (T0 + T2).
2. Every transfer context: `history: full`, `variables: all`.
3. Tools: a `webhook:` block, `interruption: provider_default`, `effect: returns_data`.
4. Human transfer: omit it while evaluating provisional Pipecat and LiveKit
   telephony routes. Their exact carrier behavior is not provider-wide.
5. Models: hosted providers for `listen` and speak models (no `provider: local`). `turn` is a preference anyway.
6. If the agent speaks first, give it a fixed `greeting.text`. A model-written opening is generated-with-a-warning on Deepgram; a fixed line is the zero-warning choice.
7. Skip for now: single `tasks` (return to owner is unverified on Vapi) and
   `task_groups` with `then: return` (fails on Vapi). A `task_group` with
   `then: transfer` or `end` does pass on all four (with a warning on LiveKit
   that TaskGroup is experimental). Also skip `requires`, `thinking_audio`,
   tracing, telephony, warm transfer, `mcp` and `local` tools, and any history
   other than `full`. `fallback` passes everywhere when the chain stays within
   one provider on Vapi.
8. Accept warnings: interruption tuning on Deepgram, turn model notes.

## Feature by feature

`ok` works, `warn` works with a warning, `fail` fails validation.

| Feature | LiveKit | Pipecat | Vapi | Deepgram |
|---|---|---|---|---|
| single agent (T0) | ok | ok | ok | ok |
| agent_transfer, `full` + `all` | ok | ok | ok | ok |
| fixed opening line (`greeting.text`) | ok | ok | ok | ok |
| model-written opening (no `text`) | ok | ok | ok | generated (warn) |
| user speaks first (`speaks_first: user`) | ok | ok | ok | warn |
| task | ok | ok | unverified | ok |
| task_group, `then: transfer\|end` | warn | ok | ok | ok |
| task_group, `then: return` | warn | ok | fail | ok |
| history `messages` / `last_n` / `reset` | ok | gated (driver v1) | ok | ok |
| history `summary` | ok | gated (driver v1) | fail | ok |
| `requires:` | ok | ok | fail | ok |
| `fallback:` (think) | ok | gated (driver v1) | conditional | ok |
| `fallback:` (listen) | ok | gated (driver v1) | fail | fail |
| human_transfer cold | provisional on SIP routes | provisional carrier REST | ok | carrier-conditional |
| human_transfer warm | provisional on SIP routes | provisional on Twilio carrier WebSocket | Twilio only | carrier-conditional |
| `thinking_audio` | ok | gated (driver v1) | fail | fail |
| `provider: local` (listen/speak) | ok | ok | fail | fail |
| webhook tools | ok | ok | ok | ok |
| webhook `auth` (bearer/api_key) | ok | ok | fail | fail |
| mcp tools | Python only | gated (driver v1) | ok | fail |
| outbound + `on_voicemail` | provisional on SIP routes | gated (voicemail not emitted) | ok | generated (warn) |
| tracing `provider: langfuse` | ok | ok | fail | fail |

## A note on Pipecat's "gated (driver v1)" rows

Several rows show Pipecat as gated even though the platform supports the feature. Those are **driver maturity gates**: the schema and Pipecat both allow it, but the first driver has not emitted the lowering yet. They fail validation today and lift when the driver ships them. This is different from a `fail` on a managed target, which is a structural limit. See the [Pipecat target page](../targets/pipecat.md) for the current list.
