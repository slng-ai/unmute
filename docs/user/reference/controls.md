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

There are two shapes, and they are different machines rather than two settings of one. You pick a shape by writing a block named after it. Exactly one block is required, and the block carries every setting of the transfer.

```yaml
controls:
  send_to_billing:
    kind: human_transfer
    when: The caller asks to be put through to the billing team.
    cold:
      destination: billing_line

  escalate_to_supervisor:
    kind: human_transfer
    when: The caller is upset and asks for a manager.
    warm:
      destination: supervisor_line
      briefing: |
        Give the caller's name and the invoice they are disputing.
        Say their identity is already verified.
        Ask whether they can take the call.
      ring_timeout: 30s
      on_unavailable: return_to_caller
```

**Cold** is one call to the carrier. The caller's leg is rerouted, your agent drops off, and the person answers knowing nothing about the call.

**Warm** keeps your agent involved. The caller goes on hold with music, the agent rings the person on a second line, tells them what the call is about, then connects the two. If the person cannot take it, the agent comes back to the caller.

Above the block you say what the tool is (`kind`, `when`). Inside it you say what the transfer does. A `cold:` block therefore always has at least a `destination:` under it.

### destination

A symbolic name, resolved through the top-level `destinations:` map in `agent.yaml`. Required in both blocks.

The map's value is the `UPPER_SNAKE` name of an environment variable holding an E.164 number or a `sip:` URI:

```yaml
# agent.yaml
destinations:
  billing_line: BILLING_PHONE_NUMBER
  overflow_desk: OVERFLOW_DESK_URI
  supervisor_line: SUPERVISOR_PHONE_NUMBER
```

The name lands in the generated `.env.example` and the required-env list, is read at call time, and belongs in [`secrets`](secrets.md) too. A literal number or URI is refused: `agent.yaml` is the portable half of a package, a number is a deployment fact, and it is exactly the value that differs between staging and production. (Until SCHEMA N40 the map sat on each target and accepted literals; both are gone.)

The model never sees a phone number and cannot dial one. It picks the symbolic name and the compiler resolves it.

Required: yes. Values: a symbolic name. Default: none. Targets: all four, core (resolution). The transport that carries it gates per target; see the route table below.

### Which shapes work where

One rule: a transfer compiles only on a route where the platform ships the
primitive. The full map with sources and the test walkthroughs is
[TRANSFERS.md](../../TRANSFERS.md).

| Route | `cold:` | `warm:` |
|---|---|---|
| LiveKit SIP with Twilio, Telnyx, or Plivo | `TransferSIPParticipant` (SIP REFER); provisional | `WarmTransferTask`; provisional |
| Pipecat Daily, Daily's number (`transport: daily-sip`) | `sip_call_transfer`; provisional | Not emitted yet |
| Pipecat Daily, your own number (`transport: daily-sip` + `carrier:`) | `sip_call_transfer`, destination composed at your trunk's termination address; provisional | Not emitted yet |
| LiveKit Twilio Connector | Not supported | Not supported |
| Pipecat carrier WebSocket (any carrier) | Not supported | Not supported |
| Vapi | native | needs the Twilio carrier (stable path) |
| Deepgram | carrier-conditional | carrier-conditional |

The two phrasings mean different things, and the difference is the whole point
(checked 2026-08-13). **"Not supported"** means the platform ships no transfer
control on that route, so there is nothing to build against; those rows are firm,
not pending. Every transfer design this project once built on them meant owning the
call's audio path, and that work is deleted. **"Not emitted yet"** means the
platform does ship it and this project has not written it: Daily documents a warm
pattern on both Daily forms and it is deliberate work rather than a default.
Validation refuses a transfer in either case and names the routes that work.

**Warm compiles on LiveKit SIP only, today.** A Pipecat warm package fails
validation pointing at `(livekit, sip)`.

Worth being exact about why, because the two reasons are different (checked
2026-08-12). On Pipecat's **carrier websocket** routes the platform has no
call-transfer control at all, so warm cannot be built there. On Pipecat's
**Daily** route the platform does document warm; we have not built it yet,
because the pattern puts the generated bot in charge of the call's audio and
that is a deliberate piece of work rather than a default. Tracked as feature
005. The `warm:` block you would write for it already exists and does not
change.

### ring_timeout

How long the person's phone rings before the agent gives up.

Required: no. Values: a duration (`30s`). Default: none written, so the platform default applies (LiveKit waits 30 seconds; the Pipecat Twilio route uses Twilio's own 60 second dial timeout). Legal in both blocks.

**It bounds ringing only.** On a LiveKit warm transfer, once the person picks up nothing bounds the consultation, and the caller hears hold music for the whole of it. The generated agent is told to decline on the person's behalf when they go quiet or never decide, which is a mitigation rather than a guarantee. Why there is no bound, and what a real one would cost, is in [TRANSFERS.md](../../TRANSFERS.md) (2026-08-12, SCHEMA N35).

### on_unavailable

What happens when the person does not take the call. One field covers every way that can happen: nobody answers within `ring_timeout`, the person declines, the line goes to voicemail, or the call fails to connect at all.

Required: no. Values: `return_to_caller | hangup`. Default: `return_to_caller`. Legal in both blocks.

With `return_to_caller` the agent picks the conversation back up and can explain, try another destination, or carry on helping. With `hangup` it says goodbye and ends the call.

### briefing

What the agent tells the person before connecting them. Plain text, so write it the way you would brief a colleague.

Required: no. Values: text. Default: none. **Legal inside `warm:` only**, because there is nobody to brief on a cold transfer.

The conversation so far is always passed along with it, on every target that supports warm transfer. You do not need to ask for a summary, and you do not need to declare a model to write one. Use `briefing` to say what matters *beyond* the transcript: what to lead with, what the person needs to decide, what has already been verified.

Omitting it does **not** leave the person unbriefed on LiveKit. Since 2026-08-12 (SCHEMA N35) the generated agent carries its own prompt saying to open with the handover and never with a greeting, and your `briefing` text lands on top of that. What the person actually hears, and the log lines a transfer leaves, are in [TRANSFERS.md](../../TRANSFERS.md).

| Target | What happens | Tag |
|---|---|---|
| LiveKit SIP with Twilio, Telnyx, or Plivo | Passed to `WarmTransferTask` on top of the transcript and the generated agent's own briefing prompt | provisional |
| Pipecat | fails; there is no warm transfer to brief | gated |
| Vapi | Mapped onto the provider's own transfer plan | gated |
| Deepgram | fails | gated |

### What warm transfer leaves behind

`WarmTransferTask` moves the person into the caller's room and shuts the agent's session down, so the caller and the person carry on alone. The generated code passes `delete_room_on_close=False` so the room outlives the agent that made it.
