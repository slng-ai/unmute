# Reference: secrets

`secrets` declares the environment values your agent needs at runtime: API tokens, base URLs, signing keys, model keys. You declare them **by name**, never by value, so a package stays safe to commit and safe to share.

```yaml
secrets:
  - SALON_API_URL
  - SALON_API_TOKEN
  - OPENAI_API_KEY
```

It is a plain list of environment variable names, each `UPPER_SNAKE`. A secret has no fields at all: no `description`, no `required`, and above all no `default` or `example`, because any field that could hold a value would eventually hold a real one. The name is the whole declaration, and every name you list is required.

Listing the same name twice is an error, since a repeat is always a typo.

## The one idea

**The name lives in your package. The value lives in the environment.** Every place a credential is needed, you write the name of the variable that holds it, and the generated code reads that variable at the moment it makes the call:

```
agent.yaml / tools/*.yaml        .env (never committed)          the running call
  token_env: SALON_API_TOKEN  →  SALON_API_TOKEN=sk_live_...  →  os.environ["SALON_API_TOKEN"]
```

Nothing in between ever holds the value. Not the compiled Python, not the prompt, not the model's context, not the compile report. This is the same thing you would do by hand with `os.getenv()`, with the declaration written down so the tooling can check it.

## How a secret reaches a tool

There are two shapes, and which one you use depends on how the tool runs.

### Shape 1: a webhook tool, declared in YAML

You name the variables and unmute writes the request. This covers most tools.

```yaml
# tools/confirm_appointment.yaml
description: Confirm that the existing appointment stays as booked.

input:
  type: object
  properties: {}

inject:
  customer_id: "{{customer_id}}"

webhook:
  url_env: SALON_API_URL                   # the base URL is itself a secret
  path: /customers/{{customer_id}}/appointments/confirm
  auth:
    type: bearer
    token_env: SALON_API_TOKEN
```

compiles to:

```python
async with httpx.AsyncClient() as client:
    response = await client.post(
        os.environ["SALON_API_URL"].rstrip("/")
        + _render(
            "/customers/{{customer_id}}/appointments/confirm",
            state,
            quote_values=True,
        ),
        headers=_bearer("SALON_API_TOKEN"),
        json={"customer_id": state.customer_id},
        timeout=30.0,
    )
    response.raise_for_status()
```

where `_bearer` is a three-line helper the compiler emits only when a tool asks for it:

```python
def _bearer(env: str) -> dict[str, str]:
    """auth: bearer — the Authorization header read from the tool's token_env."""
    return {"Authorization": "Bearer " + os.environ[env]}
```

`auth.type: api_key` emits the sibling helper and sends the token verbatim in its own header, `X-API-Key` unless you set `header:`. Both read the environment inside the request, not at import, so rotating a value never needs a recompile.

Because the base URL is `url_env` and not a literal, the same package points at staging in dev and production in prod with no edit. Full field list in [tools](tools.md).

### Shape 2: a local handler, read in Python

Some endpoints need something `webhook.auth` cannot express: request signing, OAuth2, basic auth, a vendor SDK. For those you write a `local:` handler and control the request yourself, which means reading the credential yourself:

```yaml
# tools/cancel_appointment.yaml
local:
  handler: tools/cancel_appointment.py

inject:
  customer_id: "{{customer_id}}"
```

```python
# tools/cancel_appointment.py
import hashlib
import hmac
import os


def cancel_appointment(customer_id):
    key = os.environ["SALON_API_SIGNING_KEY"].encode()
    body = f"cancel:{customer_id}".encode()
    signature = hmac.new(key, body, hashlib.sha256).hexdigest()
    ...
```

This is a plain `os.environ` read, and that is the whole mechanism. There is no separate wrapper type, no `secret` object passed into your function, and no field on the `local:` block that lists credentials. Your handler is normal Python and reads its own environment, the same as any other script you would write.

Declare `SALON_API_SIGNING_KEY` in `secrets:` and it joins the `.env.example` and the startup check like any other name. If you forget, `unmute validate` finds the read and tells you (see below).

Read the value **inside** the function rather than at module top level. A top-level read runs at import, which turns a missing value into an import error instead of the named startup failure.

