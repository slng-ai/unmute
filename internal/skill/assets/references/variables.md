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
| `type` | yes | a type expression. See the type grammar below |
| `source` | no | where the value comes from |
| `default` | no | the value to use when nothing supplies one |
| `confirm` | no | the step that must hear the caller agree before anything acts on this |
| `description` | no | a note; for `conversation` variables the model reads it |

## `shapes:` groups fields into a named type

A top-level list, declared once, each item naming a group of fields a
`type:` can then refer to:

```yaml agent.yaml
shapes:
  - name: Appointment
    description: One thing being booked, moved or cancelled.
    fields:
      - scheduled_date: Date
      - scheduled_time: Time
      - name: appointment_type
        type: Literal["haircut", "haircolor", "haircut_and_haircolor", "dry_cut"]
      - name: calling_reason
        type: Literal["create_booking", "modify_booking", "cancel_booking"]
        description: Why this particular appointment is being touched.
```

| Key | Required | What it is |
|---|---|---|
| `name` | yes | what a `type:` refers to. Written in `CapWords`: it names a generated Pydantic class |
| `description` | no | reaches the model as the class docstring |
| `fields` | yes | one or more fields, in either form below |

**Short form**, one line, a `Pair`:

```yaml
- scheduled_date: Date
```

**Long form**, reached only when the field wants a description:

```yaml
- name: scheduled_date
  type: Date
  description: The day, as the caller gave it.
```

Refused, each with its line:

- `confirm:` on a field. It belongs to the variable whose `type:` names the
  shape, never to a field inside it, so a guard cannot be escaped by naming
  the field one level down.
- A field holding two keys, or none. Same rule as every other pair list in
  this schema.
- The same field name declared twice in one shape.
- A shape with no `name:`, no `fields:`, or a name another shape already
  uses.
- A shape named the same as a primitive, a shaped text type, `Literal`,
  `list`, or `None`. Give it a name of its own, in `CapWords`.
- A shape that refers to itself, directly or through another shape. Nothing
  can render an object with no bottom, and the model would be asked to fill
  one in.

## The type grammar

A variable's `type:` and a task result field's type are both one-line type
expressions, written in Pydantic's own words:

```text
type  := atom ("|" atom)*
atom  := name | name "[" args "]"
args  := arg ("," arg)*
arg   := type | string
```

| Written | Accepted | Write instead |
|---|---|---|
| `str` `int` `float` `bool` | yes | `string` `number` `integer` `boolean` still work, same meaning |
| `Phone` `Date` `Time` `Id` | yes | text with a validated shape; sent to the model as `str`, checked where the value enters the state |
| `Literal["a", "b"]` | yes | a closed set, at least one entry, every entry in double quotes |
| `list[T]` | yes | `T` is any type in this table |
| a declared shape name | yes | must appear under `shapes:` |
| `T \| None` | yes | either side; says the value may be absent |
| `datetime` | no | write `Date` and `Time` as two fields |
| `date` | no | write `Date` |
| `time` | no | write `Time` |
| `UUID` | no | write `Id` |
| `SecretStr` | no | a secret never travels through state; give it a tool's own `*_env` field instead |
| `PaymentCardNumber` | no | outside the declared type scope |
| `dict[...]` / `Dict[...]` | no | declare a shape under `shapes:` and name it here |
| `set[...]` | no | write `list[...]` |
| `tuple[...]` | no | write `list[...]` |
| `List[...]` | no | write `list[...]`, in lower case |
| `Optional[...]` | no | write `T \| None` |
| `Union[...]` | no | write `A \| B`; only `\| None` is meaningful here |
| `Any` | no | name the real type: nothing can render or guard a value with none |
| `BaseModel` | no | name one of the shapes declared under `shapes:` |

Anything else is refused by name, with the line and the column inside the
expression, and the message says what to write instead.

Once a variable's type is anything in this table other than a bare `str`
`int` `float` `bool`, its value is appended automatically to every agent
prompt and every task prompt, as a numbered block after the authored text.
Do not template it into a prompt; the compiler already does. An empty value
renders as the words `none recorded yet.`, never `[]` or `null`. A value
carrying `confirm:` renders only in its confirming step's own prompt, and a
declared secret never renders at all.

## Where values come from

