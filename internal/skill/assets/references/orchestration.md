# Orchestration

Four ways to organize an agent. Each one owns a different boundary. Choose from
the brief before you write files, then say what context crosses that boundary.

## How an agent declares what it can do

One rule carries most of the surface:

> An agent writes what it can do inside its own block. `tools:` names files in
> `tools/`; `tasks:`, `handoffs:`, `transfers:` and `flows:` are written right
> where they run.

| Agent list | What it does | Does control come back? |
|---|---|---|
| `tools:` | does one piece of work and returns a value | yes |
| `tasks:` | runs a focused step, then control returns | yes |
| `flows:` | runs several tasks in a fixed order, then control returns | yes |
| `handoffs:` | the conversation becomes another agent, and never returns | no |
| `transfers:` | the caller goes through to a person | no |

Which list a thing is written in is its kind, so nothing carries a `kind:`
field. All five become callable function names at runtime, so they share one
namespace and a name belongs to exactly one of them.

`agents:` is a list, each item starting `- name:`, and each agent's prompt is
`prompt:`. A handoff, transfer or flow is defined once in the package, by the
agent that writes its block. Any other agent names it with a bare name. A bare
name nobody defines is refused, and so are two blocks with one name. `tasks:`
works the same way: a mapping defines a task and attaches it to this agent in
the same place; a bare string names a task another agent already defined, so
two agents can offer one task without either owning a second copy of it.

The `tools/` folder is the list of tools, read in name order. An agent's own
`tools:` list still names the tools that agent may call.

A task has `tools:` and `handoffs:` and no other list. There is no `flows:` and
no `transfers:` key on a task, so a task cannot start another task or reach a
person directly: those shapes are unwritable rather than rejected.

## Choose the native shape

| The brief needs | Native shape | Do not invent |
|---|---|---|
| A real external or local action | `tool` | A task used only as an API wrapper |
| One bounded step that returns | `task` | A second agent or progress flags |
| Several steps in a fixed order | `flow` | Current-step variables or transition tools |
| A lasting role or permission change | `agent handoff` | A returning task |
| A runtime value needed across a boundary | `variable` | Conversation memory or workflow state |

If none of these boundaries exists, keep one agent with a clear prompt and its
real tools. The user describes the job; you choose the shape. Do not make them
name an Unmute primitive.

A server-directed sequence is dynamic, even when its response calls the field
`nextStep`. Put the loop in one task that asks the returned question and calls
the real domain tools. If fixed stages surround that loop, those stages can be
tasks in a flow. Do not turn every possible server step into a flow step:
the server, not the package, owns that order.

## Do not rebuild the orchestrator

- Do not create `current_step`, happy-path, completion, or routing variables.
  Tasks, flows, handoffs, and an external server already hold that state.
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
| the model does things out of order | a flow. The order is declared, not requested |
| the model calls a tool it should not have yet | move the tool. Lists are per agent and per task, so a tool the current step does not hold cannot be called at all |
| a step runs before you have the value it needs | give the step's own `when:` the clause that names what has to be true first, "once the caller is identified" |
| you need a value out of a step and want to keep it | a task that saves it to a typed variable with `save:` |
| two phases need different tools or different permissions | a handoff |
| the caller changes intent while a task is active | put the destination's handoff on that task's own `handoffs:` list |
| the caller needs a person | none of these. That is a transfer, and what it can do depends on the phone route. See `transfers.md` |

A prompt that says "always identify the caller first" is a request. A flow
is a guarantee. A `when:` clause naming the dependency is not a guarantee the
same way, but it is cheap on one task: no extra step, no extra prompt, and
nothing the caller is spoken through. See "Order steps with the prompt" below.

It stops being cheap the moment there are two of them in a row. Two tasks the
agent picks between are the same two steps plus a model request at the seam,
and that request speaks no words and does no work: it exists only to name the
next task. A flow spends one request on the way in and none between the steps.
So a `when:` clause is for one step that needs a value to be there first, and a
flow is for a sequence, whether or not the order is safety-critical. Writing a
sequence as `when:`-bearing tasks to avoid "the weight of a flow" buys nothing
and costs the caller a silence per seam.

