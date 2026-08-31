# Variables and secrets

A variable is a named value that lives for one call. A secret is never a
variable, and the two never mix.

## Declaring a variable

```yaml agent.yaml
variables:
  customer_name:
    type: string
    source: call_start
    default: there
    description: Caller's first name, used in the greeting and the prompt.
```

| Field | Required | What it is |
|---|---|---|
| `type` | yes | `string`, `number`, `integer`, or `boolean` |
| `source` | no | where the value comes from |
| `default` | no | the value to use when nothing supplies one |
| `description` | no | a note; for `conversation` variables the model reads it |

## Where values come from

| Source | Who supplies it | Availability |
|---|---|---|
| `call_start` | the dispatch payload, or `--var` locally | every channel, before the first word |
| `conversation` | the model, through `update_variables` | during the call |
| `session_id`, `call_id`, `direction`, `from_number`, `to_number`, `carrier`, `connection` | the phone adapter | LiveKit `sip` or `connector` only |
| `stream_id` | the phone adapter | LiveKit `connector` only, not `sip` |

The selected route must prove it supplies a system source. **No Pipecat route
grants a call-source variable today**, `daily-sip` and `cloud-websocket` alike:
naming one on a Pipecat target is refused at validation. An inbound code-target
phone channel also requires a default for every `call_start` variable.

Declaring any `source: conversation` variable creates a tool called
`update_variables` in the generated project. You do not write it and you do not
list it. The name is reserved for that generated tool. The model calls it when
the caller says something worth keeping.

## Where a variable can be used

`{{name}}` renders a variable. Two kinds of site, with different timing, and the
difference is the thing people get wrong.

| Site | Renders | Can name |
|---|---|---|
| `conversation.greeting.text` | once, at session start | a variable that already has a value |
| an agent's instructions | once, at session start | a variable that already has a value |
| a task's instructions | when that task starts | a variable that already has a value, or one assigned from a task result |
| a tool's `inject:` value | on every tool call | any declared variable |
| a webhook tool's `path` | on every tool call | any declared variable, URL encoded |

"Already has a value" means `source: call_start`, a system source, or a
`default`. Naming anything else in a session start site is an error, not a
silent empty string:

```
conversation.greeting.text references {{requested_service}}, which has no value when the
prompt is built; give it source: call_start, a system source, or a default
```

An undeclared name is an error too.

## Passing a value into a tool without the model seeing it

```yaml tools/book_appointment.yaml
input:
  type: object
  properties:
    slot_id:
      type: string
  required:
    - slot_id

inject:
  customer_id: "{{customer_id}}"
  service: "{{requested_service}}"
```

`inject` values are not part of the model's schema, so the model can neither see
them nor overwrite them. An `inject` key that also names an `input` property is
a compile error, for exactly that reason.

`inject` is legal on `webhook` and `local` tools only: the two kinds whose
request Unmute builds itself. An MCP server owns its own call shape, so there is
nothing to merge into.

When an injected variable has no value at call time, the tool refuses rather
than sending a half formed request, and the model is told to ask:

```
cannot call book_appointment yet: requested_service not set. Ask the caller first.
```

## Getting a value out of a task

```yaml agent.yaml
delegates:
  check_customer:
    task: customer_record
    assign:
      customer_id: result.customer_id
```

The task's typed result lands in the variable, and the rest of the call uses it
without asking again. Declare the variable at the top level for it to land in.

## Carrying variables through a handoff

```yaml
    context:
      history: full
      variables: all
```

`variables` takes `all` or a list of names. Without it, the caller gets asked
for their phone number twice. This is a decision, so make it on purpose and say
which you chose.

## Seeding values locally

```sh
unmute dev ./my-agent --var customer_name=Ada --var customer_id=cus_2002
```

Repeatable, and each value is parsed against the declared type. `--var` accepts
`call_start` variables only, because it is the local stand-in for the dispatch
payload:

```
unmute: dev my-agent: --var requested_service=haircut: "requested_service"
  has source conversation, so the model saves it mid-call through update_variables, not you
```

## Secrets

A secret is never a variable and never a literal in a package.

```yaml agent.yaml
secrets:
  - OPENAI_API_KEY
  - SLNG_API_KEY
  - SALON_API_TOKEN
```

A list of `UPPER_SNAKE` environment variable **names**. There is no field
anywhere in the schema that takes a key, a token, or a phone number as a value,
and the compiler refuses one.

A secret reaches a call only through an environment lookup. Declare every name
the package owns: model provider keys, tracing keys, model `endpoint_env`, tool
`*_env` fields, connection `environment:` values, `destinations:` values, and
names read with `os.environ` in a local handler. The compiler also knows some
runtime or platform names that are not author declarations; the generated
runbook says who supplies those.

**Secrets never flow through `{{...}}` templates.** Every template site renders
into something spoken, prompted, traced, or logged, so a secret in one would end
up in a transcript. Writing one is refused:

```
agent.yaml:70: conversation.greeting.text references {{OPENAI_API_KEY}}, but secrets never
  flow through templates; a secret reaches a tool through its own *_env field
```

Every name must be a valid shell identifier: letters, digits, and underscores,
never starting with a digit. A deployment platform exports secrets through a
shell, so a name starting with a digit would be silently missing at run time.
The compiler refuses it first.

If a user pastes a real key into the conversation, do not write it into any
file. Put its name in `secrets:` and tell them to set the value in their
environment or their secret store.

## Phone numbers are secrets too

```yaml agent.yaml
destinations:
  billing_line: BILLING_PHONE_NUMBER
  supervisor_line: SUPERVISOR_PHONE_NUMBER
```

A destination is the name of an environment variable holding an E.164 number or
a `sip:` URI, read at call time. A number written there is refused, because
`agent.yaml` is the portable half of a package. The model never sees a number
and cannot dial one that is not listed.
