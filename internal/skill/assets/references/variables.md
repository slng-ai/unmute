# Variables and secrets

A variable is a named value that lives for one call. A secret is never a
variable, and the two never mix.

## Keep only the values the call needs

**Keep state small.** Declare a variable when a value must survive a task or
handoff, feed a later tool, or supply a needed call fact to a prompt. Before
adding one used by only one prompt, check whether the agent needs the fact at
all. Prefer fewer values that each represent one useful fact or result; do not
split a timestamp into date and time just because both fields are available.

For a clock, replace `current_date`, `current_weekday`, and `current_time` with
`current_datetime` and `current_weekday`. The timestamp already carries the
date and time. Keep the weekday only if the prompt must name it; reading the
clock's answer avoids asking the model to calculate it. If nobody needs the
weekday, keep just the timestamp.

Merge these entries into an existing package:

```python state.py
class State(BaseModel):
    current_datetime: str = ""
    current_weekday: str = ""
```

```yaml agent.yaml
prefetch:
  - name: local_clock
    clock: now
    timezone: Europe/Madrid
    assign:
      - current_datetime: result.datetime
      - current_weekday: result.day_of_week
```

Use a model for fields that travel together as a task result. Pre-fetch fills
plain values, so it cannot save the clock into a model. Keep separate values
when they have different confirmation steps or different readers.

## How a value moves

Three parts write or read state:

| Key | Think of it as | Lives | Read by |
|---|---|---|---|
| `State` in `state.py` | the call's typed state | the whole call | only prompts and tools that explicitly reference a value |
| `prefetch:` + `assign:` | state filled before the first word | the whole call | the same |
| task `assign:` | fields a task saves when it finishes | the whole call from then on | the same |

Values are supplied at session start, filled by `prefetch:`, or saved at task
finish by `assign:`. Conversation sharing is controlled separately by
`context.history`. A reset task receives no old speech, so put each saved value
it needs directly in its prompt as `{{state.name}}`.

## Declaring a variable

Types, defaults and descriptions live on the `State` class in `state.py`, next
to `agent.yaml`. Each field is one variable. `agent.yaml` has a `variables:`
list only for a value that needs a `source:` or a `confirm:`.

```python state.py
from pydantic import BaseModel, Field


class State(BaseModel):
    customer_name: str = Field(
        "there", description="Caller's first name, used in the greeting and the prompt."
    )
```

```yaml agent.yaml
variables:
  - name: customer_name
    source: call_start
```

| `variables:` key | Required | What it is |
|---|---|---|
| `name` | yes | the field on `State` this entry configures. A name that is no field is refused |
| `source` | no | where the value comes from |
| `confirm` | no | the step that must hear the caller agree before anything acts on this |

The old map form is refused, as is a `type:`, `default:` or `description:` key
and a top-level `shapes:`:

```text
variables: is a list now. Types, defaults and descriptions live on State in state.py; list here only the values that need source: or confirm:, as `- name: caller_phone`
```

An entry with neither `source:` nor `confirm:` is refused too. A field is a
variable whether or not it is listed.

## The State class

`state.py` holds one class named `State`, a Pydantic `BaseModel`. The field's
type checks saved values and defines the task's finish argument.
`Field(description=...)` is what the model reads when a task asks it for the
value, so write it for the model and so it reads whole when the value is empty.
Reasoning for the next author goes in `#` comments.

| Type | Value |
|---|---|
| `str`, `int`, `float`, `bool` | text, a whole number, a number, `true` or `false` |
| `date`, `time` | a calendar date and a time of day. A prompt shows `2026-10-02` and `16:30:00` |
| `Literal["a", "b"]` | one of the strings listed |
| `list[T]` | an array of `T`. Starts as `[]` |
| a `BaseModel` class | an object with that class's fields. Models can hold models and lists |
| `T \| None` | a value of `T` or `null` |
| `EmailStr` | a valid email address |
| `Annotated[str, StringConstraints(pattern=...)]` | text matching the pattern |

Core Pydantic and the standard library types it supports are open. So are three
`pydantic_extra_types` modules and no others:

| Module | Types | Needs |
|---|---|---|
| `phone_numbers` | `PhoneNumber`, `PhoneNumberValidator` | `phonenumbers` |
| `currency_code` | `ISO4217`, `Currency` | `pycountry` |
| `language_code` | `LanguageAlpha2`, `LanguageName`, `ISO639_3`, `ISO639_5` | `pycountry` |

Any other `pydantic_extra_types` import is refused. A sibling import is refused
too. A phone number in E.164 needs a recipe, because `PhoneNumber` alone saves
`tel:+34-600-111-222`:

```python state.py
Phone = Annotated[
    str | phonenumbers.PhoneNumber, PhoneNumberValidator(number_format="E164")
]
```

