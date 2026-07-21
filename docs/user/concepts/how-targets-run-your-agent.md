# How targets interpret your YAML

Your portable YAML defines one behavior contract. Each target maps that
contract to its own runtime, so you can reason about agents, tasks, and results
without authoring framework-specific code.

## Separate behavior from platform

`agent.yaml` defines the models and describes what must happen during a
conversation.

```yaml
models:
  think:
    routing_model:
      description: Fast greeting and routing
      provider: openai
      model: gpt-4o-mini
  speak:
    front_desk:
      description: Warm and concise
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-thalia-en"
  listen:
    transcriber: { provider: deepgram, model: nova-3 }
  turn:
    detector: { provider: local, model: silero }

agents:
  greeter:
    instructions: instructions.md
    model: routing_model
    voice: front_desk
    tools: [to_billing]
```

`targets.yaml` names the platform and overrides any model that platform cannot
run as defined.

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"

  livekit:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    models:
      # LiveKit uses its own turn detector, so override just that entry.
      detector: { provider: livekit, model: turn-detector-mini }
```

The model name is the join between the files: `agent.yaml` defines it, an agent
or a top-level selector references it, and a target may override it. The
platform and version remain target-specific. Pipecat uses the local turn model
as defined; LiveKit replaces that entry without changing the portable agent.

## Follow the interpretation flow

Every target follows the same four-part contract, even when its runtime
mechanics differ.

1. Read the portable agents, tasks, controls, tools, and conversation outcomes.
2. Resolve every used model through the selected target (its override, or the agent.yaml definition).
3. Reject any behavior that the target cannot represent faithfully.
4. Map the accepted behavior to the target without dropping fields silently.

For both shipped code drivers, repository parity tests compare the accepted
capability table to the emitted field set. A YAML field that passes the target
rules must have a LiveKit or Pipecat lowering.

## Transfer between agents

An agent transfer names the next agent, the trigger, the required state, and
the context that crosses the boundary.

```yaml
variables:
  verified: { type: boolean, default: false }

controls:
  to_billing:
    kind: agent_transfer
    to: billing
    when: The caller asks about billing or a refund.
    requires: [verified]
    context:
      history: full
      variables: all
```

LiveKit hands the session to another `Agent`. Pipecat activates another worker
and deactivates the current one. Both preserve the portable `full` history and
`all` variables contract above. LiveKit also emits finer history and variable
shaping; the Pipecat driver currently rejects those choices instead of
dropping them.

## Delegate a task and receive typed data

A task temporarily owns a focused piece of work. The delegate receives the
typed result and can assign fields into shared variables.

```yaml
tasks:
  verify_customer:
    instructions: tasks/verify_customer.md
    result:
      customer_id: string
      verified: boolean
    context:
      history: full

controls:
  run_verification:
    kind: delegate
    task: verify_customer
    when: Verify the caller before discussing the account.
    assign:
      customer_id: result.customer_id
      verified: result.verified
```

LiveKit gives the task its own `AgentTask` and excludes the delegating agent's
instructions from the context passed into it. Pipecat runs the task as a Flow
on the delegating worker. Each Flow node replaces the worker's current system
instruction with the task prompt through `role_message`, so the task call does
not receive the agent prompt. LiveKit can give a task its own think model;
Pipecat currently rejects a per-task `model` and uses the delegating agent's
model.

## Run tasks in order

A task group is a fixed sequence, not a graph. The scope controls what each
step sees, and `then` controls what happens after the final step.

```yaml
task_groups:
  booking_flow:
    steps: [find_slot, confirm_booking]
    context_scope: isolated
    then: return
    merge: results
