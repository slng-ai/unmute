# Reference: tools/*.yaml

A tool is one file in the `tools/` folder. The **file name is the tool name**: `tools/lookup_customer.yaml` defines `lookup_customer`. There is no `name` field inside the file. Which agents see the tool is decided only by their `tools:` lists in `agent.yaml`, never here. See the [add-a-tool learn page](../learn/02-add-a-tool.md).

The top level says what the model sees. **One block, named after the execution kind, says how the tool runs.** The two conversation settings stay at the top level.

```yaml
description: Look up a customer record by phone number or email. Returns the customer id and name.

input:
  type: object
  properties:
    phone:
      type: string
      description: Caller phone number in E.164 form
    email:
      type: string
      description: Caller email address

output:
  type: object
  properties:
    customer_id:
      type: string
    name:
      type: string

webhook:
  url_env: LOOKUP_CUSTOMER_URL

interruption: provider_default
effect: returns_data
```

There is exactly one execution block per file. A file with no block, two blocks, or a block with an empty body is an error that names the file (and the line, when there is one to point at). Because the block name is the kind, a field that belongs to another kind has nowhere to go: you cannot write a `handler` on a webhook tool by mistake.

## Fields

### description

What the model reads to decide when to call the tool.

Required: yes, except for a `builtin` tool where it is optional (the prebuilt supplies a default). Values: text. Default: none. Targets: all four, core.

### input

The arguments the model fills in, as a JSON Schema object. All four targets accept JSON Schema tool inputs, so nesting is allowed here.

Required: yes, except for a `builtin` tool, which has none (the prebuilt owns its schema). Values: a JSON Schema object. Default: none. Targets: all four, core.

### output

The shape the tool promises to return.

Required: no. Values: a JSON Schema object. Default: none. Tag: warn. Declared and carried into `compile-report.json`, but not enforced on any target yet (SCHEMA.md N22); the warning you see comes from Vapi only. It is still worth writing: it documents what the tool returns.

## Execution blocks

Exactly one of these blocks says how the tool runs. The block name is the execution kind, so there is no separate `execution:` field.

| Block | What it does | Where it works |
|---|---|---|
| `webhook:` | calls an HTTP endpoint you host | all four targets. **The safe choice.** |
| `local:` | runs a Python handler in your package | code targets only |
| `builtin:` | picks a provider prebuilt tool by name | LiveKit and Pipecat only (see below) |
| `mcp:` | offers the tools of a remote MCP server | fails on Deepgram (no runtime MCP client); on LiveKit needs SDK language `python`; on Pipecat, agent scope only |
| `client: {}`, `provider_hosted: {}` | gated per driver | each driver documents what it can host |

On Pipecat, the driver emits the `webhook`, `local`, `builtin`, and `mcp` blocks. The one thing it cannot do is scope an MCP source to a **task**; list it on the agent instead, and validation says so if you do not.

### webhook:

```yaml
webhook:
  url_env: LOOKUP_CUSTOMER_URL     # required: env var name, never a URL
  path: /customers/{{customer_id}}  # optional, appended to the base URL
  auth:                            # optional, see "Webhook auth" below
    type: bearer
    token_env: LOOKUP_CUSTOMER_TOKEN
```

`url_env` names the environment variable holding the endpoint. It is a variable name, never a URL: the name must be `UPPER_SNAKE`, so a pasted URL fails validation. You set the real value in your `.env`; keeping it out of the spec means the same spec points at staging in dev and production in prod. The generated code reads it as `os.environ["LOOKUP_CUSTOMER_URL"]` inside the request, so a rotated value needs no recompile. Declare the name under [`secrets:`](secrets.md) and it lands in `.env.example` and the startup check.

`path` is optional and appended to that base URL. It must start with `/`, and it may carry `{{variable}}` tokens, whose values are URL-encoded when substituted (so a customer id containing a slash cannot change the route). Works on LiveKit and Pipecat; fails on Vapi and Deepgram.

### local:

```yaml
local:
  handler: tools/lookup_customer.py   # optional, defaults to tools/<tool name>.py
```

Code targets only. The handler file travels with your package and is copied into the generated project.

**A handler that needs a credential reads it itself**, with `os.environ` inside the function:

```python
def lookup_customer(phone):
    token = os.environ["LOOKUP_CUSTOMER_TOKEN"]
    ...
```

There is no credential field on the `local:` block and no secret object passed into your function. Your handler is normal Python and reads its own environment. Declare the name under [`secrets:`](secrets.md) so it reaches `.env.example` and the startup check; `unmute validate` scans handler bodies for these reads and warns about any name the package never declares.

### mcp:

An MCP server is a **tool source**: one file, one server, and the server tells
the agent what its tools are when the connection opens.

```yaml
# tools/web_search.yaml  (the file name is the source name)
mcp:
  url_env: FIRECRAWL_MCP_URL     # required: the server address, by env var name
  transport: streamable_http     # optional: sse | streamable_http
  auth:                          # optional: the same shapes webhook auth uses
    type: bearer
    token_env: FIRECRAWL_API_KEY
  tools:                         # optional: leave it out to offer every tool
    - firecrawl_search
```

**The file is the block and nothing else.** No `description`, no `input`, no
`output`, no `inject`, no `interruption`, no `effect`. The server owns each
tool's contract, so a contract written here would be a claim nothing reads.
Writing one fails at load, naming the line to delete (SCHEMA.md N40).

