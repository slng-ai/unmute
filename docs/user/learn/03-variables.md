# 03. Variables

The agent can look a customer up, but the moment the tool returns, it forgets. **Variables** are typed shared state: named values that live for the whole call and are visible to every agent and task. This is how the agent remembers the customer id it just found, and how, later, one agent passes what it knows to another.

## Declare them in agent.yaml

```yaml
variables:
  customer_id:
    type: string
    source: call_start
    description: CRM id of the caller.
  verified:
    type: boolean
    default: false
```

Each variable has:

- **`type`**: one of `string`, `number`, `boolean`, `integer`. These four primitives are the common ground across all four platforms.
- **`default`** (optional): the starting value. `verified` starts `false`. `customer_id` has no default, so the call must supply it — and because a prompt below names it, unmute would refuse to compile if nothing could.
- **`source`** (optional): where the value comes from. `call_start` for something supplied when the call begins, like a customer id from an outbound dialer or a web session. `conversation` for something the model learns while talking. One of the route values (`to_number`, `call_id`, and friends) for something the runtime owns. Unmute checks every source against the channels you use.
- **`description`** (optional): what the value is, in one line. The model reads it when saving a conversation variable, and it shows up in the compile report.

## Use them in prompts and greetings

A variable is written `{{name}}` (spaces inside the braces are fine). It works in four places: a greeting line, agent and task prompts, a tool's `inject:` values, and a `webhook.path`. Prompts and the greeting are rendered once when the session opens, so they may only name a variable that already has a value by then — one supplied at call start, one the runtime owns, or one with a `default`:

```markdown
Use `get_invoice` to look up invoices for customer `{{customer_id}}`.
```

A greeting can use variables that are known when the call starts:

```yaml
conversation:
  greeting:
    speaks_first: agent
    text: "Hi {{customer_name}}, you have reached Acme Support."
```

## How a variable gets set

Two ways:

- **At call start**, for `source: call_start` variables. Locally that is `unmute dev --var name=value`, one flag per variable, and the same line works on either driver:

  ```sh
  unmute dev examples/salon-support --target pipecat --var customer_name=Ada --var customer_id=cus_2002
  ```

  In production the value rides your target's dispatch payload instead. Drop the flags and each variable falls back to its declared `default`, so the call still runs.
- **During the call, by the model**, for `source: conversation` variables. Declaring one gives you a tool called `update_variables` for free, attached to every agent and task, so the model can save what it hears. You never write that tool yourself.
- **During the call, from a task result**, when a delegated task returns and you map that result into a variable. That is the `assign` step you will meet in [05. Tasks](05-tasks.md).

## Sending a variable to a tool

A tool can receive a variable directly, without the model ever seeing it, through
the tool's `inject:` block. That is how a customer id reaches your API without
the model being able to invent one. If an injected variable is not set yet, the
tool refuses the call and tells the model to ask for it, instead of sending a
half-formed request. See [tools](../reference/tools.md).

Credentials are a separate thing and never travel through `{{...}}`: they are
declared by name in [secrets](../reference/secrets.md).

## What Pipecat generates

On Pipecat, your variables become a typed dataclass, created once and shared across every agent and task in the call:

```python
@dataclass
class State:
    """Typed call variables, shared across agents."""
    customer_id: str = ""
    verified: bool = False

STATE = State()
```

A variable with no default becomes an optional field (for example `customer_id: str | None = None`). Because there is one `STATE` object, when the intake agent learns the customer id, the billing agent sees it too. That shared object is what makes the handoff in the next page carry real information.

## What just got harder

Variables themselves are `core`: typed shared state works on all four platforms. Two honest notes for later:

- The four primitive types are the portable set on purpose. There are no lists, objects, or nested shapes in a variable, because the four platforms do not agree on those.
- Where the state physically lives differs per platform (Pipecat keeps the dataclass above; others use their own store), but you never see that difference. You just read and write named values.

Next: [04. Two agents](04-two-agents.md), where a second agent takes over the call and inherits everything in this state.