| Source | Who supplies it | Availability |
|---|---|---|
| `call_start` | the dispatch payload, or `--var` locally | every channel, before the first word |
| omitted | the dispatch payload if it carries the name, or `--var` locally; otherwise a step's `assign:` | never guaranteed, so a prompt reads it only through `requires:` or a `default:` |
| `conversation` | the model, through `update_variables` | during the call |
| `session_id`, `carrier`, `connection` | the phone adapter | LiveKit `sip` or `connector` only |
| `call_id`, `direction` | the phone adapter | LiveKit `sip` or `connector`, and both Pipecat Twilio routes |
| `stream_id` | the phone adapter | LiveKit `connector`, and Pipecat `cloud-websocket` |
| `from_number` | the phone adapter | LiveKit `sip` or `connector`, both directions; both Pipecat Twilio routes, inbound calls only |
| `to_number` | the phone adapter | LiveKit `sip` or `connector`, both directions; Pipecat `cloud-websocket`, outbound calls only |

The selected route must prove it supplies a system source: a route that
grants nothing for a fact refuses the variable at validation. This is no
longer one blanket LiveKit-versus-Pipecat rule. `pipecat daily-sip` grants
`call_id`, `direction` and `from_number` (inbound calls only); `pipecat
cloud-websocket` grants those plus `stream_id` and `to_number` (outbound calls
only). Both LiveKit routes grant every fact, in both directions, except that
only `connector` grants `stream_id`. A variable's own `source:` and a
`prefetch: source:` entry read this same grid, so the same fact hydrates
either way on a route that grants it. An inbound code-target phone channel
also requires a default for every `call_start` variable.

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
prefetch:
  - name: today
    clock: now
    # Required on a clock entry, never on the package. A container clock is
    # UTC, so without this the agent names the wrong day for anybody who is
    # not on it.
    timezone: Europe/Madrid
    assign:
      - booking_date: result.date
      - booking_weekday: result.day_of_week

  - name: caller
    source: from_number
    assign:
      - customer_phone: result.value

  - name: profile
    tool: look_up_customer
    # Required on a tool: entry, never defaulted. Answers "does running this
    # unasked, on every call including wrong numbers, change anything".
    writes: false
    args:
      - phone: "{{customer_phone}}"
    assign:
      - customer_name: result.name
      - customer_on_file: result.status
```

`profile` assigns two variables from one lookup. An entry can `assign:` as
many variables as the result has fields, from the one call: no second
request, no second turn.

Every entry carries a `name:` and exactly one source key.

| Source key | Reads | Produces |
|---|---|---|
| `clock: now` | the clock, in the entry's own `timezone:` | `result.date`, `result.time`, `result.datetime`, `result.day_of_week`, `result.year`, `result.timezone` |
| `source: <name>` | a fact the call itself carries | `result.value` |
| `tool: <name>` | one already-declared tool, with `writes:` declared on this entry | `result.<field>` from its `output:` |

`clock: now` is the only value `clock:` accepts. One reading of the clock
produces all six fields above, and `assign:` may name as many of them as the
entry wants.

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
- **`tool:` needs `writes: true` or `writes: false` on the entry.** There is no
  default: a pre-fetch runs unasked on every call, so the build makes you say
  whether that is safe. Webhook and local tools only. `writes:` is refused on
  a `clock:` or `source:` entry, which run no tool.
- **`source:` on the entry, not on the variable.** The variable that receives a
  call fact declares no `source:` of its own. That is what keeps a package
  compiling on a route that supplies no caller ID: the entry skips there, the
  variable keeps its default, and validation warns naming the target and the
  route. Which routes resolve which fact is per fact, not per target: see
  "Where values come from" above. A route that grants nothing for a fact
  refuses it as a variable's own `source:` too, which is the whole reason for
  this rule.

**An entry that resolves nothing is normal.** Skipping is the specified behaviour,
not a failure. The whole block has a two second budget and cannot fail a call: a
lookup that times out or raises is logged and stepped over, and the greeting
happens on time. So **every prompt naming a pre-fetched value has to read as a
whole sentence when that value is empty.**

Denied on the `slng` target: that platform owns session start, so there is no seam
to resolve a fact in.

### `writes:` is a promise, not a guarantee

**The compiler cannot check it.** It checks that you made it. Nothing reads
your handler or your endpoint to see whether it writes; `writes: false` is a
claim about this one use of the tool, and a wrong claim compiles.

It sits on the prefetch entry rather than on the tool because the question is
about this use, not about the tool in general: the same lookup might be safe
to run before a greeting and unsafe to run twice elsewhere. It is required
before `prefetch:` may run a tool, because a pre-fetch runs unasked on every
call: a tool that writes would write on every call, wrong numbers included. So
a lookup that creates a record when it finds none is exactly the tool this
entry must not point at, however convenient. Write a reading tool beside it
and pre-fetch that one instead.

`writes: true` compiles too. It is a declaration, not a request for
permission, and it prints no warning: the entry is named in
`compile-report.json` and in the generated runbook instead.

### A caller's number is best effort

`from_number` and `to_number` resolve less often than the other system
sources, on every route that grants them. A caller can withhold their own
number, and withholding does not arrive as nothing:

- Twilio's own policy is to set it to the word `anonymous`.
- Where an upstream carrier sends a word such as ANONYMOUS or RESTRICTED
  instead, Twilio converts it to keypad digits, which look exactly like a real
  number.
- Some calls simply arrive with the field empty.

Unmute treats all three as absent. A number-valued fact resolves only when it
looks like a plausible E.164 number, a `+` followed by 8 to 15 digits, and a
short list of known digit placeholders is rejected on top of that check.
Either way the entry is skipped, and the log names which entry and why.

On LiveKit `sip`, the number is also absent when the dispatch rule sets
`HidePhoneNumber`. On `pipecat cloud-websocket` it can be missing for a
configuration reason instead: the number rides a `<Parameter>` in the TwiML
Bin the user made, so a Bin created before this existed does not carry it.
Nothing warns about that at compile time, because checking would need carrier
credentials the compiler never asks for.

The same is true in the other direction. An outbound call has no caller, so
the fact worth reading is `to_number`, and on `pipecat cloud-websocket` it has
to be put into the request that places the call:

```yaml agent.yaml
prefetch:
  - name: dialing
    source: to_number
    assign:
      - customer_phone: result.value