`transport` is optional. Leave it out and both targets use the same rule: a URL
whose path ends in `/mcp` speaks streamable HTTP, anything else speaks SSE.
Write it when your server disagrees with that guess.

`tools` is the selection. Without it the agent is offered every tool the server
exposes, which for a big server is usually too many for a voice call. With it,
the agent is offered exactly those. The names are not checked at compile time,
because the server's tool list only exists once the connection is open: a name
the server does not expose is simply never offered.

`auth` is the [webhook auth](#webhook-auth) shape, `bearer` or `api_key`, with
the token named by an environment variable. Both the address and the token
reach `.env.example` and the generated startup check, so a missing value is
named before the agent dials.

Assign the source like any other tool: put its name in an agent's `tools:` list
(or a task's, on LiveKit). Two files may name the same `url_env`; they are two
independent sources with their own selections.

Runnable example: [`examples/mcp-example`](../../../examples/mcp-example/).

### builtin:

```yaml
# tools/end_call.yaml  (the file name is still your tool name)
description: End the call once the caller's issue is resolved.   # optional, extra guidance for the model

builtin:
  id: end_call
  instructions: Thank the caller and say goodbye.                # optional, the closing line
```

Some tools you would otherwise hand-write are already shipped by the platforms. Instead of authoring a handler, you **pick** one by name. Today there is one: `end_call`, a tool the model calls to hang up when the caller is done.

A prebuilt tool has **no `input` or `output`** — the platform owns its schema and behavior. Both block fields beyond `id` are optional:

- `description` (top level) adds your own guidance on top of the built-in description (LiveKit `extra_description`; Pipecat tool docstring).
- `instructions` is the closing message (LiveKit `end_instructions`; a Pipecat developer message).

`end_call` works on **LiveKit and Pipecat**. It fails on Vapi and Deepgram, which have no lowering for it. It ends the call, so its `effect` is fixed to `ends_conversation`, and an unknown `id` is an error.

**`end_call` is included by default.** `unmute init` scaffolds a `tools/end_call.yaml` and attaches it to your entry agent, and the create wizard seeds it too. Keep it, edit its wording, or delete the file (and its reference in `agent.yaml`) if you don't want it. If you switch a new agent to a managed target that can't host it, the wizard drops it for you.

## Webhook auth

Most real endpoints do not accept anonymous POSTs. `webhook.auth` says how the generated code proves who it is. `type` picks the scheme; `header` belongs to `api_key` only, and writing it on a `bearer` tool is an error rather than a silent no-op.

Works on LiveKit and Pipecat. Vapi and Deepgram fail: a managed target configures its tool auth on its own side.

**The token is an environment variable name, never a value.** Names are `UPPER_SNAKE`, so a pasted token fails validation. It lands in the generated `.env.example`; on Pipecat it also joins the startup check, so a missing token fails when the bot boots instead of mid-call. [secrets](secrets.md) shows the emitted header helper and traces a token from `agent.yaml` to the running request.

### type: bearer

```yaml
webhook:
  url_env: LOOKUP_PLACES_URL
  auth:
    type: bearer
    token_env: LOOKUP_PLACES_TOKEN
```

Sends `Authorization: Bearer <token>`.

### type: api_key

```yaml
webhook:
  url_env: LOOKUP_PLACES_URL
  auth:
    type: api_key
    token_env: LOOKUP_PLACES_API_KEY
    header: X-API-Key          # optional, this is the default
```

Sends the token verbatim in its own header.

`basic` auth, request signing (HMAC), and OAuth2 are not supported. If your
endpoint needs one, use a [`local:`](#local) Python handler, where you control
the request yourself and read the credential with `os.environ`.
`examples/outbound-reminder/tools/cancel_appointment.py` is a worked one.

## Conversation settings

Both stay at the top level of the file: they describe what the call does to the conversation, not how the tool runs.

### inject

Values merged into the tool call that the model never sees:

```yaml
inject:
  customer_id: "{{customer_id}}"   # from a variable
  channel: phone                   # or a literal
```

This is how a tool receives something the model should not have to guess or repeat back: a customer id from the dispatch, a slot the caller just named, a fixed channel label. Injected keys are invisible to the model, so it can neither read them nor override them, and a key here may not also appear in `input.properties`.

A value that is exactly one `{{token}}` keeps the variable's declared type, so an `integer` variable arrives as a JSON number. Anything mixed with surrounding text renders to a string.

If an injected variable is still unset when the model calls the tool, the call is refused with a message telling the model what to ask for, and no request is sent. A variable the runtime owns (a system source) never gates a call that way, since no caller can be asked for it.

Legal on `webhook:` (merged into the POST body) and `local:` (merged into the handler's keyword arguments). Works on LiveKit and Pipecat; fails on Vapi and Deepgram.

### interruption

What happens if the caller talks while the tool runs.

Required: no. Values: `continue | cancel | provider_default`. Default: `provider_default`. Tag: warn. Honored on Pipecat. On LiveKit tools run to completion, so non-default values warn there. On managed targets only `provider_default` means anything; other values warn.

### effect

What the tool does to the conversation when it finishes.

Required: no. Values: `returns_data | ends_conversation`. Default: `returns_data`. Targets: all four, core. For a `builtin` tool the value is fixed by the prebuilt (`end_call` is always `ends_conversation`); a conflicting value is an error.
