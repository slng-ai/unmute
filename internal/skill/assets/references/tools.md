# Tools

A tool is two things: a contract the model sees, and something that runs when
the model calls it. Both live in one file, `tools/<name>.yaml`.

## The shape of a tool file

```yaml tools/check_availability.yaml
description: >-
  List Sage and Stone slots for one service and date. Call only after customer
  identification succeeds. This tool accepts only service and date.

input:
  type: object
  properties:
    service:
      type: string
      enum:
        - haircut
        - hair-color
        - blowout
    date:
      type: string
      description: Preferred date in YYYY-MM-DD form
  required:
    - service
    - date

local:
  handler: tools/check_availability.py
```

The top is the contract. `description` and `input` are everything the model
knows about this tool, so write the description as an instruction rather than a
label, and let the schema do real work. The `enum` above means the model cannot
ask for a service the salon does not offer.

The schema is the complete argument list. Keep workflow prerequisites in the
agent or task prompt; do not make their names look like extra tool inputs in the
description. Generated Pipecat direct tools return a corrective result when a
provider adds an undeclared argument, or when a handler fails before returning,
so the model can retry instead of leaving the call stuck in progress.

The block near the bottom says how the tool runs. **Every tool file has exactly
one execution block.** Two is an error, and none is an error whose message is
also the list of what you could have written.

## The six execution blocks

| Block | The tool is | Reach for it when |
|---|---|---|
| `webhook:` | an HTTP call to a URL named by an environment variable | the user already has an API. This is the everyday case |
| `local:` | a Python function in the package | the call needs code of your own: a signature, a transform, a fixture |
| `mcp:` | a remote MCP server that offers its own tools | the user names a server and wants what it exposes |
| `builtin:` | a tool the runtime already has, selected by id | you want `end_call`, which is the only one |
| `client:` | a tool the caller's own application fulfils | never yet. Gated, see below |
| `provider_hosted:` | a tool the model provider runs itself | never yet. Gated, see below |

### Which fields each block allows

| Field | Required | Legal on |
|---|---|---|
| `description` | yes, except on `builtin:` and `mcp:` | everywhere else |
| `input` | yes, except on `builtin:` and `mcp:` | everywhere else |
| `output` | no | everywhere except `builtin:` and `mcp:` — but see below |
| `inject` | no | `webhook:` and `local:` only |
| `interruption` | no | everywhere except `mcp:` |
| `effect` | no | everywhere except `mcp:` |
| `announce` | no | `webhook:` and `local:` only |

An `mcp:` file is the block and nothing else, because the server owns each
tool's contract. A `builtin:` file needs no `description` or `input`, because
the registry supplies both.

**`output:` is author-side documentation, not a contract with the model.** The
compiler checks that it is a JSON Schema object, but no generator sends it to
the model, puts it in the compile report, or enforces it at run time. The
generated wrapper returns whatever the endpoint or handler returned, unshaped.

### The two gated blocks

`client:` and `provider_hosted:` exist in the schema and no target emits them.
Writing one fails with the target named:

```
livekit: LiveKit client tools are not proven by its driver
```

Do not write one, and do not offer one as an option. They are listed here so
that a refusal a user meets reads as a decision rather than a bug.
If you need to show the refusal, YAML requires `client: {}` or
`provider_hosted: {}`; a bare empty block is itself invalid.

## Webhook tools

```yaml tools/confirm_appointment.yaml
description: Confirm that the existing appointment stays as booked. Call it when the customer says the time works.

input:
  type: object
  properties: {}

inject:
  customer_id: "{{customer_id}}"
  channel: phone

webhook:
  url_env: SALON_API_URL
  path: /customers/{{customer_id}}/appointments/confirm
  auth:
    type: bearer
    token_env: SALON_API_TOKEN

effect: returns_data
interruption: provider_default
```

| Field | Required | What it is |
|---|---|---|
| `url_env` | yes | the `UPPER_SNAKE` name of a variable holding the base URL |
| `path` | no | starts with `/`, is appended to that base URL, and may carry `{{variable}}` tokens |
| `auth` | no | how the request authenticates |

`url_env` holds a **name**, never a URL. Writing the address there is refused.
That is what lets staging and production run the same package against different
APIs.