```

```xml
<Parameter name="to_number" value="$DEST"/>
```

The number goes into that request twice, once as the number Twilio dials and
once as this parameter, because Twilio substitutes nothing inside an inline
`Twiml=`. Both LiveKit routes need none of this: the worker places the call,
so it already holds the number. Point the user at the generated
`build/<target>/README.md`, which prints the whole request for their own route.

Tell the user to treat the caller's number as best effort on every route that
grants it, not only on Pipecat, and to mark it `confirm:` rather than act on
it unasked.

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
| a task's instructions | when that task starts | a variable that already has a value, or one listed in this task's own `requires:`; a value awaiting confirmation only in the step that confirms it |
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

A task's own instructions may name a variable another task assigns only if
this task's `requires:` lists it too. Naming it without listing it is a
compile error:

```
tasks/booking.md: task "manage_booking" instructions references {{customer_status}}, which only task "verify_customer" assigns. Add customer_status to this task's requires: list, so the step waits for the value and its prompt can read it
```

Listing the name there also holds the task back until the value exists, so the
prompt never renders it empty. Adding the name to a task's own `requires:`
does not help when that same task is the one assigning it: that would wait on
the task's own output, so assign it from an earlier task instead, or give the
variable a default or a `source:`:

```
tasks/verify-customer.md: task "verify_customer" instructions references {{customer_status}}, and "verify_customer" is the only step that assigns it, so the value does not exist while this prompt is being built. Give the variable a default or a source:, or assign it from an earlier step
```

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

A `+` on the key appends one entry instead of replacing the value:

```yaml agent.yaml
        assign:
          - appointments+: result.appointment
```

Legal only when the variable's declared type is `list[...]`. Refused
otherwise, naming the value and its declared type:

```
assign appends to "customer_phone" with "customer_phone+:", and "customer_phone"
is declared Phone rather than a list. Drop the "+" to replace the value, or
declare it list[...] so an entry can be added to it
```

Without the `+`, `assign:` replaces the value, exactly as it always has.

## Requiring a field inside a shape

`requires:` still names a whole value, and can now also name a path into one
declared as a shape:

```yaml agent.yaml
        requires:
          - customer_phone            # a whole value
          - customer.status           # one field inside a shaped value
```

The path is resolved against the declared shape at compile time, so a typo is
refused rather than becoming a guard that can never pass:

```
requires "customer.city" does not resolve: shape "Customer" declares no field
"city". It declares customer_name, customer_id, phone_number, status
```

A path through a `list[...]` is refused too: nothing says which entry it
means. A value awaiting confirmation satisfies no guard through any path
into it, the same as it satisfies none as a whole value.

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

Repeatable, and each value is parsed against the declared type. `--var` is the
local stand-in for the dispatch payload, so it accepts the two kinds of variable
that payload fills: `source: call_start`, and a variable that declares no
`source:` at all. It refuses a runtime-owned source and a `conversation`
source, because neither arrives that way:

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
