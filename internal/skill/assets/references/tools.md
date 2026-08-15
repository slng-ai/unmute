# Tools

A tool is two things: a contract the model sees, and something that runs when
the model calls it. Both live in one file, `tools/<name>.yaml`.

## The shape of a tool file

```yaml tools/check_availability.yaml
description: >-
  List Sage and Stone slots for one service and date only after customer
  identification returned a real, nonempty customer_id.

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
| `output` | no | everywhere except `mcp:` |
| `inject` | no | `webhook:` and `local:` only |
| `interruption` | no | everywhere except `mcp:` |
| `effect` | no | everywhere except `mcp:` |

An `mcp:` file is the block and nothing else, because the server owns each
tool's contract. A `builtin:` file needs no `description` or `input`, because
the registry supplies both.

### The two gated blocks

`client:` and `provider_hosted:` exist in the schema and no target emits them.
Writing one fails with the target named:

```
livekit: LiveKit client tools are not proven by its driver
```

Do not write one, and do not offer one as an option. They are listed here so
that a refusal a user meets reads as a decision rather than a bug.

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
| `path` | no | appended to that base URL, may carry `{{variable}}` tokens |
| `auth` | no | how the request authenticates |

`url_env` holds a **name**, never a URL. Writing the address there is refused.
That is what lets staging and production run the same package against different
APIs.

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

### Authentication

| Field | What it is |
|---|---|
| `type` | `bearer` or `api_key` |
| `token_env` | environment variable holding the token |
| `header` | header name, `api_key` only, defaults to `X-API-Key` |

Those two schemes are the whole list in this version. If the user's API needs a
signed request, an OAuth exchange, or mutual TLS, a webhook tool cannot do it.
Say so and write a Python handler instead, which is exactly why
`examples/outbound-reminder` has one.

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
    reference:
      type: string
  required:
    - cancelled

local:
  handler: tools/cancel_appointment.py
```

```python tools/cancel_appointment.py
import hashlib
import hmac
import os


def cancel_appointment(customer_id):
    key = os.environ["SALON_API_SIGNING_KEY"].encode()
    body = f"cancel:{customer_id}".encode()
    signature = hmac.new(key, body, hashlib.sha256).hexdigest()
    return {"cancelled": True, "reference": signature[:12]}
```

The rules the function follows:

| Rule | Why |
|---|---|
| the function name matches the tool name | that is how the generated code finds it |
| its parameters match the `input` properties plus the `inject` keys | the call is built from both |
| it returns a dict shaped like `output`, if you declared one | the result goes back to the model |
| it may be `async def` | the generated code awaits an awaitable result |
| it imports nothing from Unmute | the generated project does not depend on Unmute at run time |

A handler reaches a credential through `os.environ`, and the variable name goes
in `secrets:` like any other. That is the third and last route a secret takes
into a call.

`unmute compile` copies the file into the generated project and imports it as a
plain module.

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
| `tools` | no | which of the server's tools to offer, absent means all of them |

`url_env` is a name, never an address. `transport` is optional because both
platforms guess it from the URL: a path ending in `/mcp` is streamable HTTP,
anything else is SSE. Write it when you want the choice visible rather than
inferred. Any other value is refused with both legal ones named.

Listing specific `tools:` is usually right. A whole server dropped into an
agent's tool list is a large, unreviewed surface, and the model will use all of
it.

## Prebuilt tools

```yaml tools/end_call.yaml
description: "End the call when the caller is finished or says goodbye."
builtin:
  id: end_call
```

**The registry is closed and has one row.**

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

## The two behaviour fields

```yaml
interruption: provider_default
effect: returns_data
```

| Field | Values | Default | Meaning |
|---|---|---|---|
| `interruption` | `provider_default`, `continue`, `cancel` | `provider_default` | what happens to the call if the caller speaks while the tool runs |
| `effect` | `returns_data`, `ends_conversation` | `returns_data` | whether the conversation continues after the tool |

Both are honoured differently per target. Pipecat maps `interruption` onto its
own cancel-on-interruption setting. LiveKit runs tools to completion, so a
non-default value warns there. Read the warning to the user rather than dropping
it.

## Turning a tool on is two lists

```yaml agent.yaml
agents:
  appointment_desk:
    instructions: instructions.md
    model: reasoning
    voice: voice
    tools:
      - check_availability
      - end_call

tools:
  - check_availability
  - end_call
```

The package level list says which tool files to load. The list on the agent says
which of those this agent may call. **That second list is the visibility scope,
and it is the only thing between an agent and a tool it should not touch.** A
task carries its own list for the same reason.

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