## What each shape costs

| Shape | Control | Context | Correcting a step | The cost |
|---|---|---|---|---|
| one agent and tools | stays with the agent for the whole call | the whole conversation | ask again | the prompt carries everything, and grows |
| task | returns when the task finishes | what `context:` gives the task | run it again | one more prompt and one more tool list to keep straight, and a model request every time the agent chooses it |
| flow | returns when the flow finishes | shared across the steps, or not | steps can be revisited inside the flow | an order you have to be sure about. One request on the way in, and none between the steps |
| handoff | leaves for good | chosen speech history plus explicitly referenced saved values | another handoff back | nothing returns, so there is no automatic way back |

These are not exclusive. One agent can run a task in one phase and hand off in
another.

## One agent and tools

```yaml agent.yaml
entry_agent: appointment_desk

agents:
  - name: appointment_desk
    prompt: instructions.md
    think: reasoning
    speak: voice
    tools:
      - check_slots
      - book_appointment
      - end_call
```

Context decision: none. The agent sees the whole conversation. Say that.

## Agent handoff

```yaml agent.yaml
entry_agent: booking_desk

agents:
  - name: booking_desk
    prompt: instructions.md
    think: reasoning
    speak: voice
    handoffs:
      - name: to_appointment_manager
        to: appointment_manager
        when: The caller wants to reschedule or cancel an existing appointment.
        say:
          - I’m connecting you with our appointment manager now.
        context:
          history: full

  - name: appointment_manager
    prompt: agents/appointment-manager.md
    think: reasoning
    speak: voice
    tools:
      - cancel_appointment
    handoffs:
      - name: to_booking_desk
        to: booking_desk
        when: The caller wants to make a new appointment instead.
        say:
          - I’m connecting you back to the booking desk for your new appointment.
        context:
          history: full
```

`entry_agent` decides who answers. Each agent has its own prompt file and its
own five lists of what it can do, and keeps the call once it arrives.

`when` is the condition the model reads. Write it as the situation, not as an
instruction to call a tool.

`say:` is optional. Give it a list holding the exact short sentence the caller
should hear. A handoff `say:` takes any number of lines; one is picked each time,
never the one said last, and the next agent's voice says it. A line takes no
`{{ }}` placeholder and a blank line is refused. Leave it out for a silent
handoff. Do not duplicate the cue in the prompt; the handoff owns its order.

`save:` on a handoff sets State values as the call moves, one pair per item,
for example `- explanation: ES`.

**A handoff does not come back.** Nothing returns and no result is passed back.
That is why the example declares a handoff in each direction: coming back is
another handoff, not a return. If you want a step that runs and hands control
back with an answer, that is a task. A handoff back to the entry agent continues
the call and never repeats the call-start greeting.

A task may name a handoff in its own `handoffs:` list, by bare name. Calling it ends
that task and any remaining flow steps, then activates the target without
returning through the owner. A task has `tools:` and `handoffs:` and no other
list, so a second task or a transfer on a task is unwritable rather than
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
| omitted or `history: messages` | spoken caller and agent messages; tool records are removed |
| `history: reset` | no earlier conversation |
| `history: full` | spoken messages and complete tool-call pairs |

Saved values remain in call state, but the receiving model sees only those its
own prompt references with `{{state.name}}` or `{{state.name.field}}`. Use `reset` plus
explicit references when the receiver needs saved facts without earlier speech.

Order matters on a handoff the same way it matters on a task: say the
dependency in the agent's own flow and in the handoff's `when:`, not with a
gate. See "Order steps with the prompt" below.

## Task

