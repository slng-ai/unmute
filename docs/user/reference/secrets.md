# Reference: secrets

`secrets` declares the environment values your agent needs at runtime: API tokens, base URLs, model keys. You declare them **by name**, never by value, so a package stays safe to commit and safe to share.

```yaml
secrets:
  SALON_API_URL:
    description: Base URL of the booking API.
  SALON_API_TOKEN:
    description: Bearer token for the booking API.
  OPENAI_API_KEY:
    description: Key for the think model.
  SEGMENT_WRITE_KEY:
    description: Analytics, only used when the customer opts in.
    required: false
```

The map key **is** the environment variable name, so it must be `UPPER_SNAKE`. There is no `default` or `example` field, on purpose: any field that could hold a value would eventually hold a real one.

## Fields

### description

What the value is and where it comes from. It appears above the name in the generated `.env.example`, and in the compile report.

Required: no. Values: text. Default: none.

### required

Whether the agent can run without it. With `required: true` (the default), a generated runtime refuses to start when the value is missing or empty, and names it. `required: false` marks a credential for an optional feature.

Required: no. Values: `true | false`. Default: `true`.

## What declaring them gives you

- **A filled-in `.env.example`** in every build: each declared secret with its description above it, optional ones marked, plus the environment names your target and telephony connection need. Copy it to `.env` and fill in the blanks.
- **A startup check**: a missing required value fails the agent immediately, with the name and description in the message, instead of surfacing as a confusing failure on the first tool call.
- **A cross-check**: if your package references an environment variable it never declares, `unmute validate` prints a warning naming it and the file that references it. It is a warning, not an error, so adopting the block is never a breaking change.

## How a secret reaches the call

Only three ways, all of which keep the value out of your files:

- A tool's `webhook.url_env`, `webhook.auth.token_env`, or `mcp.url_env`. See [tools](tools.md).
- A model's `endpoint_env`.
- `os.environ` inside a `local:` Python handler.

Secrets are deliberately **not** available to `{{variable}}` templates. A template that names a secret fails to compile, saying so. Templates are for values you might speak or send as data; a credential is neither. See [variables](variables.md).
