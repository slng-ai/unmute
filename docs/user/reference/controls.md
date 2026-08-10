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

Hands the conversation to another agent (tier T2). Works on all four targets in its basic form.

```yaml
controls:
  to_billing:
    kind: agent_transfer
    to: billing
    when: Caller asks about billing, an invoice, or a refund.
    requires:
      - verified
    context:
      history: full
      variables: all
```

### to

The agent that takes over.

Required: yes. Values: an agent name. Default: none. Targets: all four, core.

### requires

A machine-checked guard: the transfer is refused unless every listed variable is set. On a failed guard the model gets a refusal naming the unmet variables, and that behavior is part of the contract.

Required: no. Values: a list of variable names. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | generated | gated |
| Pipecat | generated | gated |
| Vapi | fails (no mechanism) | gated |
| Deepgram | generated | gated |

### context.history

How much of the conversation carries to the new agent. There is no default,
because providers disagree about their own defaults, so you must state it. See
the [LiveKit and Pipecat context map](../concepts/how-targets-run-your-agent.md#compare-context-strategies)
for the exact prompt and history boundaries.

Required: yes. Values: `full | messages | last_n | summary | reset`. Default: none.

| Value | LiveKit | Pipecat | Vapi | Deepgram |
|---|---|---|---|---|
| `full` | ok | ok | ok | ok |
| `messages` | ok | driver gate | ok | ok |
| `last_n` | ok | driver gate | ok | ok |
| `summary` | ok (generated) | driver gate | fails | ok (generated) |
| `reset` | ok | driver gate | ok | ok |

`reset` never promises a literally empty context (on LiveKit a handoff marker still lands in the new context). **On Pipecat the driver emits `history: full` only today**; the other values are a driver maturity gate.

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
| Deepgram | `all` or a subset list | gated |

`all` is the only value managed targets accept. Subset lists compile on code targets.

## kind: human_transfer

Puts the caller through to a person on a phone. See the [phone-calls learn page](../learn/07-phone-calls.md).

There are two shapes, and they are different machines rather than two settings of one. You pick a shape by writing a block named after it. Exactly one block is required.

```yaml
controls:
  send_to_billing:
    kind: human_transfer
    destination: billing_line
    when: The caller asks to be put through to the billing team.
    cold: {}

  escalate_to_supervisor:
    kind: human_transfer
    destination: supervisor_line
    when: The caller is upset and asks for a manager.
    warm:
      briefing: |
        Give the caller's name and the invoice they are disputing.
        Say their identity is already verified.
        Ask whether they can take the call.
      ring_timeout: 30s
      on_unavailable: return_to_caller
```

**Cold** is one call to the carrier. The caller's leg is rerouted, your agent drops off, and the person answers knowing nothing about the call.

**Cold with nothing to configure is written `cold: {}`.** The empty braces are how YAML says "this block, with defaults", the same spelling [tools](tools.md) use for `client: {}`.

**Warm** keeps your agent involved. The caller goes on hold with music, the agent rings the person on a second line, tells them what the call is about, then connects the two. If the person cannot take it, the agent comes back to the caller.

### destination

A symbolic name, resolved through the target instance's `destinations:` map. Both shapes need it and both mean it the same way, so it sits above the block.

The map's value is one of three things, told apart by shape, so there is no extra key to learn:

```yaml
destinations:
  billing_line: "+34910000001"              # an E.164 number
  overflow_desk: "sip:desk@example.com"     # a SIP URI
  supervisor_line: SUPERVISOR_PHONE_NUMBER  # an env var holding one of those
```

Use the env var form for a number that differs between staging and production, or one you would rather not commit. It lands in the generated `.env.example` and the required-env list, and is read at call time.

Either way the model never sees a phone number and cannot dial one. It picks the symbolic name and the target resolves it.

Required: yes. Values: a symbolic name. Default: none. Targets: all four, core (resolution). The transport that carries it gates per target; see the route table below.

### Which shapes work where

| Route | `cold:` | `warm:` |
|---|---|---|
| LiveKit SIP with Twilio, Telnyx, or Plivo | Emitted offline; provisional | Emitted offline; provisional |
| LiveKit Twilio Connector | No emitted adapter | No emitted adapter |
| Pipecat carrier WebSocket with Twilio, Telnyx, or Plivo | Carrier REST path emitted offline; provisional | Designed (a second streamed call bridged in software, Twilio first); not emitted yet |
| Pipecat Daily SIP | Platform capability only; not an emitted v1 telephony route | Not the planned route: the shared room makes hold music and a private briefing conflict |
| Vapi | native | needs the Twilio carrier (stable path) |
| Deepgram | carrier-conditional | carrier-conditional |

Check the [phone-call route matrix](../learn/07-phone-calls.md#choose-a-supported-carrier-route)
before picking either shape. Every emitted Pipecat and LiveKit carrier route
is still provisional today.

### ring_timeout

How long the person's phone rings before the agent gives up.

Required: no. Values: a duration (`30s`). Default: none written, so the platform default applies (LiveKit waits 30 seconds). Legal in both blocks.

### on_unavailable

What happens when the person does not take the call. One field covers every way that can happen: nobody answers within `ring_timeout`, the person declines, the line goes to voicemail, or the call fails to connect at all.

Required: no. Values: `return_to_caller | hangup`. Default: `return_to_caller`. Legal in both blocks.

With `return_to_caller` the agent picks the conversation back up and can explain, try another destination, or carry on helping. With `hangup` it says goodbye and ends the call.

### briefing

What the agent tells the person before connecting them. Plain text, so write it the way you would brief a colleague.

Required: no. Values: text. Default: none, and the target's own briefing wording applies. **Legal inside `warm:` only**, because there is nobody to brief on a cold transfer.

The conversation so far is always passed along with it, on every target that supports warm transfer. You do not need to ask for a summary, and you do not need to declare a model to write one. Use `briefing` to say what matters *beyond* the transcript: what to lead with, what the person needs to decide, what has already been verified.

| Target | What happens | Tag |
|---|---|---|
| LiveKit SIP with Twilio, Telnyx, or Plivo | Added on top of the transcript summary, emitted offline on a provisional route | provisional |
| Pipecat carrier WebSocket (Twilio) | Generated briefing on the person's own media socket, once the driver task lands | gated |
| Vapi | Mapped onto the provider's own transfer plan | gated |
| Deepgram | fails | gated |

### What warm transfer leaves behind

The two orchestrators finish a warm transfer differently, and it is worth knowing which one you are on.

On **LiveKit** the agent moves the person into the caller's room and shuts itself down, so the caller and the person carry on alone.

On **Pipecat** both phone calls end in WebSockets on the bot process, so there is nothing to leave: the bot stays on the call, silent, copying audio between the two sockets until someone hangs up.

The caller cannot tell the difference: either way they stop hearing the agent and start hearing the person. It matters if you are reading logs, counting sessions, or [sizing capacity](channels-and-capacity.md), because a Pipecat warm transfer keeps its session open for the whole conversation.