```yaml agent.yaml
# state.py: class State(BaseModel) with customer_id: str = "" and customer_name: str = ""

agents:
  - name: appointment_desk
    prompt: instructions.md
    think: reasoning
    speak: voice
    tasks:
      - name: customer_record
        when: Identify the caller before handling an appointment request.
        prompt: tasks/customer-record.md
        tools:
          - lookup_customer
          - create_customer
        save:
          - customer_id: result.customer_id
          - customer_name: result.customer_name
```

A task is nested inside the agent that runs it. The mapping does two things at
once: it defines the task (`prompt`, `tools`, `save`, `context`) and
it attaches it, because `when:` is the trigger the model reads to decide to
run it. There is no separate catalog to keep in step with the agent's own
list.

`save:` is the save contract. The compiler derives each finish field's type
and description from its destination variable. A task with no values to save
may omit `save:`. Tool `output:` remains the tool's own API contract.

When a task returns, its private turns and finish fields stay private. The
owner receives only `{"status":"completed"}` or `{"status":"unserved"}`.

`save:` copies fields out of the result into package variables, so the rest
of the call uses them without asking again. Declare each of those variables
in `state.py`, and write `save:` as a list of one-key items, one pair per
line:

```yaml
save:
  - customer_id: result.customer_id
  - customer_name: result.customer_name
```

The right side of a pair can also be a dotted path into a model from `state.py`,
`result.<field>.<subfield>`, to pick one part of it instead of the whole
result; see "Picking one part of a structured result" in
`references/variables.md`.

**Give a reset task only what it needs.** Save facts that must cross the
boundary as typed variables, then reference those variables in the task's own
prompt.

```yaml
      - name: manage_booking
        when: The caller wants to move a saved booking.
        prompt: tasks/booking.md
        context:
          history: reset
```

For example, `tasks/booking.md` can say `Move appointment {{state.appointment_id}} to
{{state.appointment_date}} at {{state.appointment_time}}.` No request block, summary or
triggering sentence is added automatically.

A task can also declare its own `think:`, naming a different reasoning
profile for that one step alone. Leave it out and the task runs on the
agent's own model.

### Sharing a task across agents

A second agent that should offer the same task does not redefine it. It names
the task by bare string instead:

```yaml agent.yaml
# state.py: class State(BaseModel) with customer_id: str = ""

agents:
  - name: appointment_desk
    prompt: instructions.md
    tasks:
      - name: customer_record
        when: Identify the caller before handling an appointment request.
        prompt: tasks/customer-record.md
        save:
          - customer_id: result.customer_id

  - name: billing_desk
    prompt: agents/billing-desk.md
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

A task with no `when:` is a definition only, valid solely as a step of a flow.
An agent naming it by bare name (rather than listing it as a step in some
`flows:` entry) is refused: there is no trigger for the agent to act on. Give it
a `when:` to make it something an agent runs on its own, or list it as a step
in [Flow](#flow).

### The step start

`start:` is what a step does as it opens. It takes `say:`, `listen:` and
`call:` with `with:`, and all three are optional.

Entering a task costs two model requests, and they were silence. `say:` is one
fixed sentence, or a list of alternatives, spoken as the step starts:

```yaml agent.yaml
      - name: manage_booking
        when: The caller wants to create, modify, or cancel a booking.
        prompt: tasks/booking.md
        start:
          say: Let me pull up the diary.
```

Same rules as a tool's `start:`: one fixed sentence, spoken word for word.

Do not put one on a task whose first tool already has a `start:` line, and
check the tool that runs immediately **before** the task too: a lookup at the
end of one task and a `start:` into the next cover the same handover, so the
caller hears two lines for one request. Both cases read as a stutter. The
step's line covers two model requests and the tool's covers a request body, so
the step's is usually the one worth keeping.

**In a flow, put the line on each step and none on the flow.** A step's own
`start:` fires as that step is entered, so a flow of three steps covers all
three seams with three lines that are each true where they play. The flow's own
`say:` fires once on the way in, before anything has been decided, and it
has to be true of whichever step runs first: a flow that opens on a
verification question the first time and on a diary read every time after has
no such sentence, and a skipped first step would hear a line about work that
never happened.

On the `slng` target a task or a flow takes one line, not alternatives.

`listen: true` opens a step by waiting. When a step opens with one fixed
question, `say:` speaks it and `listen: true` waits, and neither costs a
request:

```yaml agent.yaml
      - name: take_stylist_note
        when: The caller wants a note left for the stylist.
        prompt: tasks/stylist-note.md
        start:
          say: What would you like me to pass on to your stylist?
          listen: true
