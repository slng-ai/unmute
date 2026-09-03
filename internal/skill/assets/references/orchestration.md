# Orchestration

Four ways to organize an agent. Each one owns a different boundary. Choose from
the brief before you write files, then say what context crosses that boundary.

## How an agent declares what it can do

One rule carries most of the surface:

> Four of the five lists point at a same-named top-level catalog; attach by
> name. The fifth, `tasks:`, has none — write the task right where it runs.

| Agent list | Catalog | What it does | Does control come back? |
|---|---|---|---|
| `tools:` | `tools:` | does one piece of work and returns a value | yes |
| `tasks:` | nested in the agent | runs a focused step, then control returns | yes |
| `task_groups:` | `task_groups:` | runs several tasks in a fixed order, then control returns | yes |
| `handoffs:` | `handoffs:` | the conversation becomes another agent, and never returns | no |
| `escalations:` | `escalations:` | the caller goes through to a person | no |

Which block a thing is written in is its kind, so nothing carries a `kind:`
field. All five become callable function names at runtime, so they share one
namespace and a name belongs to exactly one of them.

`tools:` is the odd one in shape only: the package-level `tools:` is a list of
names, because each tool is its own file under `tools/<name>.yaml`.
`task_groups:`, `handoffs:` and `escalations:` are maps of definitions written
inline in `agent.yaml`. `tasks:` is the one list whose items can be either
shape: a mapping defines a task and attaches it to this agent in the same
place; a bare string names a task another agent already defined, so two
agents can offer one task without either owning a second copy of it.

A task has `tools:` and `handoffs:` and no other list. There is no
`task_groups:` and no `escalations:` key on a task, so a task cannot start
another task or reach a person directly — those shapes are unwritable rather
than rejected.

A task with no `when:` is a definition only, valid solely as a step of a task
group. An agent naming it by bare name — rather than listing it as a step of a
`task_groups:` entry — is refused: there is no trigger for the agent to act
on.

## Choose the native shape

| The brief needs | Native shape | Do not invent |
|---|---|---|
| A real external or local action | `tool` | A task used only as an API wrapper |
| One bounded step that returns | `task` | A second agent or progress flags |
| Several steps in a fixed order | `task group` | Current-step variables or transition tools |
| A lasting role or permission change | `agent handoff` | A returning task |
| A runtime value needed across a boundary | `variable` | Conversation memory or workflow state |

If none of these boundaries exists, keep one agent with a clear prompt and its
real tools. The user describes the job; you choose the shape. Do not make them
name an Unmute primitive.

A server-directed sequence is dynamic, even when its response calls the field
`nextStep`. Put the loop in one task that asks the returned question and calls
the real domain tools. If fixed stages surround that loop, those stages can be
tasks in a task group. Do not turn every possible server step into a group step:
the server, not the package, owns that order.

## Do not rebuild the orchestrator

- Do not create `current_step`, happy-path, completion, or routing variables.
  Tasks, groups, handoffs, and an external server already hold that state.
- Do not create `advance`, `proceed`, dispatcher, or transition tools. A tool
  performs real external or local work; a native control moves the conversation.
- Do not copy every tool onto the entry agent. Each task or agent holds only the
  tools it can use in that phase.
- Keep a variable only when its value really crosses a boundary or feeds a later
  tool call. Context is not a bag of variables to maintain by hand.

## The symptom decides the shape

| What the user is seeing | The shape that fixes it |
|---|---|
| the prompt keeps growing and starts contradicting itself | split it: tasks if the parts serve one caller goal, a handoff if they are separate roles |
| the model does things out of order | a task group. The order is declared, not requested |
| the model calls a tool it should not have yet | move the tool. Lists are per agent and per task, so a tool the current step does not hold cannot be called at all |
| a step runs before you have the value it needs | `requires:` on that task. The step is held back, and the model is told which control fills the gap |
| you need a value out of a step and want to keep it | a task with `result:`, saved to a variable with `assign:` |
| two phases need different tools or different permissions | a handoff |
| the caller changes intent while a task is active | put the destination's handoff on that task's own `handoffs:` list |
| the caller needs a person | none of these. That is an escalation, and what it can do depends on the phone route. See `transfers.md` |

