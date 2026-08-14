# task-groups

One agent, three tasks, run as one ordered group. Where
[multi-task](../multi-task/README.md) delegates to a task at a time, this
package delegates to a sequence: identify the customer, select the
appointment, finalize it.

```yaml
task_groups:
  appointment_flow:
    steps:
      - identify_customer
      - select_appointment
      - finalize_appointment
    context_scope: shared
    then: return
    merge: results

controls:
  manage_appointment:
    kind: delegate
    group: appointment_flow
    when: The caller wants to book, reschedule, or cancel an appointment.
```

The agent has exactly one control and no domain tools of its own:

```yaml
agents:
  appointment_desk:
    tools:
      - manage_appointment
```

Both code targets are declared, browser audio only.

## Run it

```sh
bin/unmute validate examples/task-groups
```

```
✓ livekit (livekit)
✓ pipecat (pipecat)

Warnings:
  livekit: LiveKit turn placement is a preference
  livekit: LiveKit TaskGroup is experimental
```

The second warning is the one to read. Task groups lower onto a LiveKit
primitive that upstream marks experimental. The package still compiles and
runs on LiveKit; the warning is telling you what you are standing on. Warnings
go to standard error and the command still exits 0.

```sh
bin/unmute compile examples/task-groups
```

```sh
cp examples/task-groups/build/pipecat/.env.example examples/task-groups/.env
```

```sh
bin/unmute dev examples/task-groups --target pipecat
```

## What to look at

**`context_scope: shared`** is the difference from two separate delegations.
The three steps see one running context, so the service the caller named in
step one is still there in step two without being passed by hand.

**`merge: results`** collects each step's typed result into one result for the
group, and `then: return` hands control back to the agent when the last step
finishes.

**The order is the guarantee.** `select_appointment` cannot run before
`identify_customer`, because the group declares the sequence. That is a
stronger promise than a prompt asking the model to do things in order.

**Each step still owns its tools.**
[tasks/identify-customer.md](tasks/identify-customer.md) has the customer
tools, [tasks/select-appointment.md](tasks/select-appointment.md) has
availability, and [tasks/finalize-appointment.md](tasks/finalize-appointment.md)
has booking and cancellation.

## Compare with

| Package | What changes |
|---|---|
| [simple-prompt](../simple-prompt/README.md) | One prompt owns everything. |
| [multi-task](../multi-task/README.md) | Two independent tasks, delegated one at a time. |
| [subagents](../subagents/README.md) | Two agents that hand the caller over. |

Reference pages: [tasks](../../docs/user/reference/tasks.md),
[controls](../../docs/user/reference/controls.md).
