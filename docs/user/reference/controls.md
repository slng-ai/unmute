# Reference: controls

A **control** is an action the model can invoke besides a plain tool: delegate work, transfer to another agent, or transfer to a human. Controls share one name space with [tools](tools.md), so an agent gets a control by listing its name in the agent's `tools:` list.

## Common fields

| Field | Required | Values | Notes |
|---|---|---|---|
| `kind` | yes | `delegate \| agent_transfer \| human_transfer` | which kind of control |
| `when` | no | text | the trigger the model reads; lowered into the tool or edge description |

The rest of the fields depend on `kind`.

## kind: delegate

Hands work to a [task or task group](tasks.md).

```yaml
controls:
  run_collect:
    kind: delegate
    task: collect
    when: Collect the caller's account details.
    assign:
      verified: result.verified_flag
```

| Field | Required | Notes |
|---|---|---|
| `task` or `group` | exactly one | name of a task or a task group |
| `assign` | no, task only | map of `variable_name: result.<field>` |

`assign` maps a task's result field into a variable. The field must exist in the task's `result` and match the variable's type (an enum field assigns into a `string` variable). A **group** delegate has no `assign`; step results travel through the group's shared context instead.

Whether control comes back is decided by the target: a single task always returns; a group returns only when its `then:` is `return`. With `then: transfer` or `then: end` the delegate never returns, and the generated tool description says so. Tag: inherits T1 (gated); see [tasks](tasks.md) for the per-target table.

## kind: agent_transfer

Hands the conversation to another agent (tier T2). Works on all five targets in its basic form.

```yaml
controls:
  to_billing:
    kind: agent_transfer
    to: billing
    when: Caller asks about billing, an invoice, or a refund.
    requires: [verified]
    context:
      history: full
      variables: all
```

### to

The agent that takes over.

Required: yes. Values: an agent name. Default: none. Targets: all five, core.

### requires

A machine-checked guard: the transfer is refused unless every listed variable is set. On a failed guard the model gets a refusal naming the unmet variables, and that behavior is part of the contract.

Required: no. Values: a list of variable names. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | generated | gated |
| Pipecat | generated | gated |
| Vapi | fails (no mechanism) | gated |
| ElevenLabs | fails (no mechanism) | gated |
| Deepgram | generated | gated |

### context.history

How much of the conversation carries to the new agent. There is no default, because providers disagree about their own defaults, so you must state it.

Required: yes. Values: `full | messages | last_n | summary | reset`. Default: none.

| Value | LiveKit | Pipecat | Vapi | ElevenLabs | Deepgram |
|---|---|---|---|---|---|
| `full` | ok | ok | ok | ok | ok |
| `messages` | ok | ok | ok | fails | ok |
| `last_n` | ok | ok | ok | fails | ok |
| `summary` | ok (generated) | ok (generated) | fails | fails | ok (generated) |
| `reset` | ok | ok | ok | fails | ok |

ElevenLabs always keeps the full transcript, so only `full` works there. `reset` never promises a literally empty context (on LiveKit a handoff marker still lands in the new context). **On Pipecat the driver emits `history: full` only today**; the other values are a driver maturity gate.

### context.max_messages

Required: conditional (iff `history: last_n`). Values: int. Default: none. Illegal with any other history value.

### context.summarizer

The `models.think` entry used to write the summary. It is resolved and counted
by sizing like any other used model.

Required: conditional when `history: summary` is generated. Values: a think
model name. Default: none.

### context.include_tool_calls

Whether prior tool calls carry across.

Required: no. Values: bool. Default: `true`. Tag: gated. `false` works on code targets only. On Pipecat, a non-default value is a driver maturity gate today.

### context.variables

Which shared variables carry across.

Required: yes. Values: `all` or a list of names. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | `all` or a subset list | gated |
| Pipecat | `all` (a subset list is a driver maturity gate today) | gated |
| Vapi | `all` only | gated |
| ElevenLabs | `all` only | gated |
| Deepgram | `all` or a subset list | gated |

`all` is the only value managed targets accept. Subset lists compile on code targets.

## kind: human_transfer

Transfers the caller to a person on a phone. See the [phone-calls learn page](../learn/07-phone-calls.md).

```yaml
controls:
  to_human:
    kind: human_transfer
    destination: billing_line
    mode: cold
```

### destination

A symbolic name, resolved through the target instance's `destinations:` map to a phone number or SIP address.

Required: yes. Values: a symbolic name. Default: none. Targets: all five, core (resolution). The transport that carries it gates per target; see `mode`.

### mode

Required: yes. Values: `cold | warm`. Default: none.

| Route | Cold | Warm |
|---|---|---|
| LiveKit SIP with Twilio, Telnyx, or Plivo | Emitted offline; provisional | Emitted offline; provisional |
| LiveKit Twilio Connector | No emitted adapter | No emitted adapter |
| Pipecat carrier WebSocket with Twilio, Telnyx, or Plivo | Carrier REST path emitted offline; provisional | Not emitted |
| Pipecat Daily SIP | Platform capability only; not an emitted v1 telephony route | Not emitted |
| Vapi | native | needs the Twilio carrier (stable path) |
| ElevenLabs | native | supported |
| Deepgram | carrier-conditional | carrier-conditional |

Cold transfers the caller and the agent drops off. Warm keeps the agent on to
brief the human first. See the
[phone-call route matrix](../learn/07-phone-calls.md#choose-a-supported-carrier-route)
before selecting either mode; all emitted Pipecat and LiveKit carrier routes
remain provisional today.

### briefing

What the agent tells the human on a warm transfer.

Required: conditional (warm only). Values: `summary | message | wait`. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit SIP with Twilio, Telnyx, or Plivo | `summary`, emitted offline on a provisional route | provisional |
| Pipecat | fails (warm not emitted yet) | gated |
| Vapi | all three | gated |
| ElevenLabs | `message` | gated |
| Deepgram | fails | gated |
