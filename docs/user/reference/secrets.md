# Reference: secrets

`secrets` declares the environment values your agent needs at runtime: API tokens, base URLs, model keys. You declare them **by name**, never by value, so a package stays safe to commit and safe to share.

```yaml
secrets:
  - SALON_API_URL
  - SALON_API_TOKEN
  - OPENAI_API_KEY
```

It is a plain list of environment variable names, each `UPPER_SNAKE`. A secret has no fields at all: no `description`, no `required`, and above all no `default` or `example`, because any field that could hold a value would eventually hold a real one. The name is the whole declaration, and every name you list is required.

Listing the same name twice is an error, since a repeat is always a typo.

## What declaring them gives you

- **A filled-in `.env.example`** in every build: every declared name, plus the environment names your target and telephony connection need, grouped and labeled. Copy it to `.env` and fill in the blanks.
- **A startup check**: a missing value fails the agent immediately and names it, instead of surfacing as a confusing failure on the first tool call.
- **A cross-check**: if your package references an environment variable it never declares, `unmute validate` prints a warning naming it and the file that references it. It is a warning, not an error, so adopting the block is never a breaking change.

## How a secret reaches the call

Only three ways, all of which keep the value out of your files:

- A tool's `webhook.url_env`, `webhook.auth.token_env`, or `mcp.url_env`. See [tools](tools.md).
- A model's `endpoint_env`.
- `os.environ` inside a `local:` Python handler.

Secrets are deliberately **not** available to `{{variable}}` templates. A template that names a secret fails to compile, saying so. Templates are for values you might speak or send as data; a credential is neither. See [variables](variables.md).
