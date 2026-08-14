# subagents

Two agents that hand the caller back and forth. `booking_desk` takes new
appointments; `appointment_manager` reschedules and cancels. Each has its own
prompt, and a control moves the conversation from one to the other.

```yaml
controls:
  to_appointment_manager:
    kind: agent_transfer
    to: appointment_manager
    when: The caller wants to reschedule or cancel an existing appointment.
    context:
      history: full
      variables: all
```

The transfer control sits in the agent's tool list like any other tool, which
is how the model triggers it:

```yaml
agents:
  booking_desk:
    instructions: instructions.md
    tools:
      - lookup_customer
      - create_customer
      - check_availability
      - book_appointment
      - to_appointment_manager
```

`entry_agent: booking_desk` decides who answers. Both code targets are
declared, browser audio only.

## Run it

```sh
bin/unmute validate examples/subagents
```

```sh
bin/unmute compile examples/subagents
```

```sh
cp examples/subagents/build/pipecat/.env.example examples/subagents/.env
```

```sh
bin/unmute dev examples/subagents --target pipecat
```

## What to look at

**A transfer hands over; it does not return.** Unlike a task, nothing comes
back. The second agent owns the call from that point, which is why this package
declares a control in each direction: `to_appointment_manager` and
`to_booking_desk`.

**The tool lists differ on purpose.** `appointment_manager` is the only agent
with `cancel_appointment`. A caller cannot be talked into a cancellation by the
booking agent, because that agent has no way to perform one.

**`context:` decides what the new agent knows.** `history: full` carries the
conversation, `variables: all` carries the values collected so far, so the
caller is not asked for their phone number twice.

**Two prompts, two jobs.** [instructions.md](instructions.md) is the booking
desk. [agents/appointment-manager.md](agents/appointment-manager.md) is the
other one. Each is shorter and more specific than the single prompt in
[simple-prompt](../simple-prompt/README.md).

## Compare with

| Package | What changes |
|---|---|
| [simple-prompt](../simple-prompt/README.md) | One agent, one prompt, every tool. |
| [multi-task](../multi-task/README.md) | Delegation with a typed result, and control comes back. |
| [task-groups](../task-groups/README.md) | An ordered sequence of tasks with shared context. |

Reference pages: [handoffs](../../docs-site/build/orchestration/handoffs.mdx),
[controls](../../docs-site/reference/agent-yaml.mdx).
