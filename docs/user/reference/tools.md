# Reference: tools/*.yaml

A tool is one file in the `tools/` folder. The **file name is the tool name**: `tools/lookup_customer.yaml` defines `lookup_customer`. There is no `name` field inside the file. Which agents see the tool is decided only by their `tools:` lists in `agent.yaml`, never here. See the [add-a-tool learn page](../learn/02-add-a-tool.md).

```yaml
description: Look up a customer record by phone number or email. Returns the customer id and name.

input:
  type: object
  properties:
    phone: { type: string, description: Caller phone number in E.164 form }
    email: { type: string, description: Caller email address }

output:
  type: object
  properties:
    customer_id: { type: string }
    name:        { type: string }

execution: webhook
url_env: LOOKUP_CUSTOMER_URL
interruption: provider_default
effect: returns_data
```

## Fields

### description

What the model reads to decide when to call the tool.

Required: yes. Values: text. Default: none. Targets: all five, core.

### input

The arguments the model fills in, as a JSON Schema object. All five targets accept JSON Schema tool inputs, so nesting is allowed here.

Required: yes. Values: a JSON Schema object. Default: none. Targets: all five, core.

### output

The shape the tool promises to return.

Required: no. Values: a JSON Schema object. Default: none. Tag: warn. Enforced by generated code on code targets (LiveKit, Pipecat, Deepgram). Managed targets (Vapi, ElevenLabs) have no slot for it and warn.

### execution

Where the tool runs.

Required: yes. Values: `local | client | webhook | provider_hosted | builtin | mcp`. Default: none.

| Value | Where it works |
|---|---|
| `webhook` | all five targets. **The safe choice.** |
| `local` | code targets only (a handler file in your package) |
| `mcp` | fails on Deepgram (no runtime MCP client); on LiveKit needs SDK language `python` |
| `client`, `provider_hosted`, `builtin` | gated per driver; each driver documents what it can host |

On Pipecat the driver emits `webhook` tools only today; `local` and `mcp` are driver maturity gates.

### handler

The Python handler file for a `local` tool.

Required: conditional (iff `execution: local`). Values: a path, default `<name>.py`. Default: `<name>.py`. Code targets only.

### url_env

The environment variable holding the tool's endpoint. A variable name, never a URL value. For a `webhook` tool it is the webhook URL; for an `mcp` tool it is the MCP server address.

Required: conditional (iff `execution: webhook` or `mcp`). Values: an environment variable name. Default: none. Targets: all five, core.

### interruption

What happens if the caller talks while the tool runs.

Required: no. Values: `continue | cancel | provider_default`. Default: `provider_default`. Tag: warn. Honored on Pipecat. On LiveKit tools run to completion, so non-default values warn there. On managed targets only `provider_default` means anything; other values warn.

### effect

What the tool does to the conversation when it finishes.

Required: no. Values: `returns_data | ends_conversation`. Default: `returns_data`. Targets: all five, core.
