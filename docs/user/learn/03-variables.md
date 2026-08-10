# 03. Variables

The agent can look a customer up, but the moment the tool returns, it forgets. **Variables** are typed shared state: named values that live for the whole call and are visible to every agent and task. This is how the agent remembers the customer id it just found, and how, later, one agent passes what it knows to another.

## Declare them in agent.yaml

```yaml
variables:
  customer_id:
    type: string
  verified:
    type: boolean
    default: false
```

Each variable has:

- **`type`**: one of `string`, `number`, `boolean`, `integer`. These four primitives are the common ground across all four platforms.
- **`default`** (optional): the starting value. `verified` starts `false`. `customer_id` has no default, so it starts empty until something sets it.
- **`source`** (optional): set it to `call_start` for a value that must be supplied when the call begins, for example a customer id passed in by an outbound dialer or a web session. Unmute checks that every `call_start` variable can actually be supplied on the channels you use.

## Use them in prompts and greetings

A variable is written `{{name}}`, with no spaces inside the braces. Anywhere a prompt or a greeting line can reference one, it is substituted at runtime:

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

- **At call start**, for `source: call_start` variables.
- **During the call**, when a delegated task returns a result and you map that result into a variable. That is the `assign` step you will meet in [05. Tasks](05-tasks.md). It is the main way state changes mid-call.

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
