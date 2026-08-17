# Orchestration

Four ways to organize an agent. They are not ranked, and each one costs
something. This is the area assistants get wrong most often, and the mistake is
almost never the YAML. It is deciding what context crosses a boundary without
saying so.

## Start with more tools, not another agent

One agent with a clear prompt and the right tools goes a long way. Before adding
a second agent, ask whether the problem is the prompt or just the tool list.
Every split costs clarity somewhere, so make each one buy something you can
name.

## The symptom decides the shape

| What the user is seeing | The shape that fixes it |
|---|---|
| the prompt keeps growing and starts contradicting itself | split it: tasks if the parts serve one caller goal, a handoff if they are separate roles |
| the model does things out of order | a task group. The order is declared, not requested |
| the model calls a tool it should not have yet | move the tool. Lists are per agent and per task, so a tool the current step does not hold cannot be called at all |
| you need a value out of a step and want to keep it | a task with `result:`, delegated with `assign:` |
| two phases need different tools or different permissions | a handoff |
| the caller needs a person | none of these. That is a human transfer, and what it can do depends on the phone route. See `transfers.md` |

A prompt that says "always identify the caller first" is a request. A task group
is a guarantee. That is the difference you are buying.

## What each shape costs

| Shape | Control | Context | Correcting a step | The cost |
|---|---|---|---|---|
| one agent and tools | stays with the agent for the whole call | the whole conversation | ask again | the prompt carries everything, and grows |
| task | returns when the task finishes | what `context:` gives the task | delegate again | one more prompt and one more tool list to keep straight |
| task group | returns when the group finishes | shared across the steps, or not | steps can be revisited inside the group | an order you have to be sure about |
| handoff | leaves for good | only what `context:` carries over | another handoff back | nothing returns, so there is no result and no automatic way back |

These are not exclusive. One agent can delegate a task in one phase and hand off
in another.

## Rung 1: one agent

```yaml agent.yaml
entry_agent: appointment_desk

agents:
  appointment_desk:
    instructions: instructions.md
    model: reasoning
    voice: voice
    tools:
      - check_availability
      - book_appointment
      - end_call
```

Context decision: none. The agent sees the whole conversation. Say that.

## Rung 2: a handoff to a second agent

```yaml agent.yaml
entry_agent: booking_desk

agents:
  booking_desk:
    instructions: instructions.md
    model: reasoning
    voice: voice
    tools:
      - to_appointment_manager

  appointment_manager:
    instructions: agents/appointment-manager.md
    model: reasoning
    voice: voice
    tools:
      - to_booking_desk
      - cancel_appointment

controls:
  to_appointment_manager:
    kind: agent_transfer
    to: appointment_manager
    when: The caller wants to reschedule or cancel an existing appointment.
    announce: "I’m connecting you with our appointment manager now."
    context:
      history: full
      variables: all

  to_booking_desk:
    kind: agent_transfer
    to: booking_desk
    when: The caller wants to make a new appointment instead.
    announce: "I’m connecting you back to the booking desk for your new appointment."
    context:
      history: full
      variables: all
```

`entry_agent` decides who answers. Each agent has its own prompt file, its own
tool list, and the call for good once it arrives.

`when` is the condition the model reads. Write it as the situation, not as an
instruction to call a tool.

`announce` is optional. Give it the exact short sentence the caller should hear.
The active agent speaks it once and finishes before the next agent takes over.
Leave it out for a silent handoff. Do not duplicate the cue in the prompt; the
control owns its order.

**A handoff does not come back.** Nothing returns and no result is passed back.
That is why the example declares a control in each direction: coming back is
another handoff, not a return. If you want a step that runs and hands control
back with an answer, that is a task. A handoff back to the entry agent continues
the call and never repeats the call-start greeting.

On LiveKit, a receiving agent cannot fire another agent transfer during its
automatic entry turn. Ordinary tools still work, and transfer tools return on
the next caller turn. Do not add prompt guards for reciprocal handoff loops;
the generated driver already gates that boundary.

