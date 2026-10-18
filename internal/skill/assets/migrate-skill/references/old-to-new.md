# Old keys and their new form

Each row is one rewrite. "Where" is the file the old key sits in. `unmute
validate` names most of these by their old spelling, with the new one.

## Every old file

| Old | New | Where |
|---|---|---|
| `version: 1` or `version: 2` | delete the line | agent.yaml |

## From unmute 0.8 (`version: 2`)

### Structure

| Old | New | Where |
|---|---|---|
| `agents:` as a map, `concierge:` | a list, `- name: concierge` | agent.yaml |
| `instructions:` on an agent or a task | `prompt:` | agent.yaml |
| top-level `handoffs:` (a map) | `handoffs:` inside the agent that hands off, `- name:`, `to:`, `when:` | agent.yaml |
| top-level `escalations:` and `destinations:` | `transfers:` inside the agent that transfers, `- name:`, `to: <ENV_VAR_WITH_THE_NUMBER>`, `when:` | agent.yaml |
| an escalation's `cold:` block, `destination:`, `ring_timeout:`, `on_unavailable:` | flat on the transfer: `to:` takes the destination's variable, and `ring_timeout:` and `on_unavailable:` sit beside it | agent.yaml |
| an escalation's `warm:` block with `briefing:` | the same flat transfer with `brief:` | agent.yaml |
| top-level `task_groups:` (a map) | `flows:` inside the agent that runs it, `- name:`, `when:`, `steps:` | agent.yaml |
| a second agent that uses the same handoff, transfer or flow | the bare name, `- to_complaints`, in that agent's list | agent.yaml |
| `channels:` and `capacity:` | the top of targets.yaml. `channels:` is a list, `- name: web`, `kind:` | targets.yaml |
| `mode:` | delete it. Every step saves what the caller confirmed | agent.yaml |
| a task's `inputs:` and `via:` | delete them. A flow takes `fills:` | agent.yaml |
| `context: history: messages` | delete it. It is the default | agent.yaml |
| a flow's `context_scope: shared` and `then: return` | delete them. They are the defaults | agent.yaml |
| a top-level `tools:` list of bare names | delete the bare names. Every file in `tools/` is a tool. The list keeps only entries with `save:`, `outcomes:`, `hidden:` or `start:` | agent.yaml |
| an agent's own `tools:` list | unchanged, a list of names | agent.yaml |

### Renames

| Old | New | Where |
|---|---|---|
| prefetch `tool:` | `call:` | agent.yaml |
| prefetch `args:` | `with:`, a list of `- name: state.<value>` or a literal | agent.yaml |
| prefetch `assign:` | `save:` | agent.yaml |
| handoff `announce: "one line"` | `say:` as a list, even for one line. One line is picked each time, in the next agent's voice | agent.yaml |
| handoff `saves:` | `save:` | agent.yaml |
| handoff `opens_with:` | `start:` with `call:` and `with:` | agent.yaml |
| flow `announce:` | `say:` | agent.yaml |
| flow `arguments:` | `fills:` | agent.yaml |
| `conversation.greeting.text:` | `conversation.greeting.say:` | agent.yaml |

### A step's tools

The step keys about one tool move onto that tool's entry in the step's
`tools:` list. Each entry is `- name: <tool>`, then `confirm:` and `outcomes:`.

| Old | New | Where |
|---|---|---|
| `finish:` with `tool:` and `success:` | the tool's entry, `outcomes:` with `if:` (the success values) and `then: done` | agent.yaml |
| `finish:` `confirm:` | the tool entry's `confirm:` | agent.yaml |
| `replies:` | the tool's entry, `outcomes:` with `if:` and `say:` | agent.yaml |
| a step's `assign:` | the step's `save:`. A save that only one result makes goes on that outcome, as `save:` under it | agent.yaml |
| `opening: listen` | `start:` with `listen: true` | agent.yaml |
| a step's `announce:` | `start:` with `say:` | agent.yaml |
| `opens_with:` with `tool:` and `arguments:` | `start:` with `call:` and `with:`, each value `state.<name>`. Add `?` to a value that may be empty | agent.yaml |
| `opens_with:` `say:` | gone. The call's result goes through that tool's `outcomes:`, `if:` and `say:` | agent.yaml |
| `confirm:` on a `finish:` entry | `confirm:` on the tool's entry, beside `outcomes:`, never inside an outcome | agent.yaml |
| `{{state.x}}` in a line, where x was saved only to say it | `{{input.x}}` in a `confirm:` line, `{{result.x}}` in an outcome `say:` line. Delete the helper save | agent.yaml |