```

The line is spoken once and recorded as the step's own first turn. Without
`listen: true` the step generates its opening, as it always did. A listening
step with no `say:` warns: the caller hears nothing until they speak. Tell the
step's prompt that the question has already been asked.

`call:` runs one of the step's tools in code before the model speaks, and
`with:` gives it its arguments, one `- name: state.x` pair per item. A `?`
after the value (`state.requested_day?`) makes it optional, so the call runs
without it. No `?` means the tool requires it, and the compiler checks both
marks against the tool's input schema.

```yaml agent.yaml
      - name: manage_booking
        prompt: tasks/booking.md
        start:
          say: Let me have a look.
          call: find_slots
          with:
            - service: state.requested_service?
            - date: state.requested_day?
        tools:
          - name: find_slots
            outcomes:
              - if:
                  - offered: true
                say:
                  - "{{result.offer}}"
```

What happens when the step opens:

- A required `with:` value is empty: the call does not run, and the step says
  `say:` and listens.
- The value is already confirmed: the step is skipped.
- The tool has a `confirm:` line: the read-back is said instead of `say:`.
- Otherwise the call's result goes through the tool's outcomes, and the line
  they pick is said.

### Ending a step on its tool

A step that runs a tool and then asks the model to call `finish` spends a model
request on a decision the tool already made. Give the tool an outcome with
`then: done`:

```yaml agent.yaml
      - name: manage_booking
        prompt: tasks/booking.md
        tools:
          - find_slots
          - name: save_booking
            outcomes:
              - if:
                  - status:
                      - booked
                      - cancelled
                then: done
        save:
          - appointment: result.appointment
```

A result matching the `if:` saves the step's `save:` and ends the step, with
no request in between, and never reaches the model. Anything else goes back to
the model and the step stays open. Outcomes are written once, in
`references/tools.md`, "The tool entry in agent.yaml".

Rules worth knowing before you write one:

- Every `if:` value has to be one the tool declares in its output `enum:`,
  and every `save:` field has to be an output property of **every** tool with a
  `then: done` outcome. Both are compile-time refusals.
- After one such tool succeeds, none of them runs again in that invocation. A
  second booking is a new invocation of the step.
- `finish` stays for a request the step cannot serve, for values it already
  holds, and for a save the validator refused. A repair keeps the values the
  tool returned; the model fills only what the tool could not.
- The step says nothing after its tool unless the outcome has a `say:`
  line. Otherwise the next step opens, or the owner speaks once when the flow
  returns, so write the owner's acknowledgement into the owner's prompt and not
  into the step's.
- A handoff the model calls beside one of these tools, or while one is still
  running, waits for it: the caller is moved once the action is recorded, and
  told the action did not complete when it did not.
- Do not also write "call finish as soon as the tool succeeds" in the step's
  prompt. The generated tail says the opposite, and the two contradict.

On the `slng` target `then: done` works with one outcome per task. Its `say:`
lines are spoken by SLNG, and `confirm:` is refused. The tool needs a known
output: a typed local tool, or a hosted `slng:` tool with a mirror. Twilio
refuses `then:`, `confirm:` and `say:` outcomes.

## Order steps with the prompt

There is no field that holds a task back until a variable exists. Put the
order in the prompt: number the steps in the agent's own prompt, and
give the later task a `when:` that names what has to be true first:

```yaml agent.yaml
agents:
  - name: appointment_desk
    prompt: instructions.md
    tasks:
      - name: customer_record
        when: Identify the caller before handling an appointment request.
        prompt: tasks/customer-record.md
        save:
          - customer_phone: result.customer_phone

      - name: manage_appointment
        when: The caller wants to make, change, or cancel an appointment, once the caller is identified.
        prompt: tasks/appointment.md
