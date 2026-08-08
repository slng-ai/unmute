# Reference: tools/*.yaml

A tool is one file in the `tools/` folder. The **file name is the tool name**: `tools/lookup_customer.yaml` defines `lookup_customer`. There is no `name` field inside the file. Which agents see the tool is decided only by their `tools:` lists in `agent.yaml`, never here. See the [add-a-tool learn page](../learn/02-add-a-tool.md).

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

execution: webhook
url_env: LOOKUP_CUSTOMER_URL
interruption: provider_default
effect: returns_data
```

## Fields

### description

What the model reads to decide when to call the tool.

Required: yes, except for a `builtin` tool where it is optional (the prebuilt supplies a default). Values: text. Default: none. Targets: all four, core.

### input

The arguments the model fills in, as a JSON Schema object. All four targets accept JSON Schema tool inputs, so nesting is allowed here.

Required: yes, except for a `builtin` tool, which has none (the prebuilt owns its schema). Values: a JSON Schema object. Default: none. Targets: all four, core.

### output

The shape the tool promises to return.

Required: no. Values: a JSON Schema object. Default: none. Tag: warn. Enforced by generated code on code targets (LiveKit, Pipecat, Deepgram). The managed target (Vapi) has no slot for it and warns.

### execution

Where the tool runs.

Required: yes. Values: `local | client | webhook | provider_hosted | builtin | mcp`. Default: none.

| Value | Where it works |
|---|---|
| `webhook` | all four targets. **The safe choice.** |
| `local` | code targets only (a handler file in your package) |
| `builtin` | a provider prebuilt tool you pick by name; LiveKit and Pipecat only (see below) |
| `mcp` | fails on Deepgram (no runtime MCP client); on LiveKit needs SDK language `python` |
| `client`, `provider_hosted` | gated per driver; each driver documents what it can host |

On Pipecat, the driver emits `webhook`, `local`, and `builtin` tools. `mcp` remains a driver
maturity gate.

## Prebuilt tools (`execution: builtin`)

Some tools you would otherwise hand-write are already shipped by the platforms.
Instead of authoring a handler, you **pick** one by name. Today there is one: `end_call`,
a tool the model calls to hang up when the caller is done.

```yaml
# tools/end_call.yaml  (the file name is still your tool name)
execution: builtin
builtin: end_call
description: End the call once the caller's issue is resolved.   # optional, extra guidance for the model
instructions: Thank the caller and say goodbye.                  # optional, the closing line
```

A prebuilt tool has **no `input`, `output`, `url_env`, or `handler`** — the platform owns
its schema and behavior. Both fields are optional:

- `description` adds your own guidance on top of the built-in description (LiveKit `extra_description`; Pipecat tool docstring).
- `instructions` is the closing message (LiveKit `end_instructions`; a Pipecat developer message).

`end_call` works on **LiveKit and Pipecat**. It fails on Vapi and Deepgram,
which have no lowering for it. It ends the call, so its `effect` is fixed to
`ends_conversation`.

**`end_call` is included by default.** `unmute init` scaffolds a `tools/end_call.yaml` and
attaches it to your entry agent, and the create wizard seeds it too. Keep it, edit its
wording, or delete the file (and its reference in `agent.yaml`) if you don't want it. If you
switch a new agent to a managed target that can't host it, the wizard drops it for you.

### builtin

The prebuilt-tool id to use. See [Prebuilt tools](#prebuilt-tools-execution-builtin) above.

Required: conditional (iff `execution: builtin`). Values: a prebuilt id (today: `end_call`). Default: none. An unknown id is an error.

### instructions

The closing message for a prebuilt tool that ends the call.

Required: no, and legal only on a `builtin` tool. Values: text. Default: none (the platform's default goodbye).

### handler

The Python handler file for a `local` tool.

Required: conditional (iff `execution: local`). Values: a path, default `<name>.py`. Default: `<name>.py`. Code targets only.

### url_env

The environment variable holding the tool's endpoint. A variable name, never a URL value. For a `webhook` tool it is the webhook URL; for an `mcp` tool it is the MCP server address.

Required: conditional (iff `execution: webhook` or `mcp`). Values: an environment variable name. Default: none. Targets: all four, core.

### interruption

What happens if the caller talks while the tool runs.

Required: no. Values: `continue | cancel | provider_default`. Default: `provider_default`. Tag: warn. Honored on Pipecat. On LiveKit tools run to completion, so non-default values warn there. On managed targets only `provider_default` means anything; other values warn.

### effect

What the tool does to the conversation when it finishes.

Required: no. Values: `returns_data | ends_conversation`. Default: `returns_data`. Targets: all four, core. For a `builtin` tool the value is fixed by the prebuilt (`end_call` is always `ends_conversation`); a conflicting value is an error.