The tool lists are the guardrail. Only `appointment_manager` holds
`cancel_appointment`, so no caller can talk the booking agent into a
cancellation.

**Context decision, and you must state it:**

| Field | What it does |
|---|---|
| `history: full` | the new agent sees the conversation so far |
| `variables: all` | the values collected so far travel with the caller |

Leave them out and the caller gets asked for their phone number twice. Choose on
purpose, and tell the user what you chose.

## Rung 3: a delegated task

```yaml agent.yaml
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

`result:` is the contract. The task must come back with those fields, and
`record_status` can only be one of three values. That shape is what makes a task
different from a longer prompt. Its internal turns stay out of the agent's
context, while the completed delegate call stays there so the same request does
not run twice.

`assign:` copies fields out of the result into package variables, so the rest of
the call uses them without asking again. Declare each of those variables at the
top level.

**While a task runs, the caller is talking to the task**: its prompt, and only
its tools. The agent's own tools are not offered again until the task returns.
That is the useful half of delegation and also the part to plan for, because a
task that needs something the agent could do has to hold that tool itself.

**Context decision:** `context:` on the task says what it sees at the start.

| Field | Values | Notes |
|---|---|---|
| `history` | `full`, `messages`, `last_n`, `summary`, `reset` | required |
| `max_messages` | a positive number | legal with `last_n` only |
| `summarizer` | a model entry name | legal with `summary` only |
| `include_tool_calls` | `true` or `false` | whether tool calls travel too |
| `variables` | `all` or a list of names | transfer controls only, not tasks |

`history: full` is usually right, because the caller has already said something
the task needs. `history: reset` is right when the step must not be influenced
by what came before.

## Rung 4: a task group

```yaml agent.yaml
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

Each name in `steps` is a task already declared in `tasks:`. The order is a
guarantee, not a request in a prompt.

| Field | Values | What it does |
|---|---|---|
| `context_scope` | `shared`, `isolated` | whether the steps see one running context or each start clean |
| `then` | `return`, `transfer`, `end` | what happens after the last step |
| `then_target` | an agent name | required with `transfer`, illegal otherwise |
| `merge` | `results` | how the steps' results are combined |

**Context decision, twice over:**

- `context_scope: shared` means the service the caller named in step one is
  still there in step two. Each completed step's exact typed result is also in
  shared context before the next step starts, so never ask a later step to
  reconstruct IDs or confirmation flags from spoken wording. This is what you
  want most of the time.
- `context_scope: isolated` means each step starts from its own prompt. It is
  one setting for the whole group, not per step, so choosing it makes **every**
  step start clean without earlier results. Reach for it only when the group is
  a set of independent assessments that must not colour each other. An intake flow that must not ask
  the same question twice needs `shared`, and that is most groups.
- `then: return` sends control back to the agent that delegated, with `merge:
  results` deciding what comes back. `then: transfer` needs `then_target` and
  hands the call to that agent instead. `then: end` finishes the call.

Say which of these you chose and why. All four combinations validate, and only
one of them is what the user meant.

## Every context decision, in one place

This is the checklist. Run it before you claim a package is done, and say the
answer out loud for each boundary the package actually has.

| Boundary | The question | Where it is answered |
|---|---|---|
| a handoff | how much history does the new agent see? | `context.history` on the `agent_transfer` control |
| a handoff | which variables travel with the caller? | `context.variables`: `all` or a list |
| a handoff | do tool calls travel too? | `context.include_tool_calls` |
| a delegated task | what does the task see when it starts? | `context.history` on the task |
| a delegated task | what comes back, and where does it land? | `result:` on the task, `assign:` on the control |
| a task group | do the steps share context or each start clean? | `context_scope` |
| a task group | what happens when the last step ends? | `then`, and `then_target` if it transfers |
| a task group | what returns to the caller? | `merge: results` |