```

Reference a saved value in a task prompt only when that task needs it. No
automatic state block is added.

**The last line sits on the tool.** A tool that silently reads a value,
through `with:` or a webhook path, refuses to run while that value is empty
or unconfirmed, and the model is told which task supplies it, or, when
nothing does, to ask the caller. The worst case is one extra model turn, not
a request sent with a blank or an unconfirmed field.

**Check the order with a scripted text conversation before a live call.**
It drives the agent through a fixed script with the real model and the real
tools, so you see which task ran, and in what order.

**Never hold up the way to a person.** A transfer should carry no
dependency like this, and neither should a handoff to the agent that hears
complaints. Someone who asks for a manager should not be interviewed first.

**While a task runs, the caller is talking to the task**: its prompt, and only
what the task's own `tools:` and `handoffs:` lists name. The agent's lists are
not offered again until the task returns. Put a handoff on the task when a
caller must be able to change intent mid-task; invoking it exits the task and
any remaining flow steps. That is the useful half of nesting a task under an
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
every generated finish. It saves no domain values. On return, the owner receives
only `{"status":"unserved"}` with its earlier conversation. Task-local speech
and result values stay private. The owner uses its saved-value references and
asks the caller for any missing request details.

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
| `history` | `full`, `messages`, `last_n`, `summary`, `reset` | optional; omitted means `messages` |
| `max_messages` | a positive number | legal with `last_n` only |
| `summarizer` | a model entry name | legal with `summary` only |
| `include_tool_calls` | `true` or `false` | whether tool calls travel too |

What each value gives the step:

- `full`: the running transcript, tool records included.
- `messages`: what the caller and the agent said out loud, tool records
  dropped.
- `last_n` with `max_messages: n`: the newest n entries, tool records kept,
  never a tool result whose call was cut.
- `reset`: the step's own instructions and its explicit saved-value references, nothing else.
- `summary`: one summarizer turn in place of the transcript. LiveKit only;
  Pipecat refuses it, see the per-target table below.

Omitted history is usually enough: the task gets what both people said without
tool records. Use `full` only when the task needs tool-call pairs. Use `reset`
when it should see no earlier speech, and put each saved value it needs directly
in its prompt.

No `history:` value is a privacy control. Shortening the history does not
unsay what the caller said out loud; the caller's words are still in the
call's own record even when a step is not shown them.

## Flow

```yaml agent.yaml
# state.py: class State(BaseModel) with customer_id: str = "",
# selected_slot: str = "" and
# booking_status: Literal["booked", "cancelled"] | None = None

agents:
  - name: appointment_desk
    prompt: instructions.md
    think: reasoning
    speak: voice
    tasks:
      - name: identify_customer
        prompt: tasks/identify-customer.md
        tools:
          - lookup_customer
        save:
          - customer_id: result.customer_id

      - name: select_appointment
        prompt: tasks/select-appointment.md
        tools:
          - check_slots
        save:
          - selected_slot: result.selected_slot

      - name: finalize_appointment
        prompt: tasks/finalize-appointment.md
        tools:
          - book_appointment
        save:
          - booking_status: result.booking_status
    flows:
      - name: appointment_flow
        when: The caller wants to book, reschedule, or cancel an appointment.
        steps:
          - identify_customer
          - select_appointment
          - finalize_appointment