Pydantic's `NameEmail` is one string in the schema, so a prompt cannot read
`.name` from it. Use a two-field model instead:

```python state.py
class Contact(BaseModel):
    name: str
    email: EmailStr
```

Read one part with `{{state.contact.name}}` or a dotted assignment.

**Every field needs a default.** Use `= None` with `| None`, `= ""` for text and
`= []` for a list. A default its own type refuses is refused: `Literal["a", "b"]
= ""` fails, so write `Literal["a", "b"] | None = None`. An empty referenced
value renders as `none recorded yet.`.

Refused, each naming the field and the fix:

- an alias, or a frozen model
- a `dict` field. Declare its keys as a model
- a union of two types that are not `None`
- a model that holds itself
- a field name that shadows a `BaseModel` attribute, starts with `model_`, or is
  `state`
- a field name the generated class uses: `initial_value`, `is_confirmed`,
  `is_unconfirmed`, `lookup`, `plain`, `render`, `save_batch`, `save_call_start`,
  `save_fact`, `save_result`, `slng_session_id`, `withdraw_confirmation`
- a name that is not lowercase words joined by single underscores, at every depth

### How unmute reads state.py

`unmute validate` and `unmute compile` run `state.py` through `uv` with pinned
Pydantic (2.13.5), email-validator 2.3.0, pydantic-extra-types 2.11.1,
phonenumbers 9.0.40 and pycountry 26.2.16 on Python 3.12, about 0.2 seconds warm.
`uv` must be on `PATH` for any package with a `state.py`, an SLNG-only one
included, and the first run needs network. A package without a `state.py` needs
no `uv`. The generated project gets `state.py` as written, plus a generated
`call_state.py` with `CallState(State)`, which holds the one way a value is
saved, the confirmation marks and the rendering.

| Target | What `state.py` may hold |
|---|---|
| SLNG | a value a task's `assign:` saves, or `source: conversation`: `str`, `int`, `float`, `bool`, a phone number, `EmailStr`, `NameEmail`, `date`, `time`, `Literal[...]`, `list[...]` and BaseModels of these, each optionally `\| None`, with a `Field(description=...)`. A `datetime`, a `dict` or a constrained string is refused. A value the dispatch fills is plain `str` only |
| LiveKit and Pipecat | everything above |
| Twilio | no `state.py`. A package with one is refused |

## How a type is enforced

A type does two jobs, in two places.

**The model is told, not constrained.** The finish argument's schema comes from
the field's type and description. Format and pattern keywords are not sent,
because one target sends its schema with strict mode on and rejects them. The
rule travels as words in the description instead.

**The value is checked where it is saved.** Every save goes through the Pydantic
model: a step's result, a pre-fetched value, a dispatched one and a carrier
fact. The values of one save are checked together. A value that does not fit is
refused, nothing is written, the previous contents stand, and the reason goes
back to the model as the tool result so it corrects itself next turn.

Checking runs in process with no network request. Only a refusal costs a model
request, so a type that refuses something a caller can legitimately say is worth
changing, not a prompt worth rewording. Do not restate a format in a prompt: the
description already carries it. Format checks are not business checks such as
phone ownership or record existence.

## Grouping fields into a model

A pre-fetch cannot fill a model or a list. Use a task to produce grouped fields
and separate scalar variables for clock and lookup results.

```python state.py
class Record(BaseModel):
    id: str
    label: str = Field(description="The name shown to the caller.")


class State(BaseModel):
    selected_record: Record | None = None
```

Use the class as a field's type. `confirm:` goes on the `State` field, never on a
field inside the model.

## Where values come from

| Source | Who supplies it | Availability |
|---|---|---|
| `call_start` | the dispatch payload, or `--var` locally | every channel, before the first word |
| omitted | the dispatch payload if it carries the name, or `--var` locally; otherwise a step's `assign:` | never guaranteed, so write the prompt to read whole while it is still empty, or give its field a default in `state.py` |
| `conversation` | the model, during the call, once the caller has given and confirmed the value | `slng` only: it becomes a runtime variable the platform's `set_runtime_variables` tool fills, returned on the call record as `memory_variables`. Its field is `= None` and has a `Field(description=...)`, because the model reads it. **No `{{state.placeholder}}` either, in any prompt or the greeting.** Refused on `livekit` and `pipecat`, where a step's `assign:` does this job |

**A value the model records reaches no prompt.** It has no value when the prompt
is built, and SLNG rejects a prompt that names one, so writing `{{state.caller_email}}`
for a `source: conversation` variable is refused:

```text
agent.yaml:41: conversation.greeting.text references {{caller_email}}, a value the model
  records during the call, which no prompt receives: describe the value in the variable's
  description: and name it in prose here instead
```

