# Data Model: Spoken Agent Handoffs

## Authoring model

`spec.Control` gains:

| Field | Type | Required | Meaning |
|---|---|---|---|
| `announce` | string | no | Exact short sentence spoken by the active agent before handoff |

Rules:

- Legal only when `kind: agent_transfer`.
- Omitted means silent handoff.
- Present values must contain non-whitespace text.
- Template tokens are rejected in this version.
- `requires` and all other transfer gates run before the cue.

## Resolved model

`ir.AgentTransfer` carries the resolved string unchanged as `Announce`. No
default is synthesized. Each target lowerer copies it into its existing transfer
view.

## Scaffold model

`scaffold.Handoff` carries `Announce string`. The TUI shows an optional
"Announcement" editor row, scaffold YAML emits it only when non-empty, and
maintain mode reads it back unchanged.

## Capability model

`target.FieldTransferAnnounce` represents
`controls.agent_transfer.announce`. It is emitted by LiveKit and Pipecat and
gated on targets with no shipped lowering.

## State and lifecycle

There is no new persistent state. LiveKit's entry-agent instance receives one
ephemeral `initial` boolean. Only startup constructs it with `true`; all
handoffs use the default `false`. While Pipecat speaks an announcement, the
source worker holds one ephemeral event and one started-speaking flag. Both are
cleared before target activation.