A prompt that says "always identify the caller first" is a request. A task group
is a guarantee. That is the difference you are buying. Reach for a task group
when the order must hold, and for `requires:` when one value must exist before
one step runs. `requires:` is cheaper: no extra step, no extra prompt, and
nothing the caller is spoken through.

## What each shape costs

| Shape | Control | Context | Correcting a step | The cost |
|---|---|---|---|---|
| one agent and tools | stays with the agent for the whole call | the whole conversation | ask again | the prompt carries everything, and grows |
| task | returns when the task finishes | what `context:` gives the task | run it again | one more prompt and one more tool list to keep straight |
| task group | returns when the group finishes | shared across the steps, or not | steps can be revisited inside the group | an order you have to be sure about |
| handoff | leaves for good | only what `context:` carries over | another handoff back | nothing returns, so there is no result and no automatic way back |

These are not exclusive. One agent can run a task in one phase and hand off in
another.

## One agent and tools

```yaml agent.yaml
entry_agent: appointment_desk

agents:
  appointment_desk:
    instructions: instructions.md
    think: reasoning
    speak: voice
    tools:
      - check_availability
      - book_appointment
      - end_call
```

Context decision: none. The agent sees the whole conversation. Say that.

## Agent handoff

```yaml agent.yaml
entry_agent: booking_desk

agents:
  booking_desk:
    instructions: instructions.md
    think: reasoning
    speak: voice
    handoffs:
      - to_appointment_manager

  appointment_manager:
    instructions: agents/appointment-manager.md
    think: reasoning
    speak: voice
    tools:
      - cancel_appointment
    handoffs:
      - to_booking_desk

handoffs:
  to_appointment_manager:
    to: appointment_manager
    when: The caller wants to reschedule or cancel an existing appointment.
    announce: "I’m connecting you with our appointment manager now."
    context:
      history: full
      variables: all

  to_booking_desk:
    to: booking_desk
    when: The caller wants to make a new appointment instead.
    announce: "I’m connecting you back to the booking desk for your new appointment."
    context:
      history: full
      variables: all
```

`entry_agent` decides who answers. Each agent has its own prompt file and its
own five lists of what it can do, and keeps the call once it arrives.

`when` is the condition the model reads. Write it as the situation, not as an
instruction to call a tool.

`announce` is optional. Give it the exact short sentence the caller should hear.
The active agent speaks it once and finishes before the next agent takes over.
Leave it out for a silent handoff. Do not duplicate the cue in the prompt; the
handoff owns its order.

**A handoff does not come back.** Nothing returns and no result is passed back.
That is why the example declares a handoff in each direction: coming back is
another handoff, not a return. If you want a step that runs and hands control
back with an answer, that is a task. A handoff back to the entry agent continues
the call and never repeats the call-start greeting.

The same handoff may appear in a task's own `handoffs:` list. Calling it ends
that task and any remaining task-group steps, then activates the target without
returning through the owner. A task has `tools:` and `handoffs:` and no other
list, so a second task or an escalation on a task is unwritable rather than
rejected.

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

