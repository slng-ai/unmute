# 02. Add a tool

Our agent can talk, but it cannot do anything. A **tool** is a function the model can call during the conversation. We will give the intake agent a tool that looks up a customer record.

A tool is one file in a `tools/` folder. The file name is the tool name.

## The tool file

```text
acme/
├── agent.yaml
├── instructions.md
├── targets.yaml
└── tools/
    └── lookup_customer.yaml
```

### tools/lookup_customer.yaml

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

## Wire it up in agent.yaml

Two changes. Add the tool to the intake agent, and list it in the top-level tools manifest:

```yaml
agents:
  intake:
    instructions: instructions.md
    model: fast_reasoning
    voice: front_desk
    tools:   # this agent may call it
      - lookup_customer

tools:   # compile this tool file into the package
  - lookup_customer
```

Those two lists do different jobs, and mixing them up is the usual first mistake:

- The **top-level `tools:`** is a load manifest. Only the tool files listed here get compiled into the package. It is the list of files to include.
- The **per-agent `tools:`** decides which agent can actually call the tool. A tool an agent does not list is invisible to that agent, even if it is in the package.

So a tool must appear in both: once at the top level to be included, and once on each agent that should use it.

And mention it in the prompt so the model knows when to reach for it:

```markdown
- When the caller gives a phone number or email, use `lookup_customer` to find
  their record.
```

## Reading the tool file

**`description`** is what the model reads to decide when to call the tool. Write it for the model, plainly.

**`input`** is a JSON Schema object: the arguments the model fills in. Here, an optional `phone` and `email`. Anything in `required` must be provided.

**`output`** is the shape you promise to return. It is optional. On code targets like Pipecat it is enforced by generated code; on managed targets there is nowhere to check it, so it warns there.

**The `webhook:` block** says how the tool runs. There is exactly one such block per file and its name is the execution kind, so `webhook:` means Unmute calls an HTTP endpoint you host. **This is the one kind that works on all four platforms; it is the safe choice.** The other blocks (`local:` for a Python handler, `mcp:`, `builtin:`) are gated per platform. Writing two blocks, or none, or a block with nothing under it, is an error naming the file and the line.

**`url_env`** names the environment variable that holds the endpoint URL. It is a variable name in `UPPER_SNAKE`, never a URL. You set `LOOKUP_CUSTOMER_URL` in your `.env`. Keeping the URL out of the spec means the same spec points at your staging endpoint in dev and your real one in production.

**`interruption`** decides what happens if the caller talks while the tool is running. `provider_default` leaves it to the platform and is the safe value. `cancel` and `continue` give finer control on code targets.

**`effect`** is `returns_data` (the normal case) or `ends_conversation` (the tool hangs up after it runs, for a "goodbye" action).

## What Pipecat generates

On Pipecat, this tool becomes a method on the intake agent's worker that POSTs the arguments to your endpoint and hands the response back to the model:

```python
@tool()
async def lookup_customer(self, params: FunctionCallParams, phone: str = "", email: str = ""):
    """Look up a customer record by phone number or email. Returns the customer id and name."""
    async with httpx.AsyncClient() as client:
        response = await client.post(
            os.environ["LOOKUP_CUSTOMER_URL"],
            json={"phone": phone, "email": email},
            timeout=30.0,
        )
        response.raise_for_status()
        await params.result_callback(response.json())
```

You never write or edit this. You change the spec and recompile.

## If your endpoint needs credentials

Most real endpoints do not accept anonymous POSTs. Add an `auth:` block inside
`webhook:` and the generated code proves who it is. Two schemes:

```yaml
webhook:
  url_env: LOOKUP_CUSTOMER_URL
  auth:
    type: bearer                       # Authorization: Bearer <token>
    token_env: LOOKUP_CUSTOMER_TOKEN
```

```yaml
webhook:
  url_env: LOOKUP_CUSTOMER_URL
  auth:
    type: api_key                      # X-API-Key: <token>
    token_env: LOOKUP_CUSTOMER_API_KEY
    header: X-API-Key                  # optional, this is the default
```

The token is the **name** of an environment variable, never the value. You put
the real value in your `.env`; the generated project lists the name in its
`.env.example`. Signing (HMAC) and OAuth2 are not supported — for those, use a
`local:` Python handler and make the request yourself.

The emitted call gains one line:

```python
        response = await client.post(
            os.environ["LOOKUP_CUSTOMER_URL"],
            headers=_bearer("LOOKUP_CUSTOMER_TOKEN"),
            json={"phone": phone, "email": email},
            timeout=30.0,
        )
```

This works on LiveKit and Pipecat. The managed targets configure tool auth on
their own side, so `auth` fails there.

## What just got harder

Not much, on purpose:

- The `webhook:` block is `core`. Webhook tools work on all four platforms. If you only ever use webhook tools, tools never block you anywhere.
- `output:` is `warn` on the managed target (Vapi), because it cannot enforce it. On Pipecat it is enforced.
- `interruption: provider_default` and `effect` are `core`.

Keep tools webhook-based and they stay in the safe core. Pipecat also emits
`local` Python handlers. It doesn't emit `mcp` tools yet; see the
[Pipecat target page](../targets/pipecat.md).

## You already have one: end_call

You don't have to write the tool that hangs up the call. LiveKit and Pipecat ship
it, and you pick it by name instead of authoring a handler:

```yaml
# tools/end_call.yaml
builtin:
  id: end_call
description: End the call once the caller's issue is resolved.   # optional
instructions: Thank the caller and say goodbye.                  # optional
```

If you ran `unmute init`, this file is **already there** and attached to your entry
agent, so a fresh agent can end its own calls out of the box. Keep it, change the
wording, or delete the file to drop it. It runs on LiveKit and Pipecat only. Full
details on the [tools reference page](../reference/tools.md#builtin).

Next: [03. Variables](03-variables.md), so the agent can remember what it looked up.
