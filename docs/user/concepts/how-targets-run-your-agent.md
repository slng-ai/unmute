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

LiveKit gives the task its own `AgentTask`. Pipecat runs the task as a Flow on
the delegating worker. Both return the declared fields without leaking the
task conversation into the owner's context. LiveKit can give a task its own
think model; Pipecat currently rejects a per-task `model` and uses the
delegating agent's model.

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

## Expect target differences at explicit boundaries

Portability doesn't mean every target has the same capabilities. It means the
differences are attached to named YAML fields rather than hidden in generated
behavior.

Use [tags and gating](tags-and-gating.md) to understand the outcome labels,
then use the [LiveKit guide](../targets/livekit.md) or
[Pipecat guide](../targets/pipecat.md) for the exact native lowering and current
driver gates.
