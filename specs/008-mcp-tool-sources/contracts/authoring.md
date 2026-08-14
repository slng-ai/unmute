# Contract: the `mcp:` tool block (authoring surface)

Feature: `008-mcp-tool-sources` · Lands as SCHEMA.md amendment **N40** (dated,
numbered, appended). The Go structs in `internal/spec` are the source of this
contract; this page is the human-readable statement of it.

## The block

```yaml
# tools/<name>.yaml — <name> is the tool-source name agents and tasks list
mcp:
  url_env: FIRECRAWL_MCP_URL       # required · UPPER_SNAKE env name, never a URL
  transport: streamable_http       # optional · sse | streamable_http
  auth:                            # optional · same shape as webhook auth (§5.3)
    type: bearer                   #   bearer | api_key
    token_env: FIRECRAWL_API_KEY   #   UPPER_SNAKE env name, never a value
    # header: X-API-Key            #   api_key only; default X-API-Key
  tools:                           # optional · server tool names; absent = all
    - firecrawl_search
```

## Rules (each is a test)

1. `url_env` is required and must be UPPER_SNAKE. (exists today)
2. `transport`, when present, is exactly `sse` or `streamable_http`; any other
   value fails validation naming both legal values. When absent, the generated
   project uses the platform's default for the URL.
3. `auth` follows webhook auth §5.3: `bearer` (`token_env`) or `api_key`
   (`token_env`, optional `header`, default `X-API-Key`). A field from the
   other scheme is an error. `token_env` must be UPPER_SNAKE.
4. `tools` entries are non-empty, unique strings. They are not checked against
   the live server; a name the server does not expose is simply never offered,
   and the emitted runbook says so.
5. An `mcp` file carries **no** top-level tool contract: `description`,
   `input`, `output`, `inject`, `interruption`, and `effect` each fail **at
   load**, named individually with the file, line, and what to remove. The
   server owns the per-tool contract. **This breaks old-shape mcp files on
   purpose; N40 records it.**
6. Assignment is unchanged: the source name goes in an agent's `tools:` list
   or a task's `tools:` list. Listing it exposes the selected tools (or all)
   to exactly that scope.
7. Two files may name the same `url_env`: they are independent sources, each
   with its own selection and assignment.

## Capability row (§7 matrix update)

| | pipecat | livekit | vapi | deepgram |
|---|---|---|---|---|
| mcp tools | **ok** (was: gated) | ok, Python SDK only | ok (validates only) | fail: no runtime MCP client |

Every env var the block names reaches the generated `.env.example`, the
startup check, and `compile-report.json` reference sites
(`tools/<name>.yaml mcp.url_env`, `tools/<name>.yaml mcp.auth.token_env`).
