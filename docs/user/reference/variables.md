# Reference: variables

`variables` is typed shared state: named values that live for the whole call and are visible to every agent and task. Task results, handoff payloads, and personalization all flow through it. See the [variables learn page](../learn/03-variables.md) for the walkthrough.

```yaml
variables:
  customer_id:
    type: string
    source: call_start
    description: CRM id of the customer this call is about.
  dialed_number:
    type: string
    source: to_number
  reschedule_to:
    type: string
    source: conversation
    description: New slot the customer asks for, in spoken form.
```

There are three kinds, and the `source` field is what tells them apart:

- **input variables** (`source: call_start`) arrive with the call, from an outbound dispatch or a web session.
- **system variables** (`source: to_number` and the other route values) are filled by the runtime.
- **conversation variables** (`source: conversation`) are saved by the model during the call.

Variables are `core`: typed shared state works on all four targets. One driver note to keep in mind: on Deepgram live state lives in the generated bridge (template variables there are substitution-time only and visible to project members, so never route secrets through them).

## Fields

### type

The value's type. These four primitives are the common ground across all four platforms; there are no lists or nested shapes.

Required: yes. Values: `string | number | boolean | integer`. Default: none. Targets: all four, core.

An enum result field from a [task](tasks.md) assigns into a `string` variable.

### default

The starting value. A variable with no default starts empty.

Required: no. Values: a value of the declared `type`. Default: none. Targets: all four, core.

### description

What the value is, in one line. It is shown to the model in the generated `update_variables` tool, and it appears in the compile report and the refusal message when a tool needs a value it does not have yet.

Required: no. Values: text. Default: none. Targets: all four, core.

### source

Marks a variable that must be supplied when the call starts, for example a customer id passed by an outbound dialer or a web session.

Required: no. Values: `call_start | conversation | session_id | carrier | connection | call_id | stream_id | direction | from_number | to_number`. Default: none. Targets: all four core, except `conversation`, which works on LiveKit and Pipecat and fails on Vapi and Deepgram.

`conversation` marks a value the model learns while talking. Declaring one makes unmute generate a tool called `update_variables` whose parameters are exactly your conversation variables, attached to every agent and task, so the model has a typed way to save what it hears. You do not list it in any `tools:` block, and the name is reserved.

Unmute checks every source against the selected route. An outbound start request
must provide each non-defaulted `call_start` variable. An inbound channel can
use `call_start` only with a default. System sources come from authenticated
route metadata before the greeting; variable names never imply a source.

## How variables change

- **At call start**, for `source: call_start` variables. Locally, pass them with `unmute dev --var name=value` (repeatable); in production they ride the target's own dispatch payload as one flat JSON object.
- **During the call**, through the generated `update_variables` tool for `source: conversation` variables, or through a task delegate's `assign`, which maps a task's typed result field into a variable. See [controls](controls.md) and [tasks](tasks.md).

## Using a variable

Write `{{name}}` (spaces inside the braces are fine). It works in exactly four places:

| Place | Rendered | Notes |
|---|---|---|
| `conversation.greeting.text` | once, at session start | May only name a variable that has a value by then. |
| agent and task instructions | once, at session start | Same rule. Prompts are never re-rendered mid-call. |
| tool `inject:` values | at each tool call | A conversation variable is fine here. |
| `webhook.path` | at each tool call | Substituted values are URL-encoded. |

"Has a value by then" means an input variable, a system variable, or any variable with a `default`. A conversation variable with no default fails to compile in a prompt, because there would be nothing to say. If a tool needs a value that is still unset when the model calls it, the tool refuses and tells the model what to ask the caller for, rather than sending a half-formed request.

A token that does not name a declared variable is a compile error, so a typo is caught before the call. Secrets never work here: see [secrets](secrets.md).
