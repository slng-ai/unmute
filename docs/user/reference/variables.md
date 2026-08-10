# Reference: variables

`variables` is typed shared state: named values that live for the whole call and are visible to every agent and task. Task results, handoff payloads, and personalization all flow through it. See the [variables learn page](../learn/03-variables.md) for the walkthrough.

```yaml
variables:
  customer_id:
    type: string
  verified:
    type: boolean
    default: false
```

Variables are `core`: typed shared state works on all four targets. One driver note to keep in mind: on Deepgram live state lives in the generated bridge (template variables there are substitution-time only and visible to project members, so never route secrets through them).

## Fields

### type

The value's type. These four primitives are the common ground across all four platforms; there are no lists or nested shapes.

Required: yes. Values: `string | number | boolean | integer`. Default: none. Targets: all four, core.

An enum result field from a [task](tasks.md) assigns into a `string` variable.

### default

The starting value. A variable with no default starts empty.

Required: no. Values: a value of the declared `type`. Default: none. Targets: all four, core.

### source

Marks a variable that must be supplied when the call starts, for example a customer id passed by an outbound dialer or a web session.

Required: no. Values: `call_start | session_id | carrier | connection | call_id | stream_id | direction | from_number | to_number`. Default: none. Targets: all four, core.

Unmute checks every source against the selected route. An outbound start request
must provide each non-defaulted `call_start` variable. An inbound channel can
use `call_start` only with a default. System sources come from authenticated
route metadata before the greeting; variable names never imply a source.

## How variables change

- **At call start**, for `source: call_start` variables.
- **During the call**, through a task delegate's `assign`, which maps a task's typed result field into a variable. See [controls](controls.md) and [tasks](tasks.md).

Reference a variable in prompts and greeting text with `{{name}}` (no spaces inside the braces).