```

On LiveKit, a `shared` group uses `TaskGroup`, while an `isolated` group uses a
generated sequence of standalone `AgentTask` objects. On Pipecat, both scopes
use a Flow chain with different context strategies. Every form preserves
`merge: results`: only typed results return to the owning agent.

## Compare context strategies

Context settings control three separate boundaries: what enters a task or
agent, which instructions the model sees, and what returns to the owner. The
tables below show where each target uses a different native mechanism or
currently produces a different boundary.

### Enter a single task

A task always gets its own instructions and tools. Its `context.history` value
selects the earlier conversation that accompanies those instructions.
Both generated targets add the short step instruction `Begin this step.`; they
don't add the parent prompt.

| `context.history` | LiveKit task input | Pipecat task input today |
|---|---|---|
| `full` | Copies the parent conversation and tool history, but excludes the parent agent's instructions. The task model sees the task prompt as its system prompt. | Uses Pipecat Flow `APPEND` for context messages and the task prompt as `role_message`. The model sees the running conversation with the task system instruction, not the agent system instruction. |
| `messages` | Keeps user and assistant messages only. It excludes prior instructions and tool calls. | Driver gate. The compile fails because this shaping isn't emitted yet. |
| `last_n` | Keeps the newest `max_messages` context items after excluding the parent instructions. Function-call pairs stay intact. | Driver gate. The compile fails because this shaping isn't emitted yet. |
| `summary` | Runs the named `summarizer` think model, then starts the task with the summary and the task prompt. | Driver gate. The compile fails because this shaping isn't emitted yet. |
| `reset` | Starts with the task prompt and no parent conversation. | Driver gate for a single task. Pipecat `RESET` is available for isolated task-group steps. |

Set `include_tool_calls: false` to remove function calls and outputs from a
LiveKit `full` or `last_n` context. The Pipecat driver rejects this option until
it emits the corresponding filter.

<!-- prettier-ignore -->
> [!NOTE]
> Pipecat `APPEND` preserves context messages, not the previous
> `role_message`. The task role replaces the agent system instruction.

On return, Pipecat restores the owner's system instruction, pre-task messages,
and tools, then adds the typed task result as a developer message. LiveKit
keeps the parent prompt out of the task input, but the current standalone
`AgentTask` completion path merges the task's user and assistant turns back
into the owner. It excludes the task instructions and, by default, task tool
calls. This return merge is separate from the task-entry prompt boundary.

### Enter task-group steps

The group-level `context_scope` overrides each member task's own
`context.history` setting.

| `context_scope` | LiveKit | Pipecat |
|---|---|---|
| `shared` | Uses `TaskGroup` with the owner instructions excluded. Steps share the group's conversation. Unmute disables LiveKit's automatic group summary call. | Uses Flow `APPEND`. Each step retains the owner context and earlier step context, then replaces the previous role with its own task prompt. |
| `isolated` | Runs standalone `AgentTask` objects with no owner or earlier-step conversation. Each step sees only its own prompt. | Uses Flow `RESET` for every step. Each step sees only its current task role, task message, and tools. |

For `then: return`, both targets restore the owner's pre-group context and
return only the typed step results. LiveKit restores a copied `ChatContext`.
Pipecat restores the agent system instruction plus its message-and-tool
snapshot, then adds the results as a developer message.

### Transfer between agents

Agent transfers use the same five `history` values, but they start another
agent instead of a temporary task.

| `context.history` | LiveKit transfer | Pipecat transfer today |
|---|---|---|
| `full` | Copies the full conversation without the source agent's instructions. The destination uses its own instructions. | Uses the native worker handoff with the running context. This is the only history mode the current driver emits. |
| `messages` | Keeps user and assistant messages only. | Driver gate. |
| `last_n` | Keeps the newest `max_messages` context items. | Driver gate. |
| `summary` | Runs the named summarizer and gives the destination its result. | Driver gate. |
| `reset` | Starts the destination without prior conversation; a handoff marker can still be present. | Driver gate. |

LiveKit also emits `include_tool_calls: false` and a subset list for
`context.variables`. Pipecat currently accepts tool calls and
`context.variables: all` only. See the complete field rules in the
[controls reference](../reference/controls.md) and the
[tasks reference](../reference/tasks.md).

### Choose the smallest useful context

Use the narrowest history that still contains the facts the receiver needs.

- Use `reset` when the task is self-contained.
- Use `messages` when dialogue matters but tool history doesn't.
- Use `last_n` when only recent turns matter.
- Use `summary` for long conversations when an extra summarizer call costs less
  than repeatedly sending the full transcript.
- Use `full` when the receiver needs the complete conversation or tool history.

On Pipecat today, single tasks and transfers require `full`. Use an isolated
task group only when its steps genuinely don't need the earlier conversation;
`RESET` reduces tokens by discarding that context, not by compressing it.

## Expect target differences at explicit boundaries

Portability doesn't mean every target has the same capabilities. It means the
differences are attached to named YAML fields rather than hidden in generated
behavior.

Use [tags and gating](tags-and-gating.md) to understand the outcome labels,
then use the [LiveKit guide](../targets/livekit.md) or
[Pipecat guide](../targets/pipecat.md) for the exact native lowering and current
driver gates.