```

The three member tasks are nested inside the agent that runs the flow, the
same way any other task is. None of them carries its own `when:`: a step's
trigger is the flow's, not its own, so the flow runs them in order once it
starts. Each name in `steps` is a task already declared inside some agent's
own `tasks:` list. The order is a guarantee, not a request in a prompt. A
flow contains tasks, not other flows.

The agent that writes the block defines the flow, and another agent runs it by
naming it in its own `flows:` list, the same way it names a handoff or a
transfer. It costs one model request to enter,
whatever the flow then runs: each step after the first opens by itself. The
same tasks written as separate `when:`-bearing tasks cost a request per step,
and the extra request speaks nothing and does nothing but name the next task.
The flow's `context_scope` governs
how the members relate while the flow runs. It does not replace a member
task's `context:` block. A member may omit both `save:` and `context:` when it
saves no value and the default spoken-message history is right.

A flow can say a line as it starts, with `say:`. It can also hold `fills:`, the
State values the agent fills in when it starts the flow, and `non_blocking:`,
the steps whose failure is recorded while the flow goes on. A task takes
`fills:` the same way.

| Field | Values | What it does |
|---|---|---|
| `context_scope` | `shared` (default), `isolated` | whether the steps see one running context or each start clean |
| `then` | `return` (default), `transfer`, `end` | what happens after the last step |
| `then_target` | an agent name | required with `transfer`, illegal otherwise |
| `merge` | `results` (default) | how the steps' results are combined |

Write a field only to change its default. A flow that shares context and
returns to its owner writes none of them.

**Choose the conversation each member receives:**

A step is a bare task name, or an item that says how the flow treats it:

```yaml agent.yaml
    steps:
      - task: identify_customer
        skip_when_confirmed: customer_id
      - select_appointment
