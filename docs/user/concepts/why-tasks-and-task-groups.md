# Why tasks and task groups

You can build a whole agent with one set of instructions and a few tools. That
is the right starting point. Tasks and task groups are what you reach for when
a single agent stops being enough. This page explains what they give you and
when the trade is worth it.

For how to write them, see the [tasks](../learn/05-tasks.md) and
[task groups](../learn/06-task-groups.md) tutorials. For the exact per-target
behavior, see [how targets interpret your YAML](how-targets-run-your-agent.md).

## Start with one agent

A single agent holds the whole conversation. It has one prompt and one tool
set. That is simple to write, simple to test, and it keeps all the context in
one place. Use it until you hit a real wall.

Four walls tell you a single agent is no longer the right shape:

- **The prompt is too big.** You keep adding rules and the model starts doing
  its main job worse.
- **Phases need different tools.** The lookup phase and the payment phase want
  different tools or permissions, and mixing them invites mistakes.
- **A step needs several turns of its own.** You need to collect and check
  structured input (a name, an email, an address) over a few back-and-forth
  turns before moving on.
- **The caller wants to go back.** Someone says "actually, change my name" and
  you need to revisit an earlier step without losing the rest.

If none of these is true, stay with one agent. Every structure you add costs
latency and complexity.

## What a task gives you

A **task** is a small unit of work with one goal. It borrows the conversation,
does its one job, returns a **typed result**, then hands control back to the
agent that called it. In your YAML a task declares its own `instructions`, its
own `tools`, and a `result` shape.

That buys you four things:

| Without a task | With a task |
|---|---|
| One large prompt tries to do everything | Each task prompt covers one job |
| The answer is free text you read and guess at | The answer is a typed `result` you can act on |
| You copy the same collect-and-check logic into every agent | You write it once and delegate to it |
| You test the whole conversation at once | You test each step on its own |

The typed result is the heart of it. A task does not just talk; it returns
data. The owning agent can take that data and `assign` it into shared
variables, so the rest of the call can use it.

## What a task group adds

A **task group** is an ordered list of tasks that run front to back and share
context. It exists for the fourth wall: going back. If the caller corrects an
earlier answer, the group can return to that step and keep the rest. You get
that safe back-and-forth without building a state machine by hand.

A task group is a list, not a map. There are no branches, no loops, no routers.
Steps run in order, and `then` decides what happens after the last one: return
to the owner, transfer to another agent, or end the call. `merge: results`
means only the typed results come back, not the chatter from each step.

Reach for a task group when the steps have a fixed order and the caller might
need to fix an earlier one. If the steps are independent, or the order does not
matter, separate task delegates are simpler.

## The bigger gain in Unmute

There is a second gain that is specific to this tool. You describe the flow
**once**, in portable YAML, as `tasks` and `task_groups`. Unmute then compiles
it into each platform's own native building blocks. You never hand-write and
sync framework code for each target.

That is why the same task can run as a first-class task on one platform and as
a conversation step on another, from a single spec. The vocabulary in your YAML
stays the same; the generated code changes per target. See
[our take on orchestrators](our-take-on-orchestrators.md) for why the targets
differ at all.

## How the targets differ, in one line each

The full mapping, with every context option and current driver gate, lives in
[how targets interpret your YAML](how-targets-run-your-agent.md). The short
version:

- **LiveKit** has tasks built in. A task becomes an `AgentTask`, and a shared
  task group becomes a `TaskGroup`. It can even give a task its own think model.
- **Pipecat** has no task primitive, so Unmute lowers a task to a Flow step on
  the delegating worker, and a task group to a Flow chain. A per-task model is
  refused today.
- **Vapi and ElevenLabs** are managed platforms with no place to host generated
  logic, so some task and group options fail validation instead of running. See
  [tiers](tiers.md).

One naming note if you also read the platform docs: "task" is not a universal
word. In LiveKit a task is a conversational unit. In Pipecat the same idea is a
Flow node, and "task" there means something else. In Unmute a `task` always
means a delegate-and-return step, whichever platform runs it.

## When to use what

- **One agent and tools.** Simple flow, no distinct phases. The default.
- **A task delegate.** One focused step that must finish and hand back typed
  data (verify a caller, collect an email).
- **A task group.** Several steps in a fixed order where the caller might
  revisit an earlier one (a booking flow, an intake questionnaire).
- **An agent transfer.** A whole new phase with its own persona, tools, or
  permissions. See [controls](../reference/controls.md).

Full field rules are in the
[tasks and task_groups reference](../reference/tasks.md).