Both shapes are shown side by side, running, in [examples/outbound-reminder](https://github.com/slng-ai/unmute_cli/tree/main/examples/outbound-reminder).

### The other two slots

- A **model** endpoint: `endpoint_env` on a model binding, for a self-hosted or proxied provider. It lowers to `base_url=os.environ["ACME_BASE_URL"]` in the service constructor. See [models and voices](models-and-voices.md).
- An **MCP** server address: `mcp.url_env`, which lowers to `MCPServerHTTP(url=os.environ["BOOKINGS_MCP_URL"], ...)`.

## Why secrets never go through `{{...}}`

A template that names a secret fails to compile, and the error says so. This is deliberate, and the reason is worth understanding, because it is the one rule that surprises people.

`{{...}}` is the **speakable channel**. Every site it renders into ends up somewhere a credential must never be:

| Template site | Where the rendered value ends up |
|---|---|
| `conversation.greeting.text` | spoken out loud to the caller |
| agent and task `instructions` | the model's context, and the model provider's logs |
| a tool's `inject:` | tool call arguments, which Langfuse traces record |
| `webhook.path` | a URL, which every proxy and access log keeps |

A token in any of those is a burned token. So variables and secrets are not two grades of the same thing, they are two channels with different destinations:

- **Variables** are values you might say, show, or send as data. They flow through `{{...}}`.
- **Secrets** are credentials. They flow through the `*_env` slots and `os.environ`, and they never touch a template.

If you find yourself wanting a secret in a template, what you actually want is one of the two shapes above. A URL that must stay private is `url_env`, not a variable interpolated into a path.

## What declaring them gives you

- **A filled-in `.env.example`** in every build: every declared name, then the names your target and telephony connection need, grouped and labeled. Copy it to `.env` and fill in the blanks.

  ```
  # Environment for pipecat (generated by unmute).
  # Copy this file to `.env` and fill in the values. Never commit `.env`.

  OPENAI_API_KEY=
  SALON_API_SIGNING_KEY=
  SALON_API_TOKEN=
  SALON_API_URL=

  # required by the target or a connection
  TWILIO_ACCOUNT_SID=
  ```

- **A startup check**: every declared name is in the generated `REQUIRED_ENV`, and a missing value fails the agent immediately, naming it, instead of surfacing as a confusing failure on the first tool call.

- **A cross-check**: if your package references an environment variable it never declares, `unmute validate` prints a warning naming it and the site that references it. It scans tool `url_env`, `auth.token_env`, `mcp.url_env`, model `endpoint_env`, the Langfuse trio when `tracing:` is set, **and the body of every local handler** for `os.environ["X"]`, `os.environ.get("X")`, and `os.getenv("X")`:

  ```
  Warnings:
    pipecat: environment variables referenced but not declared in secrets:
      SALON_API_SIGNING_KEY (tools/cancel_appointment.py os.environ)
  ```

  It is a warning, not an error, so adopting the block is never a breaking change, and a package with no `secrets:` block at all stays silent. The handler scan is a text match rather than a Python parse, so a name inside a comment counts too. It over-reports rather than under-reports on purpose: the cost of a spurious line is that you declare one more name, and the cost of a miss is a credential that fails mid-call.

  Connection environment values and `destinations` values are part of this check. They used to be exempt, on the grounds that they are declared in their own file; that left no single list of what a package needs to run. The cost of including them is that the name appears twice — once in the connection, where it says which *role* the value plays on the route, and once here, where it says the runtime needs it. Two lines, two different questions.

  Names you never write are still absent, because nothing in the package declares them: `REDIS_URL` and `UNMUTE_PUBLIC_URL` come from `unmute dev` or the operator, `LIVEKIT_*` from the Compose graph or LiveKit Cloud, and `DAILY_API_KEY` and `PIPECAT_CLOUD_ORGANIZATION` from the route's own runtime. They still have to be set wherever the agent runs; the generated `.env.example` groups them under their own heading. See [connections](connections.md#where-each-value-goes).

## Where the values go

Values live in an ignored `.env`, never in the package. `unmute dev` reads `.env` from the current directory first, then from the package root, so one repository-root `.env` covers every example. In deployment, set them however your platform sets environment variables.

The compile report (`build/<target>/compile-report.json`) lists each declared secret with the sites that reference it, so you can see at a glance what a name is actually for:

```json
{
  "name": "SALON_API_SIGNING_KEY",
  "referenced_by": ["tools/cancel_appointment.py os.environ"]
}
```

It records names and sites only. No value ever enters it.