```

`skip_when_confirmed:` names a variable that step confirms, through `confirm:`
on the variable. The flow skips the step when the variable is confirmed as the
flow starts, which is what makes a second booking on one call cost nothing. A
variable nobody confirms, or one another step confirms, is refused.

The side effect is the point: a step some flow names this way withdraws what it
confirms every time it is entered, standalone entry included, so a caller
correcting the value re-verifies it and the next run does not skip on the
strength of what they just replaced.

A flow also stops when a step ends unserved: the later steps do not run and the
owner is handed the unserved status.

- `context_scope: shared` lets later tasks inherit the flow's running
  conversation, filtered by each task's own `context.history`.
- `context_scope: isolated` starts each member without inherited flow
  conversation. Saved variables remain available through explicit prompt references.

**A task or flow that already ran is refused until the caller speaks again.**
Not authorable, and it applies to every step on both code targets. A completed
result with no new caller turn above it is the model reading its own finished
work as though it were a fresh request: a live call saved a booking, re-entered
the flow on the caller's own "3 o'clock" still sitting above the result, read
the diary twice more, and the caller's last words on the call were an agent
saying it was off to check the diary. The refusal comes before the step's
`start:`, so a refused call speaks nothing at all.

Write the owner's prompt to expect this rather than to work around it: a bare
"yes" above a completed flow is the caller answering the flow's own question,
which the flow has already acted on.

Task results are private. Each completed step supplies only completion status;
later tasks read saved values through `{{state.name}}` or `{{state.name.field}}`.
With `then: return`, the owner gets its original context back plus `completed`
or `unserved`, and reads saved values through its own prompt references.
`merge: results` combines results internally; it does not expose a result map
to the owner. `then: transfer` activates `then_target`; `then: end` ends the call.

A member task may also list a handoff, under its own `handoffs:` key. Calling
it ends that task and skips the flow's remaining steps, then hands the caller
directly to the receiving agent without returning through the flow's owner.

## Every context decision, in one place

This is the checklist. Run it before you claim a package is done, and say the
answer out loud for each boundary the package actually has.

| Boundary | The question | Where it is answered |
|---|---|---|
| a handoff | how much history does the new agent see? | `context.history` on the handoff entry |
| a handoff | do tool calls travel too? | `context.include_tool_calls` |
| a task or a handoff | which saved values can the receiver see? | only `{{references}}` in the receiver's own prompt |
| a task | what does the task see when it starts? | `context.history` on the task |
| a task | what is saved? | `save:` on the task; destination variables supply the types |
| a flow | do the steps share context or each start clean? | `context_scope` |
| a flow | what happens when the last step ends? | `then`, and `then_target` if it transfers |
| a flow | what reaches later shared steps and returns to the caller? | completion status; saved values are visible only through explicit prompt references |

Omitted history has a deliberate default: spoken messages without tool
records. State other choices when they matter.

Two things the table does not cover, because they trip people up:

- **A handoff, transfer or flow is a block, not a tool file.** Write it in an
  agent's `handoffs:`, `transfers:` or `flows:` list; a task is nested where it
  runs. None of the five belong in the top-level `tools:` list, which holds only
  tools with behaviour. A task may name a handoff and nothing else.
- **A definition nobody reaches is a build error**, not dead config. The
  refusal names the file, the line, and the agents you could attach it to:

  ```
  agent.yaml:73: handoff "to_front_desk" is declared but no agent reaches it; add it to the
    handoffs: of one of these agents: front_desk, disputes_specialist
  ```

  The same check covers a flow nothing attaches, an agent no handoff reaches,
  and a file in `tools/` no agent lists. A tool named only by a `prefetch:`
  entry's `call:` field passes: the reachability walk marks it reachable on its
  own, because it never reaches an agent's `tools:` list. A task with no
  `when:` that no flow's `steps:` lists is refused too, with its own message. An
  unreferenced `models:` entry is the one exception: that map is a palette and
  unused entries are legal.

## Where a target refuses a shape

Raise these **before** you write files, not after validate fails.

| Shape or option | Refused on | What it says |
|---|---|---|
| `think:` on a task | Pipecat | the Pipecat driver does not emit per-task model yet |
| `include_tool_calls: false` on a transfer context | Pipecat | the Pipecat driver does not shape transfer context yet |

### Task history by target

| Target | Supported task history | Rejected task history |
|---|---|---|
| `pipecat` | `full`, `messages`, `last_n`, `reset` | `summary` |

`history: summary` is refused on `pipecat`, on a task and on a handoff, with:

```
the Pipecat driver does not summarize a context yet: it supports history: full, messages, last_n and reset
```

Choose `summary` only for a target that supports it; validation refuses the
unsupported package instead of silently widening or dropping context.

Two things follow from that table.

**Per-task `think:` does not work on Pipecat.** If a user wants a cheap model
for one step and an expensive one for another, and the target is Pipecat, say
so before you write it. On LiveKit it compiles.

**Flows stand on a LiveKit beta.** They compile and run, and unmute does
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
| typed save | yes, through `save:` and destination variables | no |
| targets | a task, run in place | another agent |
| context control | `context:` on the task | `context:` on the handoff entry |

## The shapes, as packages

One package in the unmute repository shows these shapes working together:
`examples/salon-concierge`. Two agents hand the caller over, in both
directions. The concierge defines two tasks, `verify_customer` and
`manage_booking`. The complaint specialist defines no tasks of its own:
`record_complaint` sits directly on it as a tool, because filing a complaint
is one action rather than a step that has to happen in order. It does name
`verify_customer`, as a bare `- verify_customer` reusing the concierge's
definition, because `record_complaint` pins the caller's number with `with:` and is
refused while the caller has not agreed to it. A task within reach beats a
prompt rule, so the prompt asks for the step at the point it is needed rather
than forbidding it. `to_concierge` is the way back. Both tasks end on their own tool through
`then: done` outcomes.

The same package carries the flow, `book`: `verify_customer` with
`skip_when_confirmed: customer_phone`, then `manage_booking`, on the flow
defaults: shared context, and the call returns to the concierge. `manage_booking` has no `when:`
because the flow runs it. `verify_customer` keeps a `when:` for one case
only, a caller correcting their phone number; every other route into
verification goes through the flow. Read it before writing a flow of your
own, and repeat the LiveKit beta note in "Where a target refuses a shape"
before a user ships one.

The one-agent, one-prompt shape is shown in
`examples/salon-concierge-single-prompt`. `unmute init <name>` also scaffolds
that structure.
