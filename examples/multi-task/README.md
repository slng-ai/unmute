# multi-task

One agent, two tasks. The same salon workflow as
[simple-prompt](../simple-prompt/README.md), with the customer record and the
appointment request pulled out into focused steps the agent delegates to.

A task is delegate and return: the agent hands the conversation to the task,
the task runs its own prompt with its own tools, and control comes back with a
typed result.

```yaml
tasks:
  customer_record:
    instructions: tasks/customer-record.md
    tools:
      - lookup_customer
      - create_customer
    result:
      customer_id: string
      customer_name: string
      record_status:
        enum:
          - existing
          - created
          - failed
      summary: string
    context:
      history: full

controls:
  check_customer:
    kind: delegate
    task: customer_record
    when: Identify the caller before handling an appointment request.
    assign:
      customer_id: result.customer_id
      customer_name: result.customer_name
```

The agent's own tool list is the two controls, not the five domain tools:

```yaml
agents:
  appointment_desk:
    tools:
      - check_customer
      - manage_appointment
```

Both code targets are declared, browser audio only.

## Run it

Same as every structural example. Docker running, the keys from the generated
`.env.example` (`OPENAI_API_KEY`, `SLNG_API_KEY`, and the three Langfuse
values, because this package sets `tracing: langfuse`).

```sh
bin/unmute validate examples/multi-task
```

```sh
bin/unmute compile examples/multi-task
```

```sh
cp examples/multi-task/build/pipecat/.env.example examples/multi-task/.env
```

```sh
bin/unmute dev examples/multi-task --target pipecat
```

`--target livekit` runs the same package on the other driver.

## What to look at

**The result is typed, and it goes somewhere.** `result:` declares the four
fields the task returns. `assign:` copies two of them into the package
variables `customer_id` and `customer_name`, so the rest of the call can use
them without asking again.

**Each task only sees its own tools.** `customer_record` has the two customer
tools. `appointment_request` has the three appointment tools. The model cannot
book before identification because the booking tool is not on the table yet.

**The parent prompt got shorter.** [instructions.md](instructions.md) now
routes; the workflow detail moved into
[tasks/customer-record.md](tasks/customer-record.md) and
[tasks/appointment-request.md](tasks/appointment-request.md).

**`context: history: full`** means the task reads the conversation so far. That
is the default here because the caller has usually already said something the
task needs.

## Compare with

| Package | What changes |
|---|---|
| [simple-prompt](../simple-prompt/README.md) | One prompt owns everything. |
| [task-groups](../task-groups/README.md) | The tasks run as one ordered group with shared context. |
| [subagents](../subagents/README.md) | Two agents that hand the caller over instead of delegating. |

Reference pages: [tasks](../../docs/user/reference/tasks.md),
[controls](../../docs/user/reference/controls.md).