That is the whole way one works: put the detail in `Field(description=...)`, which the
model reads when it fills the value, and refer to the thing in prose ("the email
address you read back") rather than with a placeholder. `examples/hotel-concierge`
is the shape — one runtime variable never written into its prompt, beside four
template variables that are.
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

A value from the dispatch or from the phone adapter is checked against the
field's type in `state.py` when it is saved, the same check a step's `assign:`
gets. A dispatched value that does not fit stops the call before the greeting,
and the error names the field. A phone-adapter fact that does not fit, such as
`anonymous` for an E.164 phone field, is treated as a fact that never arrived.

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
    tool: look_up_account
    # Required on a tool: entry, never defaulted. Answers "does running this
    # unasked, on every call including wrong numbers, change anything".
    writes: false
    args:
      - phone: "{{state.customer_phone}}"
    assign:
      - account_name: result.name
      - account_on_file: result.status
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

### A pre-fetch fills a variable holding one plain value

It resolves before anybody speaks, so all it has is one value: a formatted
clock reading, the number the call carries, one field of a tool result.

Assignable: a plain type, a `date`, a `time`, a checked phone number, an email
address, and a `Literal` when the tool's own result field declares the same set.
Refused: a `list[...]` or a model, naming the step to assign it from instead.

Do not reach for a pre-fetch to seed a list. A list is what a call accumulates
while it runs, one entry per thing that happened, appended by the step that
took it. Nothing has happened when the pre-fetch runs, so there is nothing to
append, and a plain value written there breaks the append later in the call
rather than at compile time.

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
  - name: customer_phone
    confirm: verify_customer
```

Until that step has heard the caller agree, the value:

- renders in **no prompt** except that step's own, refused at compile time
  everywhere else;
- and makes every tool injecting it refuse itself to the model, by name.

The mark clears when the confirming step assigns the value. **Confirmation is
inherited**: a value looked up from an unconfirmed value carries the same
confirming step, because a name found from a number nobody agreed to is exactly as
unconfirmed as that number was.

Write the confirming step's prompt to **read the value back and ask for a yes**,
and to ask from scratch when the value is empty. Both paths, in one prompt.

## How a variable reaches a prompt

Use `{{state.name}}` at the site that needs the value:

| Site | Renders | Can name |
|---|---|---|
| `conversation.greeting.text` | once, at session start | a variable that already has a value |
| an agent's instructions | on entry, and again once one of its own steps records a value | any declared variable |
| a task's instructions | when that task starts | any declared variable |
| a tool's `inject:` value | on every tool call | any declared variable |
| a webhook tool's `path` | on every tool call | any declared variable, URL encoded |

### Naming one part of a value

A `{{state.name}}` placeholder can also name one field inside a variable, with a
dotted path: `{{state.customer.status}}`. The root, the name before the first dot,
follows every rule a whole value already follows at that site: a declared
variable awaiting confirmation renders only in its confirming step's prompt, the
greeting only names something that already has a value, and a secret never
renders.

Each name after the first dot is a field the model of the value before it
declares, so a path can go as deep as the models go.

```python state.py
class Customer(BaseModel):
    phone_number: Phone
    status: Literal["new", "returning", "vip"]


class State(BaseModel):
    customer: Customer | None = None
```

`customer` gets its value from a step's `assign:`, the same as any variable.
A prompt can then read one field of it:

```md
The caller is a {{state.customer.status}} customer, if the lookup has run.
```

Written to read whole before the lookup has run: a part renders the same
empty words the whole value would when the value, or a link on the way, is
absent, `none recorded yet.`, never `None`, never a hole, never an error. A field that is itself a
shape or a list renders as compact JSON; any other field renders as plain
text.

The compiler checks the path before generating anything, naming the file and
the line. Refused:

```
references {{last_appointment.kind}}: last_appointment has no field "kind"; it has scheduled_date, scheduled_time, appointment_type

references {{appointments.scheduled_date}}: appointments is list[Appointment], and a path cannot name a field inside a list: nothing says which entry it means. Record the entry you need into its own variable with assign: on the step that records it, and name that variable here

references {{note.first}}: note is a plain string with no fields to name; write {{state.note}}

references {{caller_phone.digits}}: caller_phone is a string, which has no fields to name; write {{state.caller_phone}}
```

A name `State` does not declare is refused with `which State in state.py does not
declare`.

A bare `{{customer}}` that names a `State` field is refused with `write
{{state.customer}}`. A placeholder carries no logic: no conditions, no filters, no function calls.
Write the value into a sentence and say what to do with it in the
instructions. A structured part already renders as JSON, so there is nothing
left for the placeholder to compute.

Same grammar `assign:` uses for a path into a result or a
shape: see "Picking one part of a structured result" below.

None of the five may name or render a value still awaiting confirmation: see
"A value the caller has to confirm" above for what each site does instead.

Write the sentence so it reads whole when the named value is empty: at every
site but one, an unset variable renders as nothing, never as the word `None`.

The greeting is that one exception. It renders once, before the first word,
with no later turn to catch up on a value that arrives after, so it may only
name a variable that already has a value: `source: call_start`, a system
source, a `default`, or a `prefetch:` entry that assigns it. Naming anything
else there is an error, not a silent empty string:

```
conversation.greeting.text references {{requested_service}}, which has no value when the
prompt is built; give it source: call_start, a system source, or a default
```

An agent's instructions and a task's instructions may name any declared
variable, whether or not anything has assigned it yet. An undeclared name is
an error everywhere, greeting included.

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
  - customer_id: "{{state.customer_id}}"
  - service: "{{state.requested_service}}"
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

The task's finish field is derived from the destination field on `State`. A
successful finish saves it there. Declare the field in `state.py`; do not repeat
the field or type in a task `result:` block.

A `+` on the key appends one entry instead of replacing the value:

```yaml agent.yaml
        assign:
          - appointments+: result.appointment
```

Legal only when the field's declared type is `list[...]`. Refused
otherwise, naming the value and its declared type:

```
assign appends to "customer_phone" with "customer_phone+:", and "customer_phone"
is declared Phone rather than a list. Drop the "+" to replace the value, or
declare it list[...] in state.py so an entry can be added to it
```

Without the `+`, `assign:` replaces the value, exactly as it always has.

### Picking one part of a structured result

The right side of an `assign:` pair does not have to be the whole result. It
can be `result.<field>`, or a dotted path into a model from `state.py`,
`result.<field>.<subfield>`, as deep as the model goes:

```yaml agent.yaml
tasks:
  - name: manage_booking
    assign:
      - appointments+: result.appointment
      - last_booking_day: result.appointment.scheduled_date
```

with `last_booking_day` declared `date | None`.

The picked part's type has to fit the field it lands in: a `date` into a
`date`, a `Literal` into the same `Literal`, a whole model into a field declared
with that model. Refused rather than silently
accepted:

- **a path through a list.** Nothing says which entry to take, so
  `result.appointments.scheduled_date` is refused.
- **a path into a field that is not a model.** Once the path reaches a
  scalar, going one level deeper has nothing left to read.

If a field on the way is optional, `Appointment | None` above, the picked
value may be absent for a visit where the task left it out. Give the field
the same option, `date | None`, so an absent pick is a legal value, or use an
appending assign, `name+:`, which already skips an absent entry instead of
writing one into the list.

The same path form works in a prompt placeholder too: see "Naming one part of
a value" above.

### A value the caller may not give ends in `?`

A step must return every whole value it assigns. A finish that leaves one out,
or sends an explicit null, is refused: nothing is saved, the model is told why
and the caller waits through the retry. Mark a value the step may leave out with
`?` after the result field:

```yaml agent.yaml
assign:
  - callback_time: result.callback_time?
```

Also declare the field `time | None = None` in `state.py`. Do not try to solve
absence in the description: no wording reliably stops a model saying "nothing"
when there is nothing. An empty string is still right for a text value that is
always asked for and may not be known yet. Appending assigns, `notes+:`, are
always optional and drop an absent entry. A finish with a non-empty
`unserved_request` is never refused for a missing value.

## Ordering a step that needs an earlier value

There is no field that holds a step back until a variable exists. Say the
order instead: number the flow in the agent's own instructions, and give the
later step a `when:` clause that names what has to be true first, "once the
caller is verified." See "Order steps with the prompt" in
`references/orchestration.md` for the full pattern, including what a
silently reading tool does with a value that is not there yet.

## Share saved values across a handoff

Saved values remain in call state across a handoff, but the receiving model
sees only values its own prompt references. Put each needed value in that
prompt with `{{state.name}}` or `{{state.name.field}}`.

```yaml
    context:
      history: full
```

Omitting `history` shares spoken messages by default. Use `history: reset` to
share no earlier speech.

## Seeding values locally

```sh
unmute dev ./my-agent --var customer_name=Ada --var customer_id=cus_2002
```

Repeatable, and each value is parsed against the field's type in `state.py`: pass
JSON for a model or a list, quoted for your shell. `--var` is the
local stand-in for the dispatch payload, so it accepts the two kinds of variable
that payload fills: `source: call_start`, and a variable that declares no
`source:` at all. It refuses a runtime-owned source, because that one arrives
from the carrier, not the dispatch:

```
unmute: dev my-agent: --var call_id=abc123: "call_id" has source call_id, so the
  runtime supplies it, not you
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