Both names — the `url_env` and any `auth.token_env` — also go in the package's
top-level `secrets:` list. That is a separate file from this one, and forgetting
it is a warning at exit 0 rather than an error, so it is easy to miss. See
`package.md`.

`path` renders per call and the rendered value is URL encoded for you. Because
it renders per call rather than at session start, a variable the conversation
itself filled in is fine here. A token naming nothing at all fails at compile
time.

**`path` templates a declared variable, never an `input` property.** These are
two different things and mixing them up is the most common webhook mistake:

```yaml
input:
  type: object
  properties:
    tracking_number:
      type: string

webhook:
  url_env: COURIER_API_URL
  path: /tracking/{{tracking_number}}   # WRONG: that is an input property
```

```
tools/track_parcel.yaml:19: tool "track_parcel" webhook.path references {{tracking_number}},
  which is not a declared variable
```

**Every `input` property is sent as the JSON request body already.** So the
usual fix is to delete the template and keep a fixed path:

```yaml
webhook:
  url_env: COURIER_API_URL
  path: /tracking
```

The API then receives `{"tracking_number": "..."}` in the body. Only put a
`{{name}}` in the path when `name` is in the package's top-level `variables:`
block, and say to the user which shape the request ended up with, because they
may need to change their endpoint to match.

Every `inject` value must be a scalar: a string, number, boolean, or null. Maps
and lists are refused.

### Authentication

| Field | What it is |
|---|---|
| `type` | `bearer` or `api_key` |
| `token_env` | environment variable holding the token |
| `header` | header name, `api_key` only, defaults to `X-API-Key` |

Those two schemes are the whole list in this version. If the user's API needs a
signed request, an OAuth exchange, or mutual TLS, a webhook tool cannot do it.
Say so and write a Python handler instead.

## Python tools

Two files that go together: the tool file and the handler beside it.

```yaml tools/cancel_appointment.yaml
description: Cancel the appointment outright. Call it only when the customer says plainly that they want to cancel, never when they want a different time.

input:
  type: object
  properties: {}

inject:
  customer_id: "{{customer_id}}"

output:
  type: object
  properties:
    cancelled:
      type: boolean
    customer_id:
      type: string
  required:
    - cancelled
    - customer_id

local: {}
```

```python tools/cancel_appointment.py
def cancel_appointment(customer_id):
    return {"cancelled": True, "customer_id": customer_id}
```

This is the self-contained fixture from `examples/outbound-reminder`; its live
test exercises outbound calling without requiring a booking API.

The rules the function follows:

| Rule | Why |
|---|---|
| the function name matches the tool name | that is how the generated code finds it |
| its parameters match the `input` properties plus the `inject` keys | the call is built from both |
| it returns the value your description and prompt expect | the result goes back to the model; `output` is not enforced |
| it may be `async def` | the generated code awaits an awaitable result |
| it imports nothing from Unmute | the generated project does not depend on Unmute at run time |

**An optional `input` property is always passed, as an empty string.** The
generated call is by keyword every time, so a Python default in your handler is
dead code — it receives `""`, not `None` and not your default. Write
`def check(date, part_of_day="")` and treat `""` as "not given". A handler that
tests `if part_of_day is None:` compiles clean and misbehaves on the first call,
and neither `validate` nor `compile` will say a word about it.

A handler reaches a credential through `os.environ`, and the variable name goes
in `secrets:` like any other. Literal lookups are also inferred for generated
environment instructions and startup checks.

`unmute compile` copies the file into the generated project and imports it as a
plain module.

The `local.handler` field is optional. When it is absent, Unmute uses
`tools/<tool-name>.py`, so the example above resolves to
`tools/cancel_appointment.py`.

## MCP servers

One block, and nothing else in the file.

```yaml tools/web_search.yaml
mcp:
  url_env: FIRECRAWL_MCP_URL
  transport: streamable_http
  auth:
    type: bearer
    token_env: FIRECRAWL_API_KEY
  tools:
    - firecrawl_search
```