Example, a booking step:

```yaml
# Old
- name: manage_booking
  instructions: tasks/booking.md
  opens_with:
    tool: find_slots
    arguments:
      - service: requested_service
  finish:
    - tool: book_appointment
      success:
        - status:
            - booked
  assign:
    - appointment: result.appointment

# New
- name: manage_booking
  prompt: tasks/booking.md
  start:
    call: find_slots
    with:
      - service: state.requested_service?
  tools:
    - name: book_appointment
      outcomes:
        - if:
            - status:
                - booked
          save:
            - appointment: result.appointment
          then: done
```

### Tool files

What the caller hears and what the call remembers moved out of `tools/*.yaml`
into agent.yaml, onto the tool's entry in the top-level `tools:` list,
`- name: <tool>`.

| Old | New | Where |
|---|---|---|
| `assign:` | `save:` on the entry. It runs on every result | tool file to agent.yaml |
| `replies:` | `outcomes:` with `if:` and `say:` | tool file to agent.yaml |
| `announce:` | `start:` with `say:` | tool file to agent.yaml |
| `announce_wait:` | `start:` with `wait: true` | tool file to agent.yaml |
| `hidden:` | `hidden:` on the entry | tool file to agent.yaml |
| `inject:` with `"{{state.x}}"` | `with:` with `state.x`. Each name must be a parameter of the handler | tool file |

`save:`, `hidden:` and `start:` go on the top-level entry only, because they
are the same on every agent. `confirm:` and `then:` go on a step's entry only.

## From version 1 (`version: 1`)

Do these first, then everything under "From unmute 0.8". Some rows are from
packages older than unmute 0.7. Skip a row the package does not have.

| Old | New | Where |
|---|---|---|
| `variables:` as a map, each with `type:`, `default:`, `description:` | each value is a field of `class State(BaseModel)` in `state.py`. `variables:` becomes a list that keeps only values with `source:` or `confirm:` | agent.yaml, state.py |
| `shapes:` | a `BaseModel` in `state.py` | agent.yaml, state.py |
| a step's `result:`, `expect:` or `requires:` | delete them. Type the value on `State` and save it with `save:` | agent.yaml |
| `{{x}}` naming a saved value, in a prompt or a line | `{{state.x}}` | prompts, agent.yaml |
| `local: handler: tools/x.py`, whose function was named after the tool | `local: handler: tools/x.py:<function>`. One file may hold many tools | tool file |
| `input:` and `output:` on a local tool | delete them. The function's typed parameters are the input, its return type the output. Put a parameter's description in `Annotated[str, Field(description=...)]` | tool file, tools/*.py |
| a handler with untyped arguments that returns `dict` | typed parameters and a Pydantic model as the return type | tools/*.py |
| package `timezone:` | `timezone:` on the clock prefetch entry | agent.yaml |
| tool file `read_only:` | `writes: true` or `writes: false` on the prefetch entry that runs the tool | tool file, agent.yaml |
| tool file `execution:`, top-level `handler:`, `url_env:`, `auth:` | one block: `local:`, `webhook:`, `mcp:` or `builtin:`, holding those keys | tool file |
| a saved value that exists only so a line can say it | delete the save and the `State` field. A `confirm:` line reads `{{input.x}}`, and an outcome's `say:` reads `{{result.x}}` | agent.yaml, state.py |

A webhook, knowledge, mcp or slng tool keeps its own shape. A webhook tool
keeps `input:` and `output:` as JSON Schema.