A boundary with no stated decision is a default nobody chose. Name it.

Two things the table does not cover, because they trip people up:

- **A control is not a tool file.** Names under `controls:` go in an agent's or
  a task's `tools:` list, but never in the package-level `tools:` list, which
  loads files from `tools/`.
- **Declaring a control without attaching it is a build error**, not dead
  config. Write both halves in the same edit. The refusal names the file, the
  line, and the agents you could attach it to:

  ```
  agent.yaml:73: control "to_front_desk" is declared but no agent reaches it; add it to the
    tools: of one of these agents: front_desk, disputes_specialist
  ```

  The same check covers a task or task group nothing delegates to, an agent no
  `agent_transfer` reaches, a `destinations:` entry no control resolves to, and
  a `tools:` entry no agent lists. An unreferenced `models:` entry is the one
  exception: that map is a palette and unused entries are legal.
- **A second agent's instructions cannot read a `conversation` variable.** An
  instructions file renders once, at session start, so it can only name a value
  that already exists. With `history: full` the new agent can see what was said,
  but writing `{{customer_name}}` into its prompt for a value the first agent
  collected mid-call is refused. Rely on the history and say so in prose.

## Guards the machine checks

```yaml
controls:
  book_appointment:
    kind: delegate
    task: appointment_request
    when: The caller wants to book, once you know who they are.
    requires:
      - customer_id
```

`requires:` is a machine-checked precondition on a control, not a sentence in a
prompt the model can talk itself past. Use it wherever an order really matters
and a task group would be too heavy.

**Put it on the control that consumes the value, never on the one that produces
it.** The example above guards booking, because booking needs a `customer_id`.
Putting `requires: customer_id` on the identification control that creates
`customer_id` would mean it could never fire at all.

Note that `requires:` on a transfer is refused on Vapi, which has no
machine-checked guard. On the two targets that generate, it works.

## Where a target refuses a shape

Raise these **before** you write files, not after validate fails.

| Shape or option | Refused on | What it says |
|---|---|---|
| `model:` on a task | Pipecat | the Pipecat driver does not emit per-task model yet |
| `include_tool_calls: false` on a transfer context | Pipecat, Vapi | the Pipecat driver does not shape transfer context yet |
| a variables subset on a transfer context | Pipecat, Vapi | Pipecat accepts context, not a subset. Vapi accepts `variables: all` only |
| `task_groups` | LiveKit warns | LiveKit TaskGroup is experimental. It compiles and runs |
| `tasks` at all | Vapi | Vapi return-to-prior-assistant is unverified |
| `context_scope: isolated` | Vapi | Vapi cannot isolate task-group context |
| `requires:` on a transfer | Vapi | Vapi has no machine-checked transfer guard |
| a nested task result | Vapi | Vapi cannot enforce nested task results |

Two things follow from that table.

**Per-task model does not work on Pipecat.** If a user wants a cheap model for
one step and an expensive one for another, and the target is Pipecat, say so
before you write it. On LiveKit it compiles.

**LiveKit's task group warning is worth repeating.** The package validates,
compiles, and runs. The warning names what it is standing on, and a user
deciding whether to ship should hear it:

```
  livekit: LiveKit TaskGroup is experimental
```

## Delegate or transfer

Both are controls. The difference is whether control comes back.

| | `delegate` | `agent_transfer` |
|---|---|---|
| returns | yes | no |
| typed result | yes, `result:` | no |
| targets | a task or a task group | another agent |
| context control | `context:` on the task | `context:` on the control |

## The four shapes, as packages

These live in the unmute repository, not in the project you are working in.
`examples.md` says how to reach them. Read the closest one before you write a
new one.

| Package | Shape |
|---|---|
| `examples/simple-prompt` | one agent, one prompt, every tool |
| `examples/multi-task` | one agent, two tasks it delegates to |
| `examples/task-groups` | one agent, three ordered tasks, shared context |
| `examples/subagents` | two agents that hand the caller over |
