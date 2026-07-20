# Reference: the safe core

The **safe core** is the subset of the schema that passes validation on every
one of the five targets. Write inside it and the same package runs on LiveKit,
Pipecat, Vapi, ElevenLabs, and Deepgram, with warnings at most, never a failure.
The compiler's [safe-core regression fixture](../../../internal/testdata/safe_core/)
follows these rules exactly. This page is the authoritative list from
`SCHEMA.md` section 7.

## The rules

1. Any number of agents with `agent_transfer` between them (T0 + T2).
2. Every transfer context: `history: full`, `variables: all`.
3. Tools: `execution: webhook`, `interruption: provider_default`, `effect: returns_data`.
4. Human transfer: `mode: cold` only. Pipecat needs the Daily SIP transport; Deepgram needs a carrier in its target instance.
5. Models: hosted providers for `listen` and speak models (no `provider: local`). `turn` is a preference anyway.
6. If the agent speaks first, give it a fixed `greeting.text`. A model-written opening is conditional on ElevenLabs and generated-with-a-warning on Deepgram; a fixed line is the zero-warning choice.
7. Skip for now: single `tasks` (return to owner is unverified on Vapi) and `task_groups` with `then: return` (fails on Vapi). A `task_group` with `then: transfer` or `end` does pass on all five (with a warning on LiveKit that TaskGroup is experimental). Also skip `requires`, `thinking_audio`, warm transfer, `mcp` and `local` tools, and any history other than `full`. `fallback` passes everywhere when the chain stays within one provider on Vapi and the fallback models carry no settings beyond the id on ElevenLabs. `outbound: true` with `on_voicemail` passes everywhere but is generated with a warning on Deepgram, so keep it out if you want zero warnings.
8. Accept warnings: `minimum_words` on ElevenLabs, interruption tuning on Deepgram, turn model notes.

## Feature by feature

`ok` works, `warn` works with a warning, `fail` fails validation.

| Feature | LiveKit | Pipecat | Vapi | ElevenLabs | Deepgram |
|---|---|---|---|---|---|
| single agent (T0) | ok | ok | ok | ok | ok |
| agent_transfer, `full` + `all` | ok | ok | ok | ok | ok |
| fixed opening line (`greeting.text`) | ok | ok | ok | ok | ok |
| model-written opening (no `text`) | ok | ok | ok | conditional (workflow node) | generated (warn) |
| user speaks first (`speaks_first: user`) | ok | ok | ok | ok | warn |
| task | ok | ok | unverified | conditional | ok |
| task_group, `then: transfer\|end` | warn | ok | ok | ok | ok |
| task_group, `then: return` | warn | ok | fail | ok | ok |
| history `messages` / `last_n` / `reset` | ok | gated (driver v1) | ok | fail | ok |
| history `summary` | ok | gated (driver v1) | fail | fail | ok |
| `requires:` | ok | ok | fail | fail | ok |
| `fallback:` (think) | ok | gated (driver v1) | conditional | ok | ok |
| `fallback:` (listen) | ok | gated (driver v1) | fail | fail | fail |
| human_transfer cold | ok | Daily SIP only | ok | ok | carrier-conditional |
| human_transfer warm | native | ships, not emitted yet | Twilio only | ok | carrier-conditional |
| `thinking_audio` | ok | gated (driver v1) | fail | ok | fail |
| `provider: local` (listen/speak) | ok | ok | fail | fail | fail |
| webhook tools | ok | ok | ok | ok | ok |
| mcp tools | Python only | gated (driver v1) | ok | ok | fail |
| outbound + `on_voicemail` | ok | gated (driver v1) | ok | ok | generated (warn) |

## A note on Pipecat's "gated (driver v1)" rows

Several rows show Pipecat as gated even though the platform supports the feature. Those are **driver maturity gates**: the schema and Pipecat both allow it, but the first driver has not emitted the lowering yet. They fail validation today and lift when the driver ships them. This is different from a `fail` on a managed target, which is a structural limit. See the [Pipecat target page](../targets/pipecat.md) for the current list.