`requires:` is legal on a handoff when variables must exist before the call
leaves this agent. It is also legal on a task, which is usually the better
place for it: see [Guarding a step](#guarding-a-step) below.

## Task

```yaml agent.yaml
agents:
  appointment_desk:
    instructions: instructions.md
    think: reasoning
    speak: voice
    tasks:
      - name: customer_record
        when: Identify the caller before handling an appointment request.
        instructions: tasks/customer-record.md
        tools:
          - lookup_customer
          - create_customer
        assign:
          - customer_id: result.customer_id
          - customer_name: result.customer_name
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
```

A task is nested inside the agent that runs it. The mapping does two things at
once: it defines the task — `instructions`, `tools`, `result`, `context` — and
it attaches it, because `when:` is the trigger the model reads to decide to
run it. There is no separate catalog to keep in step with the agent's own
list.

`result:` is the contract. The task must come back with those fields, and
`record_status` can only be one of three values. That shape is what makes a
task different from a longer prompt. Its internal turns stay out of the
agent's context, while the completed call stays there so the same request
does not run twice.

Every task, including a task inside a group, needs a non-empty `result:` and `context.history`.

**Task `result:` and tool `output:` are different contracts.** Tool contracts
live in `tools/<name>.yaml`; the task's `tools:` list contains names only. The
task result describes the outcome returned after all of its tool calls. Keep
only what the calling agent needs instead of copying an attached tool's output
schema by default.

`assign:` copies fields out of the result into package variables, so the rest
of the call uses them without asking again. Declare each of those variables at
the top level, and write `assign:` as a list of one-key items, one pair per
line:

```yaml
assign:
  - customer_id: result.customer_id
  - customer_name: result.customer_name
```

A task can also declare its own `think:`, naming a different reasoning
profile for that one step alone. Leave it out and the task runs on the
agent's own model.

### Sharing a task across agents

A second agent that should offer the same task does not redefine it. It names
the task by bare string instead:

```yaml agent.yaml
agents:
  appointment_desk:
    tasks:
      - name: customer_record
        when: Identify the caller before handling an appointment request.
        instructions: tasks/customer-record.md
        result:
          customer_id: string
        context:
          history: full

  billing_desk:
    # appointment_desk defines this one. A bare name runs the same task from
    # here, so there is one definition and both agents can offer it.
    tasks:
      - customer_record
```

Task names are unique across the whole package. Two agents defining a task
under the same name is refused, naming both agents:

```
agent.yaml:17: task "customer_record" is defined by agent "appointment_desk" and again
  by agent "billing_desk". A task name is one name across the package: keep
  one definition and let the other agent name it, "- customer_record"
```

### A definition with no `when:`

A task with no `when:` is a definition only, valid solely as a step of a task
group. An agent naming it by bare name — rather than listing it as a step in
some `task_groups:` entry — is refused: there is no trigger for the agent to
act on. Give it a `when:` to make it something an agent runs on its own, or
list it as a step in [Task group](#task-group).

### Covering the entry with `announce:`

Entering a task costs two model requests, and they were silence. One fixed
sentence, spoken as the task starts:

```yaml agent.yaml
      - name: manage_booking
        when: The caller wants to create, modify, or cancel a booking.
        announce: Let me pull up the diary.
        instructions: tasks/booking.md
        requires:
          - customer_phone
```

Same field as a tool's, same rules: one fixed sentence, spoken word for word.
**Not spoken when the task is held back by a `requires:` guard**, which
matters: a caller who hears "let me pull up the diary" and is then asked for a
phone number has been told something untrue.

Do not put one on a task whose first tool already announces, and check the
tool that runs immediately **before** the task too: a lookup at the end of one
task and an `announce:` into the next cover the same handover, so the caller
hears two lines for one request. Both cases read as a stutter. The task's line
covers two model requests and the tool's covers a request body, so the task's
is usually the one worth keeping.

Denied on the `slng` target, which writes one agent with no steps.

## Guarding a step

A task that needs a value the conversation has not collected yet declares
`requires:`. Put the guard on the task that needs the value, not on the
handoff that reaches it:

```yaml agent.yaml
agents:
  appointment_desk:
    tasks:
      - name: customer_record
        when: Identify the caller before handling an appointment request.
        instructions: tasks/customer-record.md
        assign:
          - customer_phone: result.customer_phone
        result:
          customer_phone: string
        context:
          history: full

      - name: manage_appointment
        when: The caller wants to make, change, or cancel an appointment.
        requires:
          - customer_phone
        instructions: tasks/appointment.md
        result:
          status: string
        context:
          history: full
```

Every name in `requires:` must be a declared variable, or the package fails to
compile. That is deliberate: a guard on a name nothing sets can never pass, and
the symptom would be a task that silently never starts.

**What the caller hears: nothing.** The refusal goes to the model, not to the
caller. It names the missing variable and the task that supplies it, so the
model runs `customer_record` and calls the step again on the same turn. The
compiler also appends the requirement to the task's own description, so the
model usually collects the value during the earlier turns and the guard is
never reached. After five refusals of the same task the agent stops recovering
quietly and asks the caller for the value out loud, in its own words. That
bound lives in the emitted code, not in a prompt. Both refusals are logged
with the variable and task names only — never the value, which matters when
the name is a phone number.

**Do not put an agent in front of a task to hold a guard.** Before `requires:`
worked on a task, the only machine-checked way to gate a step was to give the
step to a second agent and guard the handoff to it. That agent then had to be
spoken through, which cost the caller a turn and taught a shape nobody needed.
One agent, one guarded task.

**And do not gate reaching a person.** An escalation takes no `requires:` at
all, and a handoff to the agent that hears complaints should not carry one.
Someone who asks for a manager should not be interviewed first.

**While a task runs, the caller is talking to the task**: its prompt, and only
what the task's own `tools:` and `handoffs:` lists name. The agent's lists are
not offered again until the task returns. Put a handoff on the task when a
caller must be able to change intent mid-task; invoking it exits the task and
any remaining group steps. That is the useful half of nesting a task under an
agent, and also the part to plan for, because anything the task may need has
to appear in its own list.

**A task with no handoff still gets an escape, and you do not write it.** Every
generated task prompt ends with the compiler's own rule: when the caller asks
for something none of the step's tools can do, and no handoff on the step covers
it, call the step's finish function right away with the closest result it has,
instead of refusing. Do not repeat that instruction in a task's own file, and do
not add a handoff to a step that must stay locked down just to give it a way out.

That rule is narrow on purpose: it never excuses skipping the step's own work,
the caller's original reason for being in the step is not an unserved request,
and a handoff the step declares wins over it.

The request itself travels in `unserved_request`, a reserved optional string on
every generated finish. **Never declare it in a task's `result:`** — validation
rejects a task result that claims the name. It arrives inside the returned
result, and the owning agent is told to take that request next, so the caller
does not repeat it. Only `then: return` hands the result to an owner on both
targets: `then: end` ends the call, and on `then: transfer` only Pipecat passes
the results to the receiving agent.

### The opening turn cannot hand off again

A receiving agent's automatic opening reply is offered every tool it has except
its own handoffs, so two agents cannot bounce the call between them before the
caller hears anything. Every later turn has the full list back, handoffs
included. Do not add a prompt rule for this; it is in the emitted code.

### Empty LiveKit task responses

Generated LiveKit tasks retry up to twice when a full successful response has
neither non-whitespace text nor a tool call. Each retry starts immediately in
the same speech turn. It keeps the task state and model settings, and
adds a distinct temporary recovery instruction to a fresh copy of the
conversation context before each retry. After a task tool returns, recovery
keeps only `finish` instead of the full tool list. Non-whitespace text or an
allowed tool call stops recovery.
Errors and cancellations keep LiveKit's normal behavior.

If all three opening attempts are empty, the task speaks one fixed brief
failure, runs no action, and stays active for the caller's next turn. If an
empty reply follows a task tool, recovery can only call `finish`; it cannot run
another operation. Exhausting that recovery asks the caller to check the current
state before trying again. This applies only to generated LiveKit tasks. Normal
LiveKit agents and Pipecat output are unchanged.

**Context decision:** `context:` on the task says what it sees at the start.

| Field | Values | Notes |
|---|---|---|
| `history` | `full`, `messages`, `last_n`, `summary`, `reset` | required |
| `max_messages` | a positive number | legal with `last_n` only |
| `summarizer` | a model entry name | legal with `summary` only |
| `include_tool_calls` | `true` or `false` | whether tool calls travel too |
| `variables` | `all` or a list of names | handoffs only, not tasks |

`history: full` is usually right, because the caller has already said something
the task needs. `history: reset` is right when the step must not be influenced
by what came before.

## Task group

```yaml agent.yaml
tools:
  - lookup_customer
  - check_availability
  - book_appointment

agents:
  appointment_desk:
    instructions: instructions.md
    think: reasoning
    speak: voice
    tasks:
      - name: identify_customer
        instructions: tasks/identify-customer.md
        tools:
          - lookup_customer
        result:
          customer_id: string
          summary: string
        context:
          history: full

      - name: select_appointment
        instructions: tasks/select-appointment.md
        tools:
          - check_availability
        result:
          selected_slot: string
          summary: string
        context:
          history: full

      - name: finalize_appointment
        instructions: tasks/finalize-appointment.md
        tools:
          - book_appointment
        result:
          booking_status:
            enum:
              - booked
              - cancelled
          summary: string
        context:
          history: full
    task_groups:
      - appointment_flow

task_groups:
  appointment_flow:
    when: The caller wants to book, reschedule, or cancel an appointment.
    steps:
      - identify_customer
      - select_appointment
      - finalize_appointment
    context_scope: shared
    then: return
    merge: results
```

The three member tasks are nested inside the agent that runs the group, the
same way any other task is. None of them carries its own `when:`: a step's
trigger is the group's, not its own, so the group runs them in order once it
starts. Each name in `steps` is a task already declared inside some agent's
own `tasks:` list. The order is a guarantee, not a request in a prompt. A
group contains tasks, not other groups.

An agent runs a group by naming it in its own `task_groups:` list, the same
way it names a handoff or an escalation. The group's `context_scope` governs
how the members relate while the group runs. It does not replace a member
task's `context:` block. Every member still needs its own non-empty `result:`
and `context.history` so it is a complete task on its own.

| Field | Values | What it does |
|---|---|---|
| `context_scope` | `shared`, `isolated` | whether the steps see one running context or each start clean |
| `then` | `return`, `transfer`, `end` | what happens after the last step |
| `then_target` | an agent name | required with `transfer`, illegal otherwise |
| `merge` | `results` | how the steps' results are combined |

**Context decision, twice over:**

- `context_scope: shared` means the service the caller named in step one is
  still there in step two, with nothing passed by hand. This is what you want
  most of the time. Each exact typed result enters the shared context before
  the next task starts. LiveKit labels it with the source task, so the next task
  can identify the value instead of reconstructing it from conversation wording.
- `context_scope: isolated` means each step starts from its own prompt. It is
  one setting for the whole group, not per step, so choosing it makes **every**
  step start clean. An isolated group carries no results between steps. Reach
  for it only when the group is a set of independent assessments that must not
  colour each other. An intake flow that must not ask the same question twice
  needs `shared`, and that is most groups.
- `then: return` sends control back to the agent that named the group, with
  `merge: results` returning the final map keyed by task name. `then: transfer`
  needs `then_target` and hands the call to that agent instead. `then: end`
  finishes the call.

Say which of these you chose and why. All four combinations validate, and only
one of them is what the user meant.

A member task may also list a handoff, under its own `handoffs:` key. Calling
it ends that task and skips the group's remaining steps, then hands the caller
directly to the receiving agent without returning through the group's owner.

## Every context decision, in one place

This is the checklist. Run it before you claim a package is done, and say the
answer out loud for each boundary the package actually has.

| Boundary | The question | Where it is answered |
|---|---|---|
| a handoff | how much history does the new agent see? | `context.history` on the handoff entry |
| a handoff | which variables travel with the caller? | `context.variables`: `all` or a list |
| a handoff | do tool calls travel too? | `context.include_tool_calls` |
| a task | what does the task see when it starts? | `context.history` on the task |
| a task | what comes back, and where does it land? | `result:` on the task, `assign:` on the same task |
| a task group | do the steps share context or each start clean? | `context_scope` |
| a task group | what happens when the last step ends? | `then`, and `then_target` if it transfers |
| a task group | what reaches later shared steps and returns to the caller? | exact intermediate results enter shared context before the next step; the final `merge: results` map is keyed by task name |

A boundary with no stated decision is a default nobody chose. Name it.

Two things the table does not cover, because they trip people up:

- **A catalog entry is not a tool file.** Names under `task_groups:`,
  `handoffs:` and `escalations:` go in an agent's list of the same name; a
  task is nested where it runs rather than named from a catalog. None of the
  five belong in the package-level `tools:` list, which loads files from
  `tools/`. A task may name a handoff and nothing else.
- **Declaring a catalog entry without attaching it is a build error**, not dead
  config. Write both halves in the same edit. The refusal names the file, the
  line, and the agents you could attach it to:

  ```
  agent.yaml:73: handoff "to_front_desk" is declared but no agent reaches it; add it to the
    handoffs: of one of these agents: front_desk, disputes_specialist
  ```

  The same check covers a task group nothing attaches, an agent no handoff
  reaches, a `destinations:` entry no escalation resolves to, and a `tools:`
  entry no agent lists. A task with no `when:` that no task group's `steps:`
  lists is refused too, with its own message. An unreferenced `models:` entry
  is the one exception: that map is a palette and unused entries are legal.
- **A second agent's instructions cannot read a `conversation` variable.** An
  instructions file renders once, at session start, so it can only name a value
  that already exists. With `history: full` the new agent can see what was said,
  but writing `{{customer_name}}` into its prompt for a value the first agent
  collected mid-call is refused. Rely on the history and say so in prose.

## Where a target refuses a shape

Raise these **before** you write files, not after validate fails.

| Shape or option | Refused on | What it says |
|---|---|---|
| `think:` on a task | Pipecat | the Pipecat driver does not emit per-task model yet |
| `include_tool_calls: false` on a transfer context | Pipecat | the Pipecat driver does not shape transfer context yet |
| a variables subset on a transfer context | Pipecat | Pipecat accepts context, not a subset |

### Task history by target

| Target | Supported task history | Rejected task history |
|---|---|---|
| `pipecat` | `full` | `messages`, `last_n`, `summary`, `reset` |

Pipecat emits full task history only. Choose another history mode only for a
target that supports it; validation refuses the unsupported package instead of
silently widening or dropping context.

Two things follow from that table.

**Per-task `think:` does not work on Pipecat.** If a user wants a cheap model
for one step and an expensive one for another, and the target is Pipecat, say
so before you write it. On LiveKit it compiles.

**Task groups stand on a LiveKit beta.** They compile and run, and unmute does
not warn about it: a note about a framework's own maturity fired on every run of
a working package and told the author nothing to do. Say it here instead, where
somebody is choosing the shape, and point them at the LiveKit release notes if
they are deciding whether to ship.

## Task or handoff

The difference is whether control comes back. That is why they are two lists
rather than one list with a kind field.

| | `tasks:` | `handoffs:` |
|---|---|---|
| returns | yes | no |
| typed result | yes, `result:` | no |
| targets | a task, run in place | another agent |
| context control | `context:` on the task | `context:` on the handoff entry |
| `requires:` | yes | yes |

## The shapes, as packages

One package in the unmute repository shows these shapes working together:
`examples/salon-concierge` has two agents that hand the caller over, two tasks
nested in the concierge — one of them guarded with `requires:` — and a bare
name that lets the complaint specialist run the same verification task without
a second copy.

The one-agent, one-prompt shape has no package. `unmute init <name>` scaffolds
it, and `package.md` in this bundle has the same shape inline.

Task groups have no package either. The YAML above is the reference, and the
LiveKit beta note in "Where a target refuses a shape" is the thing to repeat
before a user ships one.
