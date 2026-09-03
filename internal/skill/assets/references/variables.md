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
| `confirm` | no | the step that must hear the caller agree before anything acts on this |
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

## Resolving a value before the call starts

`prefetch:` names the facts that are knowable before the greeting and resolves
them once per call, so the model never spends a turn discovering them. **An
ordered list. Entries resolve top to bottom, and the file's order is the agent's
order.**

```yaml agent.yaml
# Required before any entry can read the clock. A container clock is UTC, so
# without this the agent names the wrong day for anybody who is not on it.
timezone: Europe/Madrid

prefetch:
  - name: today
    clock: date
    assign:
      - booking_date: result.date

  - name: caller
    source: from_number
    assign:
      - customer_phone: result.value

  - name: profile
    tool: look_up_customer
    args:
      - phone: "{{customer_phone}}"
    assign:
      - customer_name: result.name
```

Every entry carries a `name:` and exactly one source key.

| Source key | Reads | Produces |
|---|---|---|
| `clock: date` | the package clock, in `timezone:` | `result.date` |
| `source: <name>` | a fact the call itself carries | `result.value` |
| `tool: <name>` | one already-declared read-only tool | `result.<field>` from its `output:` |

`tool:` is the general case. Any read-only tool qualifies when every argument is
something the package already holds: a fixed value, a call fact, the clock, or a
value an earlier entry assigned. An account record, a price list, a rota, a
caller's last order: if the prompt would otherwise tell the model to call it
first, pre-fetch it instead. A tool whose arguments depend on what the caller says
cannot be pre-fetched.

**Write these as lists, not maps.** `assign:` and `args:` take **one pair per
item**: `- customer_phone: result.value`, each on its own `- ` line. Writing them
as a mapping is refused, and so is an item holding two pairs, which is what a
dropped indent produces:

```yaml
# Refused. Two keys in one item.
assign:
  - customer_name: result.name
    customer_id: result.id
```

Three rules that catch most first attempts:

- **Order matters and is not fixed for you.** An entry reading a value that a
  *later* entry assigns is refused, naming both entries and telling you which one
  to move up. Reading a value an *earlier* entry assigned is the intended shape.
- **`tool:` needs `read_only: true` on the tool.** A pre-fetch runs unasked on
  every call, so a tool that writes would write on every call, wrong numbers
  included. Webhook and local tools only.
- **`source:` on the entry, not on the variable.** The variable that receives a
  call fact declares no `source:` of its own. That is what keeps a package
  compiling on a route that supplies no caller ID: the entry skips there, the
  variable keeps its default, and validation warns naming the target and the
  route. A call fact resolves on LiveKit `sip` and `connector` and nowhere else.
  Declaring the same fact as a variable's own `source:` is a hard error on a
  Pipecat target rather than a warning, which is the whole reason for this rule.

**An entry that resolves nothing is normal.** Skipping is the specified behaviour,
not a failure. The whole block has a two second budget and cannot fail a call: a
lookup that times out or raises is logged and stepped over, and the greeting
happens on time. So **every prompt naming a pre-fetched value has to read as a
whole sentence when that value is empty.**

Denied on the `slng` target: that platform owns session start, so there is no seam
to resolve a fact in.

## A value the caller has to confirm

Some facts arrive as proposals. A caller's number comes from the carrier, and the
caller may be ringing from a friend's phone or may hold a second account, so
acting on it unasked is wrong.

```yaml agent.yaml
variables:
  customer_phone:
    type: string
    default: ""
    confirm: verify_customer
```

Until that step has heard the caller agree, the value:

- satisfies no `requires:` guard, so a step needing it does not start;
- renders in **no prompt** except that step's own, refused at compile time
  everywhere else;
- and makes every tool injecting it refuse itself to the model, by name.

The mark clears when the confirming step assigns the value. **Confirmation is
inherited**: a value looked up from an unconfirmed value carries the same
confirming step, because a name found from a number nobody agreed to is exactly as
unconfirmed as that number was.

Write the confirming step's prompt to **read the value back and ask for a yes**,
and to ask from scratch when the value is empty. Both paths, in one prompt.

## Where a variable can be used

`{{name}}` renders a variable. Two kinds of site, with different timing, and the
difference is the thing people get wrong.

| Site | Renders | Can name |
|---|---|---|
| `conversation.greeting.text` | once, at session start | a variable that already has a value, and never one awaiting confirmation |
| an agent's instructions | once, at session start | a variable that already has a value, and never one awaiting confirmation |
| a task's instructions | when that task starts | a variable that already has a value, or one assigned from a task result; a value awaiting confirmation only in the step that confirms it |
| a tool's `inject:` value | on every tool call | any declared variable; one awaiting confirmation makes the call refuse itself until it is settled |
| a webhook tool's `path` | on every tool call | any declared variable, URL encoded |

"Already has a value" means `source: call_start`, a system source, a `default`, or
a `prefetch:` entry that assigns it. Naming anything else in a session start site
is an error, not a silent empty string:

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
agents:
  appointment_desk:
    tasks:
      - name: customer_record
        assign:
          - customer_id: result.customer_id
```

The task's typed result lands in the variable named by `assign:`, and the rest
of the call uses it without asking again. Declare the variable at the top level
for it to land in.

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

To stand in for a **caller ID**, use `--source`, not `--var`:

```sh
unmute dev ./my-agent --source from_number=<a number in E.164>
```

It seeds the **call fact**, which a `prefetch:` entry then reads, so the run
exercises the pre-fetch, the confirmation marking and the read-back. Only the eight
facts a call carries are accepted. On a real call the carrier's own value wins: a
seed only fills what the route supplied nothing for.

**Do not reach for `--var` here.** Seeding the variable directly writes the value,
skips the pre-fetch, marks nothing as awaiting confirmation, and lets a local run
act on a number it never read back. The run would pass a path a real call fails,
which is worse than having no local path at all.

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