| Field | Required | What it is |
|---|---|---|
| `url_env` | yes | the `UPPER_SNAKE` name of the variable holding the server address |
| `transport` | no | `sse` or `streamable_http` |
| `auth` | no | `bearer` with `token_env`, or `api_key` with `token_env` and an optional `header` |
| `tools` | no | non-empty, unique server tool names to offer; absent means all of them |

`url_env` is a name, never an address. `transport` is optional because both
platforms guess it from the URL: a path ending in `/mcp` is streamable HTTP,
anything else is SSE. Write it when you want the choice visible rather than
inferred. Any other value is refused with both legal ones named.

Listing specific `tools:` is usually right. A whole server dropped into an
agent's tool list is a large, unreviewed surface, and the model will use all of
it.

MCP sources are required at runtime. Pipecat connects them during bot setup;
LiveKit probes every source before `AgentSession.start`, always attempts to close
every created probe, and mounts a fresh client on the agent or task. A connection
or tool-list error stops the session before it greets the caller on either
target. A LiveKit probe close error also stops startup. Pipecat cleanup errors
surface during teardown after every close has been attempted.

With Langfuse tracing enabled, Pipecat MCP calls emit finite `tool:<name>` spans with tool arguments and, when completed, the result.
Pipecat refuses to start when an agent tool, task function, or MCP source on the same agent exposes the same name.
Pipecat 1.7 has one cleanup limit: cancellation during `MCPClient.start()` may leave a partial transport open until async-generator or process cleanup; cancellation after `start()` returns is cleaned up normally.

## Prebuilt tools

```yaml tools/end_call.yaml
description: "End the call when the caller is finished or says goodbye."
builtin:
  id: end_call
  instructions: Thank the caller briefly, then end the call.
```

**The registry is closed and has one row.**

Builtin ids: `end_call`. `builtin.instructions` is optional and tells the model
what to do as the prebuilt runs without changing its fixed behavior.

| id | Effect | Default description |
|---|---|---|
| `end_call` | `ends_conversation` | End the call when the caller is finished or says goodbye. |

There is no plugin seam, and you cannot add to it from a package. Do not invent
a builtin id: an unknown one is refused by name.

If what the user wants is not `end_call`, it is usually a webhook, a Python
handler, or an MCP server. **One thing it is not is a tool at all:** handing the
caller to a person is a `human_transfer` control at the top level of
`agent.yaml`, not a file in `tools/`. See `transfers.md`, and check there first,
because on a browser-only package a transfer is not possible at all.

The registry decides the effect and the parameters for you. Writing an `effect`
that disagrees fails rather than being ignored, and a `builtin:` file takes no
`input`, `output`, `handler`, or `url_env`. Leave `description` out and the
registry default is used.

Add `end_call` to every agent that answers a phone. `unmute init` scaffolds it
for exactly that reason.

## Hidden values the model cannot see

```yaml
inject:
  customer_id: "{{customer_id}}"
  channel: phone
```

`inject` is a flat map merged into the call and never advertised to the model,
so the model can neither see the value nor overwrite it. An `inject` key that
also names an `input` property is a compile error, for that reason.

Legal on `webhook:` and `local:` only. An MCP server owns its own call shape, so
there is nothing to merge into.

When an injected variable has no value at call time, the tool refuses instead of
sending a half formed request, and the model is told what to ask for.

Use `inject` for anything the caller should not be able to change: the customer
id, the channel, a tenant. A parameter in `input` is a parameter the model can
invent.

## The three behaviour fields

```yaml
interruption: provider_default
effect: returns_data
announce: Let me check the calendar.
```

| Field | Values | Default | Meaning |
|---|---|---|---|
| `interruption` | `provider_default`, `continue`, `cancel` | `provider_default` | what happens to the call if the caller speaks while the tool runs |
| `effect` | `returns_data`, `ends_conversation` | `returns_data` | whether the conversation continues after the tool |
| `announce` | any one sentence | absent, nothing is spoken | a fixed line the agent speaks as the tool starts, so a slow call is not silence |

All three are honoured differently per target. Pipecat maps `interruption` onto
its own cancel-on-interruption setting. LiveKit runs tools to completion, so a
non-default value warns there. Read the warning to the user rather than dropping
it.

### When to write `announce:`

