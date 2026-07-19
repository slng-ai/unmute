# How targets interpret your YAML

Your portable YAML defines one behavior contract. Each target maps that
contract to its own runtime, so you can reason about agents, tasks, and results
without authoring framework-specific code.

## Separate behavior from platform

`agent.yaml` defines the models and describes what must happen during a
conversation.

```yaml
models:
  routing_model:
    description: Fast greeting and routing
    provider: openai
    model: gpt-4o-mini
  front_desk:
    description: Warm and concise
    provider: slng
    model: "slng/deepgram/aura:2-en"
    voice: "aura-2-thalia-en"

agents:
  greeter:
    instructions: instructions.md
    model: routing_model
    voice: front_desk
    tools: [to_billing]
```

`targets.yaml` names the platform and its per-target plumbing, and overrides any
model that platform cannot run as defined.

```yaml
targets:
  livekit:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    models:
      listen: { provider: deepgram, model: nova-3 }
      turn:  { provider: livekit, model: turn-detector-mini }
      # LiveKit runs a different voice, so override just that entry:
      front_desk: { provider: elevenlabs, voice: cgSgspJ2msm6clMCkdW9 }
```

The model name is the join between the files: `agent.yaml` defines it, an agent
references it, and a target may override it. The platform, listen/turn plumbing,
and version remain target-specific.

## Follow the interpretation flow

Every target follows the same four-part contract, even when its runtime
mechanics differ.

1. Read the portable agents, tasks, controls, tools, and conversation outcomes.
2. Resolve every used model through the selected target (its override, or the agent.yaml definition).
3. Reject any behavior that the target cannot represent faithfully.
4. Map the accepted behavior to the target without dropping fields silently.

For LiveKit, repository parity tests compare the accepted capability table to
the emitted field set. A YAML field that passes the target rules must have a
LiveKit lowering.

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
      history: last_n
      max_messages: 8
      include_tool_calls: false
      variables: [verified]
```

LiveKit changes which agent owns the conversation. Another target can use a
different mechanism, but it must preserve the history and variable choices in
the YAML.

## Delegate a task and receive typed data

A task temporarily owns a focused piece of work. The delegate receives the
typed result and can assign fields into shared variables.

```yaml
tasks:
  verify_customer:
    instructions: tasks/verify_customer.md
    model: careful_model
    result:
      customer_id: string
      verified: boolean
    context:
      history: messages

controls:
  run_verification:
    kind: delegate
    task: verify_customer
    when: Verify the caller before discussing the account.
    assign:
      customer_id: result.customer_id
      verified: result.verified
```

LiveKit gives the task its own task object and, when `model` is present, its
own think model. The portable contract is simpler: the owner receives
the declared result fields and the task's conversation doesn't leak back.

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

On LiveKit, a `shared` group uses the framework's task-group behavior. An
`isolated` group runs each task with a fresh context because the native group
always shares context. Both forms preserve `merge: results`: only typed results
return to the owning agent.

## Expect target differences at explicit boundaries

Portability doesn't mean every target has the same capabilities. It means the
differences are attached to named YAML fields rather than hidden in generated
behavior.

Use [tags and gating](tags-and-gating.md) to understand the outcome labels,
then use the target or reference page for the exact field. The
[LiveKit YAML guide](../targets/livekit.md) collects the complete LiveKit
surface in one place.