Write it on a tool that keeps the caller waiting: a webhook to a slow service, a
handler that reads a calendar or a database. Do not write it on a fast tool, and
do not write one on every tool. Two agents talking over each other is worse than
a short pause.

The sentence is fixed, so it is spoken word for word every time that tool runs.
Write what the agent is **doing**, never what it expects to find, and keep it
shorter than the wait it covers:

| Write this | Not this |
|---|---|
| `Let me check the calendar.` | `Let me find you some great times!` |
| `One moment while I look that up.` | `I'm querying the availability API.` |

If the package instructions already tell the agent to say it is checking
something, remove that instruction when you add `announce:`. Otherwise the model
speaks its own version and the tool speaks the fixed one, and the caller hears
both. This is the most common way to get it wrong.

Rules that will fail the compile if you break them:

- Legal on `webhook:` and `local:` only. Every other kind has no body to speak
  before. An `mcp:` file is refused at load with the line number.
- A fixed sentence. `{{variables}}` are refused, because a rendered line would
  need a round trip, which is the delay the field exists to hide.
- A blank value reads as absent. Nothing is spoken and nothing is emitted.
- On Pipecat, list an announcing tool on an **agent**, not on a task. A task tool
  is emitted as a flows handler with no seam to speak from, so it is refused by
  name. LiveKit emits the same line in either place.
- A target whose driver has no lowering for the field fails validation with that
  driver's own reason. Read the error to the user rather than dropping the field.

Nothing waits for the line to finish playing, on either driver. The tool's own
work starts straight away, and the tool's `interruption:` value still decides
what happens if the caller speaks over it.

## Define once, attach by name

**Define each tool once.** The full definition exists only in
`tools/<name>.yaml`: `description`, `input`, optional `output` and `inject`, and
one execution block. A local handler lives beside it in `tools/<handler>.py`.
Do not put any of those fields in `agent.yaml`.

Every `tools:` entry in `agent.yaml` is a string name:

- the top-level list loads `tools/<name>.yaml`,
- `agents.<name>.tools` grants an agent access, and
- `tasks.<name>.tools` grants a task access.

```yaml agent.yaml
tools:
  - check_availability
  - end_call

agents:
  appointment_desk:
    instructions: instructions.md
    model: reasoning
    voice: voice
    tools:
      - check_availability
      - end_call
```

For a task-scoped tool, attach the same loaded name to the task instead:

```yaml agent.yaml
tasks:
  find_slot:
    instructions: tasks/find-slot.md
    tools:
      - check_availability
    result:
      summary: string
    context:
      history: full
```

The agent and task lists are visibility scopes. Attach a tool only where it is
called; do not grant it to both unless both really call it. Never replace a
name with an inline mapping of `description`, `input`, `output`, `local`, or
`webhook`.

**Task `result:` and tool `output:` are different contracts.** A tool's optional
`output:` describes one tool call and stays in `tools/<name>.yaml`. A task's
required `result:` describes what the whole delegated task returns to its caller
after any tool calls. It may select or combine tool data, so design it for what
the caller needs instead of copying a tool output schema by default.

A file in `tools/` that the package level list does not name is not loaded at
all, and nothing complains. When a tool is never offered, check that list first.

Splitting tool lists is how you make a wrong action impossible rather than
discouraged. In `examples/subagents`, only the appointment manager holds
`cancel_appointment`, so no caller can talk the booking agent into a
cancellation.

## Choosing a kind, from a plain English ask

| The user says | Write |
|---|---|
| "call our booking API" | `webhook:` with `url_env` and `auth` |
| "our API needs a signed request" | `local:`, because webhook auth is bearer and api_key only |
| "look something up in this spreadsheet of ours" | `local:`, and say the handler is a fixture unless they wire it up |
| "use the Firecrawl MCP server" | `mcp:` with `tools:` naming what it may use |
| "let it hang up" | `builtin:` with `id: end_call` |
| "let the caller's app do it" | nothing yet. `client:` is gated on every target. Say so |

When it could be a webhook or a handler, ask one question: is there an HTTP
endpoint already? If yes, webhook. If no, a handler, and say plainly that the
handler you wrote is a stub the user has to fill in.
